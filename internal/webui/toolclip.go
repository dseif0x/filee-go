package webui

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/tools"
)

// The clip editor keeps one ClipCutter per video on the server, so the
// editing (cuts, trims, undo) is exactly the original's; the page sends
// operations and draws the state it gets back.

type clipSession struct {
	mu     sync.Mutex
	cutter *presets.ClipCutter
	// proxy is a browser-playable copy of a video the browser can't play.
	proxy    string
	proxyErr string
	proxyMu  sync.Mutex
}

// clipState is what the page draws.
type clipState struct {
	Duration float64             `json:"duration"`
	Start    float64             `json:"start"`
	End      float64             `json:"end"`
	Length   float64             `json:"length"`
	Pieces   []clipPieceJSON     `json:"pieces"`
	FadeIn   float64             `json:"fadeIn"`
	FadeOut  float64             `json:"fadeOut"`
	Music    *presets.ClipMusic  `json:"music,omitempty"`
	CanUndo  bool                `json:"canUndo"`
	CanRedo  bool                `json:"canRedo"`
	HasGaps  bool                `json:"hasGaps"`
	Plain    bool                `json:"plain"`
	Result   any                 `json:"result,omitempty"`
	Message  string              `json:"message,omitempty"`
	Edit     *presets.ClipEdit   `json:"edit,omitempty"`
	Display  map[string]string   `json:"display"`
	Silences []presets.ClipRange `json:"silences,omitempty"`
}

type clipPieceJSON struct {
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	State  string  `json:"state"`
	Volume float64 `json:"volume"`
	Speed  float64 `json:"speed"`
	Muted  bool    `json:"muted"`
}

func stateOf(c *presets.ClipCutter) clipState {
	st := clipState{
		Duration: c.Duration(), Start: c.Start(), End: c.End(), Length: c.Length(), FadeIn: c.FadeIn(), FadeOut: c.FadeOut(),
		Music: c.Music(), CanUndo: c.CanUndo(), CanRedo: c.CanRedo(), HasGaps: c.HasGaps(),
		Display: map[string]string{},
	}
	for _, p := range c.Pieces() {
		state := "kept"
		switch p.State {
		case presets.PieceRemoved:
			state = "removed"
		case presets.PieceBlank:
			state = "blank"
		}
		st.Pieces = append(st.Pieces, clipPieceJSON{Start: p.Start, End: p.End, State: state, Volume: p.Volume, Speed: p.Speed, Muted: p.Muted})
	}
	if e, err := c.ToEdit(); err == nil {
		st.Edit = e
		st.Plain = e.IsPlainCut()
	}
	st.Display["start"] = presets.ClipDisplay(st.Start)
	st.Display["end"] = presets.ClipDisplay(st.End)
	st.Display["length"] = presets.ClipDisplay(st.Length)
	st.Display["duration"] = presets.ClipDisplay(st.Duration)
	return st
}

// clipOp is one editing operation: {"op": "split", "at": 12.5}, …
type clipOpRequest struct {
	Op       string  `json:"op"`
	At       float64 `json:"at"`
	Index    int     `json:"index"`
	End      bool    `json:"end"`
	CloseUp  bool    `json:"closeUp"`
	Value    float64 `json:"value"`
	Muted    bool    `json:"muted"`
	Join     string  `json:"join"`
	Duration float64 `json:"duration"`
	Path     string  `json:"path"`
	Indices  []int   `json:"indices"`
}

func (s *server) clipSessionFor(t *ToolJob, i int, duration float64) *clipSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	cs := t.clips[i]
	if cs == nil && duration > 0 {
		r := presets.ClipRange{Start: 0, End: duration}
		cs = &clipSession{cutter: presets.NewClipCutter(duration, r)}
		t.clips[i] = cs
	}
	return cs
}

func (s *server) clipOp(w http.ResponseWriter, r *http.Request, t *ToolJob, i int) {
	var in clipOpRequest
	if !readJSON(w, r, &in) {
		return
	}
	if in.Op == "init" {
		d := in.Duration
		if !(d > 0) || math.IsInf(d, 0) {
			if pd, ok := probeDuration(r.Context(), t.file(i).Path); ok {
				d = pd
			}
		}
		if !(d > 0) || math.IsInf(d, 0) {
			writeError(w, http.StatusUnprocessableEntity, errors.New(s.a.Text("clip.cannot_play")))
			return
		}
		cs := s.clipSessionFor(t, i, d)
		cs.mu.Lock()
		st := stateOf(cs.cutter)
		cs.mu.Unlock()
		writeJSON(w, st)
		return
	}
	cs := s.clipSessionFor(t, i, 0)
	if cs == nil {
		writeError(w, http.StatusBadRequest, errors.New("the clip isn't open"))
		return
	}
	var result any
	message := ""
	if in.Op == "remove-silence" {
		// Found outside the lock: it reads the whole sound.
		cs.mu.Lock()
		start, end := cs.cutter.Start(), cs.cutter.End()
		cs.mu.Unlock()
		silences, err := findSilences(r.Context(), t.file(i).Path, start, end)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		cs.mu.Lock()
		n := cs.cutter.RemoveSilences(silences)
		cs.mu.Unlock()
		result = n
		if n == 0 {
			message = s.a.Text("clip.no_silence")
		} else {
			message = s.a.Text("clip.silence_removed", n)
		}
	}
	cs.mu.Lock()
	c := cs.cutter
	do := func(f func()) {
		if in.Join != "" {
			c.Batch(f, in.Join)
		} else {
			f()
		}
	}
	switch in.Op {
	case "remove-silence", "state":
	case "move-start":
		do(func() { c.MoveStart(in.At) })
	case "move-end":
		do(func() { c.MoveEnd(in.At) })
	case "move":
		do(func() { c.Move(in.At) })
	case "set-length":
		c.SetLength(in.Value)
	case "split":
		ok := c.Split(in.At)
		result = ok
		if !ok {
			message = s.a.Text("clip.cannot_cut")
		}
	case "remove":
		ok := true
		c.Batch(func() {
			idx := in.Indices
			if len(idx) == 0 {
				idx = []int{in.Index}
			}
			// From the last: indices before stay where they are.
			for k := len(idx) - 1; k >= 0; k-- {
				ok = c.Remove(idx[k], in.CloseUp) && ok
			}
		}, "")
		result = ok
		if !ok {
			message = s.a.Text("clip.last_piece")
		}
	case "restore":
		result = c.Restore(in.Index)
	case "close-gap":
		result = c.CloseGap(in.Index)
	case "close-gaps":
		result = c.CloseGaps()
	case "trim":
		v := c.TrimEdge(in.Index, in.End, in.At)
		if !math.IsNaN(v) {
			result = v
		}
	case "volume":
		do(func() {
			for _, k := range indicesOr(in) {
				c.SetVolume(k, in.Value)
			}
		})
	case "mute":
		c.Batch(func() {
			for _, k := range indicesOr(in) {
				c.SetMuted(k, in.Muted)
			}
		}, "")
	case "speed":
		c.Batch(func() {
			for _, k := range indicesOr(in) {
				c.SetSpeed(k, in.Value)
			}
		}, "")
	case "fade-in":
		c.SetFadeIn(in.Value)
	case "fade-out":
		c.SetFadeOut(in.Value)
	case "music":
		if in.Path == "" {
			c.SetMusic(nil)
		} else {
			cs.mu.Unlock()
			length, ok := probeDuration(r.Context(), in.Path)
			cs.mu.Lock()
			if !ok {
				message = s.a.Text("clip.no_music")
				break
			}
			c.SetMusic(&presets.ClipMusic{Path: in.Path, Length: length, Volume: presets.DefaultMusicVolume, Fade: true})
		}
	case "move-music":
		do(func() { c.MoveMusic(in.At) })
	case "music-volume":
		do(func() { c.SetMusicVolume(in.Value) })
	case "undo":
		result = c.Undo()
	case "redo":
		result = c.Redo()
	default:
		cs.mu.Unlock()
		writeError(w, http.StatusBadRequest, errors.New("unknown operation "+in.Op))
		return
	}
	st := stateOf(c)
	cs.mu.Unlock()
	st.Result, st.Message = result, message
	writeJSON(w, st)
}

func indicesOr(in clipOpRequest) []int {
	if len(in.Indices) > 0 {
		return in.Indices
	}
	return []int{in.Index}
}

func (s *server) clipSave(w http.ResponseWriter, r *http.Request, t *ToolJob, i int) {
	var in struct {
		Fast bool `json:"fast"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	cs := s.clipSessionFor(t, i, 0)
	if cs == nil {
		writeError(w, http.StatusBadRequest, errors.New("the clip isn't open"))
		return
	}
	cs.mu.Lock()
	edit, err := cs.cutter.ToEdit()
	cs.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.remember(t, func(p *presets.Preset) { p.Media.ExactCut = !in.Fast })
	f := t.file(i)
	t.mu.Lock()
	conv := presets.ClipConversionOf(t.Preset, f.Target, edit)
	t.mu.Unlock()
	// Fast is a stream copy, which only a plain cut can be.
	conv.Media.ExactCut = !in.Fast || !edit.IsPlainCut()
	j := s.a.ConvertWith([]string{f.Path}, conv, t.Name, nil)
	s.track(t, i, j.ID())
	s.advance(t, i)
	writeJSON(w, t.snapshot())
}

// browserPlays: a web view plays the container (H.264/HEVC/VP9/AV1 inside).
func browserPlays(format string) bool {
	switch format {
	case "mp4", "m4v", "mov", "webm", "ogv":
		return true
	}
	return false
}

func ffmpegPath() string {
	return tools.Find(tools.Spec{Names: []string{"ffmpeg"}, Env: "FILEE_FFMPEG", Dirs: map[string][]string{
		"darwin": {"ffmpeg", "/opt/homebrew/bin", "/usr/local/bin"}, "linux": {"ffmpeg"}, "windows": {"ffmpeg", `ffmpeg\bin`},
	}})
}

func ffprobePath() string {
	return tools.Find(tools.Spec{Names: []string{"ffprobe"}, Env: "FILEE_FFPROBE", Dirs: map[string][]string{
		"darwin": {"ffmpeg", "/opt/homebrew/bin", "/usr/local/bin"}, "linux": {"ffmpeg"}, "windows": {"ffmpeg", `ffmpeg\bin`},
	}})
}

// probeDuration reads a media file's length with FFprobe, when it is there.
func probeDuration(ctx context.Context, path string) (float64, bool) {
	exe := ffprobePath()
	if exe == "" {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := tools.Run(ctx, tools.Cmd{Path: exe, Args: []string{"-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", path}})
	if err != nil {
		return 0, false
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(res.Stdout)), 64)
	return d, err == nil && d > 0
}

var silenceLine = regexp.MustCompile(`silence_(start|end):\s*(-?[0-9.]+)`)

// findSilences runs FFmpeg's silencedetect over the clip's stretch of the sound.
func findSilences(ctx context.Context, path string, start, end float64) ([]presets.ClipRange, error) {
	exe := ffmpegPath()
	if exe == "" {
		return nil, errors.New("FFmpeg is not installed")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	args := []string{"-hide_banner", "-nostats", "-ss", presets.ClipFfmpeg(start), "-to", presets.ClipFfmpeg(end), "-i", path,
		"-vn", "-af", "silencedetect=noise=-35dB:d=" + strconv.FormatFloat(presets.MinSilence, 'f', 2, 64), "-f", "null", "-"}
	res, err := tools.Run(ctx, tools.Cmd{Path: exe, Args: args})
	if err != nil && res == nil {
		return nil, err
	}
	var out []presets.ClipRange
	open := math.NaN()
	for _, m := range silenceLine.FindAllStringSubmatch(string(res.Stderr), -1) {
		v, _ := strconv.ParseFloat(m[2], 64)
		v += start
		if m[1] == "start" {
			open = v
		} else if !math.IsNaN(open) {
			out = append(out, presets.ClipRange{Start: open, End: v})
			open = math.NaN()
		}
	}
	if !math.IsNaN(open) {
		out = append(out, presets.ClipRange{Start: open, End: end})
	}
	return out, nil
}

// clipMedia serves the video for the <video> element: the file itself when
// the browser plays it, else a small H.264 copy made with FFmpeg.
func (s *server) clipMedia(w http.ResponseWriter, r *http.Request, t *ToolJob, i int) {
	f := t.file(i)
	if browserPlays(f.Format) {
		s.serveSource(w, r, t, i)
		return
	}
	t.mu.Lock()
	cs := t.clips[i]
	if cs == nil {
		cs = &clipSession{}
		t.clips[i] = cs
	}
	t.mu.Unlock()
	cs.proxyMu.Lock()
	if cs.proxy == "" && cs.proxyErr == "" {
		cs.proxy, cs.proxyErr = makeProxy(r.Context(), f.Path)
	}
	proxy, perr := cs.proxy, cs.proxyErr
	cs.proxyMu.Unlock()
	if proxy == "" {
		writeError(w, http.StatusUnprocessableEntity, errors.New(perr))
		return
	}
	fh, err := os.Open(proxy)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer fh.Close()
	fi, _ := fh.Stat()
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, "", fi.ModTime(), fh)
}

func makeProxy(ctx context.Context, path string) (string, string) {
	exe := ffmpegPath()
	if exe == "" {
		return "", "The browser can't play this video and FFmpeg is not installed to make a preview."
	}
	dir, err := os.MkdirTemp("", "filee-clip")
	if err != nil {
		return "", err.Error()
	}
	out := filepath.Join(dir, "preview.mp4")
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
	defer cancel()
	_, err = tools.Run(ctx, tools.Cmd{Path: exe, Args: []string{"-hide_banner", "-y", "-i", path, "-map", "0:v:0", "-map", "0:a:0?",
		"-vf", "scale=-2:'min(720,ih)'", "-c:v", "libx264", "-preset", "veryfast", "-crf", "28", "-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart", out}})
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err.Error()
	}
	return out, ""
}
