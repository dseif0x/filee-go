package webui

import (
	"context"
	"errors"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"

	"github.com/dseif0x/filee-go/internal/history"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/presets"
)

// ---- crop ----

type cropRequest struct {
	// X, Y, Width, Height are parts (0–1) of the upright picture or page.
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (s *server) crop(w http.ResponseWriter, r *http.Request, t *ToolJob, i int) {
	var in cropRequest
	if !readJSON(w, r, &in) {
		return
	}
	area, ok := presets.ClampArea(presets.HiddenArea{X: in.X, Y: in.Y, Width: in.Width, Height: in.Height})
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("the frame is empty"))
		return
	}
	f := t.file(i)
	t.mu.Lock()
	p := t.Preset.Clone()
	t.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	out, key, detail := s.cropFile(ctx, f.Path, f.Format, area, p)
	t.setFile(i, func(tf *ToolFile) {
		switch {
		case key == "toast.skipped":
			tf.State, tf.Message = "skipped", s.a.Text(key)
		case key != "":
			tf.State, tf.Message = "failed", s.a.Text(key)
			if detail != "" {
				tf.Message += " (" + detail + ")"
			}
		default:
			tf.State, tf.Outputs, tf.Message = "done", []string{out}, ""
		}
	})
	if key == "" {
		s.record(p, t.Name, []string{f.Path}, []string{out})
	}
	s.advance(t, i)
	writeJSON(w, t.snapshot())
}

// cropFile crops one picture (or every page of a PDF) as the preset's output
// rule says; key is the error key ("" when it worked, "toast.skipped" when
// the conflict rule skips it).
func (s *server) cropFile(ctx context.Context, path, format string, a presets.HiddenArea, p *presets.Preset) (out, key, detail string) {
	applied := s.a.Settings().WithDefaultOutput(p)
	ext := filepath.Ext(path)
	if len(ext) > 0 {
		ext = ext[1:]
	}
	tokens := presets.Tokens{SourcePath: path, PresetName: s.a.Texts().PresetName(applied), Index: 1, Now: time.Now()}
	out = presets.Resolve(&applied.Output, tokens, ext, nil, "")
	if out == "" {
		return "", "toast.skipped", filepath.Base(path)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", "error.conversion_failed", err.Error()
	}
	var err error
	if format == "pdf" {
		err = cropPdf(ctx, path, out, a)
	} else {
		err = s.cropImage(ctx, path, format, out, a)
	}
	if err != nil {
		_ = os.Remove(out)
		if errors.Is(err, errUnsupported) {
			return "", "error.unsupported_source", ""
		}
		return "", "error.conversion_failed", err.Error()
	}
	if applied.Output.KeepDates {
		if fi, err := os.Stat(path); err == nil {
			_ = os.Chtimes(out, fi.ModTime(), fi.ModTime())
		}
	}
	return out, "", ""
}

var errUnsupported = errors.New("unsupported")

// cropImage crops a picture Go reads, written in its own format when Go
// writes it, else through the engines from a PNG.
func (s *server) cropImage(ctx context.Context, path, format, out string, a presets.HiddenArea) error {
	if format == "gif" || format == "png" && isAnimatedPNG(path) {
		if n := gifFrames(path); n > 1 || format == "png" {
			// Multi-frame pictures can't be cropped interactively.
			return errUnsupported
		}
	}
	img, err := decodeFile(path)
	if err != nil {
		img, err = s.decodeVia(ctx, path)
		if err != nil {
			return err
		}
	}
	b := img.Bounds()
	x, y, w, h, ok := presets.PixelBounds(a, b.Dx(), b.Dy())
	if !ok {
		return errors.New("the frame is outside the picture")
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), img, image.Pt(b.Min.X+x, b.Min.Y+y), draw.Src)
	if enc := encoderFor(format); enc != nil {
		return writeImage(out, dst, enc)
	}
	// Written as PNG, then converted to the original's format by the engines.
	tmp, err := os.MkdirTemp("", "filee-crop")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	mid := filepath.Join(tmp, "crop.png")
	if err := writeImage(mid, dst, encoderFor("png")); err != nil {
		return err
	}
	outs, cleanup, err := s.conv.convertTo(ctx, mid, format, "")
	if err != nil {
		return err
	}
	defer cleanup()
	return moveFile(outs[0], out)
}

func (s *server) decodeVia(ctx context.Context, path string) (image.Image, error) {
	outs, cleanup, err := s.conv.convertTo(ctx, path, "png", "1")
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return decodeFile(outs[0])
}

type encodeFunc func(f *os.File, img image.Image) error

func encoderFor(format string) encodeFunc {
	switch format {
	case "png":
		return func(f *os.File, img image.Image) error { return png.Encode(f, img) }
	case "jpg":
		return func(f *os.File, img image.Image) error { return jpeg.Encode(f, img, &jpeg.Options{Quality: 95}) }
	case "gif":
		return func(f *os.File, img image.Image) error { return gif.Encode(f, img, nil) }
	case "bmp":
		return func(f *os.File, img image.Image) error { return bmp.Encode(f, img) }
	case "tiff":
		return func(f *os.File, img image.Image) error {
			return tiff.Encode(f, img, &tiff.Options{Compression: tiff.Deflate})
		}
	}
	return nil
}

func writeImage(path string, img image.Image, enc encodeFunc) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := enc(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, b, 0o644)
}

func gifFrames(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return 0
	}
	return len(g.Image)
}

// isAnimatedPNG: the PNG has an acTL chunk before its image data.
func isAnimatedPNG(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil || len(b) < 8 {
		return false
	}
	for i := 8; i+8 <= len(b); {
		n := int(uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3]))
		typ := string(b[i+4 : i+8])
		if typ == "acTL" {
			return true
		}
		if typ == "IDAT" || n < 0 {
			return false
		}
		i += 12 + n
	}
	return false
}

// record adds a finished tool result to the history (as the original's crop
// and page editor do: they don't run as jobs).
func (s *server) record(p *presets.Preset, name string, sources, outputs []string) {
	e := &history.Entry{
		JobID: newID(), FinishedAt: time.Now(), PresetID: p.ID, PresetName: name, State: jobs.JobCompleted,
		Sources: sources, Outputs: outputs, Failures: []history.Failure{}, Undoable: []history.Output{},
	}
	for _, o := range outputs {
		if fi, err := os.Stat(o); err == nil && !fi.IsDir() {
			e.Undoable = append(e.Undoable, history.Output{Path: o, Length: fi.Size(), WrittenUtc: fi.ModTime().UTC()})
		}
	}
	if err := s.a.RecordHistory(e); err != nil {
		s.log.Warn("could not save the history", "error", err)
	}
}

func newID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// ---- pixelate / blur ----

type hideRequest struct {
	Areas    []presets.HiddenArea `json:"areas"`
	Style    presets.HideStyle    `json:"style"`
	Strength int                  `json:"strength"`
}

func (s *server) hide(w http.ResponseWriter, r *http.Request, t *ToolJob, i int) {
	var in hideRequest
	if !readJSON(w, r, &in) {
		return
	}
	if in.Style != presets.HideBlur {
		in.Style = presets.HidePixelate
	}
	s.remember(t, func(p *presets.Preset) { presets.RememberHide(p, in.Style, in.Strength) })
	f := t.file(i)
	t.mu.Lock()
	conv := presets.HideConversionOf(t.Preset, f.Target, in.Areas)
	t.mu.Unlock()
	if len(conv.Image.HiddenAreas) == 0 {
		writeError(w, http.StatusBadRequest, errors.New(s.a.Text("hide.empty_hint")))
		return
	}
	j := s.a.ConvertWith([]string{f.Path}, conv, t.Name, nil)
	s.track(t, i, j.ID())
	s.advance(t, i)
	writeJSON(w, t.snapshot())
}

// ---- edit pictures ----

type imageEditRequest struct {
	Target string               `json:"target"`
	Image  presets.ImageOptions `json:"image"`
}

// imageEditChoices are the formats offered and the first picture's size.
func (s *server) imageEditChoices(t *ToolJob) map[string]any {
	t.mu.Lock()
	files := slices.Clone(t.Files)
	t.mu.Unlock()
	var sources []string
	for _, f := range files {
		sources = append(sources, f.Format)
	}
	out := map[string]any{
		"targets": presets.ImageEditTargetsFrom(sources, s.a.Planner()), "lossy": presets.ImageEditLossyTargets,
		"resizeModes": presets.ImageEditResizeModes,
	}
	if w0, h0, ok := imageSize(files[0].Path); ok {
		out["width"], out["height"] = w0, h0
	}
	keepsLossy := false
	for _, f := range files {
		if slices.Contains(presets.ImageEditLossyTargets, f.Format) && presets.ImageEditTargetFor(f.Format, presets.SameAsSource, s.a.Planner()) == presets.SameAsSource {
			keepsLossy = true
		}
	}
	out["keepsLossy"] = keepsLossy
	return out
}

func (s *server) imageEdit(w http.ResponseWriter, r *http.Request, t *ToolJob) {
	if r.URL.Query().Get("choices") == "1" {
		writeJSON(w, s.imageEditChoices(t))
		return
	}
	var in imageEditRequest
	if !readJSON(w, r, &in) {
		return
	}
	if in.Target == "" {
		in.Target = presets.SameAsSource
	}
	img := in.Image
	img.ResizePercent = clampInt(img.ResizePercent, 1, 1000)
	img.LongEdge = clampInt(img.LongEdge, 16, 20000)
	img.Width = clampInt(img.Width, 16, 20000)
	img.Quality = clampInt(img.Quality, 1, 100)
	s.remember(t, func(p *presets.Preset) { presets.RememberImageEdit(p, in.Target, img) })
	t.mu.Lock()
	files := slices.Clone(t.Files)
	tool := t.Preset.Clone()
	t.mu.Unlock()
	// One conversion for each format the pictures are written in.
	groups := map[string][]int{}
	var order []string
	planner := s.a.Planner()
	for k, f := range files {
		target := presets.ImageEditTargetFor(f.Format, in.Target, planner)
		if _, ok := groups[target]; !ok {
			order = append(order, target)
		}
		groups[target] = append(groups[target], k)
	}
	for _, target := range order {
		var paths []string
		for _, k := range groups[target] {
			paths = append(paths, files[k].Path)
		}
		j := s.a.ConvertWith(paths, presets.ImageEditConversionOf(tool, target, img), t.Name, nil)
		for _, k := range groups[target] {
			t.setFile(k, func(f *ToolFile) { f.Target = target })
			s.track(t, k, j.ID())
		}
	}
	writeJSON(w, t.snapshot())
}

func clampInt(v, lo, hi int) int { return max(lo, min(v, hi)) }

var _ = math.Round
