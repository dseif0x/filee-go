package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/tools"
)

// FFmpeg reports its progress every half second while it works, so this long
// without a single line means it is stuck (e.g. on a stalled network share).
// Unlike other programs there is no overall timeout: a film legitimately
// encodes for an hour.
const DefaultStallTimeout = 10 * time.Minute

// QueryTimeout bounds ffmpeg -version / -encoders: a copy that does not
// answer at once is broken or hangs.
const QueryTimeout = 10 * time.Second

// frameTimeKey is the key of the per-frame stats line the GIF palette pass writes.
const frameTimeKey = "frame_time"

const keptErrorLines = 40

// RunResult is the exit code of an FFmpeg run and the last lines it wrote to stderr.
type RunResult struct {
	ExitCode int
	Stderr   string
}

// RunFfmpeg runs ffmpeg with args and waits; onPosition gets the output
// position in seconds whenever FFmpeg reports one. A run silent for longer
// than stall (DefaultStallTimeout when 0) is killed. A non-zero exit is not
// an error here: the caller reads RunResult.
func RunFfmpeg(ctx context.Context, ffmpeg string, args []string, onPosition func(float64), stall time.Duration) (RunResult, error) {
	if stall <= 0 {
		stall = DefaultStallTimeout
	}
	var mu sync.Mutex
	var lines []string
	res, err := tools.Run(ctx, tools.Cmd{
		Path: ffmpeg, Args: args, Dir: filepath.Dir(ffmpeg),
		// SVT-AV1 logs its settings to stderr, where they would bury FFmpeg's error message.
		Env:          []string{"SVT_LOG=1"},
		StallTimeout: stall, LowPriority: true,
		OnStdoutLine: func(l string) {
			if onPosition != nil {
				if s, ok := ParsePosition(l); ok {
					onPosition(s)
				}
			}
		},
		OnStderrLine: func(l string) {
			mu.Lock()
			lines = append(lines, l)
			if len(lines) > keptErrorLines {
				lines = lines[1:]
			}
			mu.Unlock()
		},
	})
	out := RunResult{}
	if res != nil {
		out.ExitCode = res.ExitCode
	}
	mu.Lock()
	out.Stderr = strings.Join(lines, "\n")
	mu.Unlock()
	var exit *tools.ExitError
	if errors.As(err, &exit) {
		return out, nil
	}
	var stalled *tools.StalledError
	if errors.As(err, &stalled) {
		return out, fmt.Errorf("FFmpeg stopped responding (no progress for %s) and was stopped", tools.DescribeLimit(stall))
	}
	return out, err
}

// runChecked runs ffmpeg and turns a failure into an error with FFmpeg's reason.
func runChecked(ctx context.Context, ffmpeg string, args []string, onPosition func(float64), stall time.Duration) error {
	r, err := RunFfmpeg(ctx, ffmpeg, args, onPosition, stall)
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return failure(r)
	}
	return nil
}

func failure(r RunResult) error {
	if reason := Summarize(r.Stderr); reason != "" {
		return fmt.Errorf("FFmpeg failed (exit code %d): %s", r.ExitCode, reason)
	}
	return fmt.Errorf("FFmpeg failed (exit code %d).", r.ExitCode)
}

// runBriefly runs a short query with a time limit; stdout is returned.
func runBriefly(exe string, args []string, timeout time.Duration) (int, string, error) {
	res, err := tools.Run(context.Background(), tools.Cmd{Path: exe, Args: args, Timeout: timeout})
	var exit *tools.ExitError
	if errors.As(err, &exit) {
		return res.ExitCode, string(res.Stdout), nil
	}
	if err != nil {
		return -1, "", err
	}
	return res.ExitCode, string(res.Stdout), nil
}

// ParsePosition reads the position in seconds from one line of -progress
// output (out_time_us=2000000) or of the palette pass' frame stats
// (frame_time=2.5); ok is false for every other line.
func ParsePosition(line string) (float64, bool) {
	key, value, found := strings.Cut(line, "=")
	if !found || key == "" {
		return 0, false
	}
	value = strings.TrimSpace(value)
	switch key {
	case "out_time_us":
		us, err := strconv.ParseInt(value, 10, 64)
		if err != nil || us < 0 {
			return 0, false // "N/A" before the first frame
		}
		return float64(us) / 1e6, true
	case frameTimeKey:
		s, err := strconv.ParseFloat(value, 64)
		if err != nil || s < 0 || s != s || s > 1e300 {
			return 0, false
		}
		return s, true
	}
	return 0, false
}

// Summarize shortens FFmpeg's error output: its last three distinct lines, without log prefixes.
func Summarize(stderr string) string {
	var lines []string
	seen := map[string]bool{}
	for _, l := range strings.Split(stderr, "\n") {
		l = stripPrefix(strings.TrimSpace(l))
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		lines = append(lines, l)
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " ")
}

// stripPrefix: "[aost#0:1/wmav2 @ 0000012a] too many channels" → "too many channels".
func stripPrefix(line string) string {
	for strings.HasPrefix(line, "[") {
		end := strings.Index(line, "] ")
		if end <= 0 {
			break
		}
		line = strings.TrimLeft(line[end+2:], " \t")
	}
	return line
}

var versionPattern = regexp.MustCompile(`version\s+n?(\d+(?:\.\d+)+|\S+)`)

type cached[T any] struct {
	mu sync.Mutex
	m  map[string]T
}

func (c *cached[T]) get(key string, f func() T) T {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.m[key]; ok {
		return v
	}
	if c.m == nil {
		c.m = map[string]T{}
	}
	v := f()
	c.m[key] = v
	return v
}

// cacheKey is the executable with its modification time: an update is noticed.
func cacheKey(exe string) string {
	t := int64(0)
	if st, err := os.Stat(exe); err == nil {
		t = st.ModTime().UnixNano()
	}
	return exe + "|" + strconv.FormatInt(t, 10)
}

type versionResult struct {
	version string
	err     error
}

var versions cached[versionResult]
var encoderSets, filterSets cached[map[string]bool]

// VersionOf is "9.0.2" from ffmpeg -version, cached per executable.
func VersionOf(ffmpeg string) (string, error) {
	r := versions.get(cacheKey(ffmpeg), func() versionResult {
		code, out, err := runBriefly(ffmpeg, []string{"-hide_banner", "-version"}, QueryTimeout)
		if err != nil {
			var t *tools.TimeoutError
			if errors.As(err, &t) {
				return versionResult{err: errors.New("FFmpeg did not start in time.")}
			}
			return versionResult{err: err}
		}
		if code != 0 {
			return versionResult{err: fmt.Errorf("FFmpeg does not start (exit code %d).", code)}
		}
		first, _, _ := strings.Cut(out, "\n")
		if m := versionPattern.FindStringSubmatch(first); m != nil {
			return versionResult{version: m[1]}
		}
		return versionResult{version: strings.TrimSpace(first)}
	})
	return r.version, r.err
}

// EncodersOf lists the encoders of ffmpeg (-encoders), cached; nil (assume
// the usual build) when the list can't be read.
func EncodersOf(ffmpeg string) map[string]bool {
	return encoderSets.get(cacheKey(ffmpeg), func() map[string]bool {
		code, out, err := runBriefly(ffmpeg, []string{"-hide_banner", "-encoders"}, QueryTimeout)
		if err != nil || code != 0 {
			return nil
		}
		return ParseEncoders(out)
	})
}

// HasFilter is true when ffmpeg has the filter name (assumed when the list can't be read).
func HasFilter(ffmpeg, name string) bool {
	set := filterSets.get(cacheKey(ffmpeg), func() map[string]bool {
		code, out, err := runBriefly(ffmpeg, []string{"-hide_banner", "-filters"}, QueryTimeout)
		if err != nil || code != 0 {
			return nil
		}
		return ParseFilters(out)
	})
	return set == nil || set[name]
}

// ParseEncoders reads the names in ffmpeg -encoders output
// (" V....D libx265  libx265 H.265 / HEVC (codec hevc)").
func ParseEncoders(list string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(list, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && len(f[0]) == 6 && (f[0][0] == 'V' || f[0][0] == 'A' || f[0][0] == 'S') {
			out[f[1]] = true
		}
	}
	return out
}

// ParseFilters reads the names in ffmpeg -filters output (" .S zscale  V->V  Apply resizing, …").
func ParseFilters(list string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(list, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || (len(f[0]) != 2 && len(f[0]) != 3) || !strings.Contains(f[2], "->") {
			continue
		}
		flags := true
		for _, c := range f[0] {
			if c != '.' && c != 'T' && c != 'S' && c != 'C' {
				flags = false
			}
		}
		if flags {
			out[f[1]] = true
		}
	}
	return out
}
