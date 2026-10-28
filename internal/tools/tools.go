// Package tools finds and runs the external programs some engines use
// (FFmpeg, LibreOffice, Pandoc, Ghostscript, Tesseract, calibre, 7-Zip,
// rhwp). They are found on PATH, in Filee's own engine folder, and in the
// usual install places of each OS.
package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EnginesDir is where Filee installs downloaded engines
// (<user config dir>/filee/engines); overridable with FILEE_ENGINES_DIR.
func EnginesDir() string {
	if d := os.Getenv("FILEE_ENGINES_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "filee", "engines")
}

// Spec describes how to find one program.
type Spec struct {
	// Names are executable names tried on PATH and in the folders (without .exe).
	Names []string
	// Env names an environment variable holding the full path (e.g. FILEE_FFMPEG).
	Env string
	// Dirs are extra folders to look in, per GOOS ("darwin", "linux", "windows");
	// relative ones are below EnginesDir.
	Dirs map[string][]string
	// Package is the engine package id ("ffmpeg", "libreoffice", …): a copy
	// the user picked for it in Settings › Engines (SetOwnCopies) is used
	// first, as the C# EngineEnvironment.OwnCopies.
	Package string
	// Requires are programs the user's own copy must have next to the found
	// one (ffprobe beside ffmpeg).
	Requires []string
}

var (
	mu        sync.Mutex
	cache     = map[string]string{}
	ownCopies = map[string]string{}
)

// SetOwnCopies sets the user's own copies of engine packages (package id →
// the program or folder they picked; Settings › Engines). When they changed
// the cached finds are forgotten, and it reports true: re-check the engines then.
func SetOwnCopies(copies map[string]string) bool {
	mu.Lock()
	defer mu.Unlock()
	if maps.Equal(copies, ownCopies) || len(copies) == 0 && len(ownCopies) == 0 {
		return false
	}
	ownCopies = maps.Clone(copies)
	if ownCopies == nil {
		ownCopies = map[string]string{}
	}
	cache = map[string]string{}
	return true
}

// OwnCopy is the program of the user's own copy of s's package, or "" when
// none is set or it lacks the programs.
func OwnCopy(s Spec) string {
	if s.Package == "" {
		return ""
	}
	mu.Lock()
	path := ownCopies[s.Package]
	mu.Unlock()
	if path == "" {
		return ""
	}
	for _, n := range s.Names {
		if f := FolderWithPrograms(path, append([]string{n}, s.Requires...)...); f != "" {
			return Program(f, n)
		}
	}
	return ""
}

// Find returns the full path of the program, or "" when it is not installed:
// the FILEE_* variable, the user's own copy (Spec.Package), Filee's engines
// folder and the usual install places, then the PATH. Results are cached
// until Forget.
func Find(s Spec) string {
	key := strings.Join(s.Names, "|") + "|" + s.Env + "|" + s.Package
	mu.Lock()
	if p, ok := cache[key]; ok {
		mu.Unlock()
		return p
	}
	mu.Unlock()
	p := find(s)
	mu.Lock()
	cache[key] = p
	mu.Unlock()
	return p
}

// Forget clears the cache (after an engine was installed).
func Forget() {
	mu.Lock()
	cache = map[string]string{}
	mu.Unlock()
}

func find(s Spec) string {
	if s.Env != "" {
		if p := os.Getenv(s.Env); p != "" && isExec(p) {
			return p
		}
	}
	if p := OwnCopy(s); p != "" {
		return p
	}
	for _, d := range expandDirs(s.Dirs[runtime.GOOS]) {
		for _, n := range s.Names {
			for _, c := range candidates(filepath.Join(d, n)) {
				if isExec(c) {
					return c
				}
			}
		}
	}
	for _, n := range s.Names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	// Homebrew and MacPorts are not on the PATH of an app started from Finder.
	if runtime.GOOS == "darwin" {
		for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"} {
			for _, n := range s.Names {
				if p := filepath.Join(d, n); isExec(p) {
					return p
				}
			}
		}
	}
	return ""
}

// expandDirs resolves "~/" (home), relative folders (below EnginesDir) and
// glob patterns ("C:/Program Files/gs/gs*/bin", newest version first).
func expandDirs(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if strings.HasPrefix(d, "~/") {
			h, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			d = filepath.Join(h, d[2:])
		} else if !filepath.IsAbs(d) {
			d = filepath.Join(EnginesDir(), d)
		}
		if strings.ContainsAny(d, "*?[") {
			matches, _ := filepath.Glob(d)
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			out = append(out, matches...)
			continue
		}
		out = append(out, d)
	}
	return out
}

// FolderWithPrograms returns the folder holding every one of programs, given
// a program or a folder the user picked: that folder itself, or its bin,
// program or Contents/MacOS sub folder (FFmpeg builds, LibreOffice, .app
// bundles). "" when they are missing.
func FolderWithPrograms(path string, programs ...string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	start := path
	if !st.IsDir() {
		start = filepath.Dir(path)
	}
	for _, f := range []string{start, filepath.Join(start, "bin"), filepath.Join(start, "program"), filepath.Join(start, "Contents", "MacOS")} {
		ok := true
		for _, p := range programs {
			found := false
			for _, c := range candidates(filepath.Join(f, p)) {
				if isExec(c) {
					found = true
					break
				}
			}
			if !found {
				ok = false
				break
			}
		}
		if ok {
			return f
		}
	}
	return ""
}

// Program returns the path of name in folder (with .exe on Windows), or "".
func Program(folder, name string) string {
	for _, c := range candidates(filepath.Join(folder, name)) {
		if isExec(c) {
			return c
		}
	}
	return ""
}

func candidates(p string) []string {
	if runtime.GOOS == "windows" {
		return []string{p + ".exe", p + ".cmd", p + ".bat", p}
	}
	return []string{p}
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || st.Mode()&0o111 != 0
}

// Result of Run.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
}

// Cmd is one program run.
type Cmd struct {
	Path string
	Args []string
	Dir  string
	Env  []string // added to the current environment
	// Stdin is fed to the program when set.
	Stdin []byte
	// OnStderrLine / OnStdoutLine receive output lines as they come (progress parsing).
	OnStderrLine func(string)
	OnStdoutLine func(string)
	// Timeout is the longest the program may run (0: no limit); then it is
	// killed and Run returns *TimeoutError.
	Timeout time.Duration
	// StallTimeout kills a program that wrote nothing on either stream for this
	// long (0: never) and Run returns *StalledError: for programs that keep
	// reporting progress, so a long run goes on and a hung one stops.
	StallTimeout time.Duration
	// LowPriority runs the program below normal priority, so a long conversion
	// on every core leaves room for the desktop.
	LowPriority bool
}

// Run runs the program and waits; the process is killed when ctx ends. A
// non-zero exit is returned as *ExitError with the end of stderr.
func Run(ctx context.Context, c Cmd) (*Result, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.Timeout > 0 {
		var cancelT context.CancelFunc
		runCtx, cancelT = context.WithTimeout(runCtx, c.Timeout)
		defer cancelT()
	}
	cmd := exec.CommandContext(runCtx, c.Path, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	// A killed program may leave a child holding the pipes; don't wait for it forever.
	cmd.WaitDelay = 10 * time.Second
	hideWindow(cmd)
	killTree(cmd)
	if c.LowPriority {
		lowerAtStart(cmd)
	}
	if c.Stdin != nil {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	}
	var watch *StallWatch
	var touch func()
	if c.StallTimeout > 0 {
		watch = NewStallWatch(c.StallTimeout)
		touch = watch.Touch
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &lineWriter{buf: &out, fn: c.OnStdoutLine, touch: touch}
	cmd.Stderr = &lineWriter{buf: &errb, fn: c.OnStderrLine, touch: touch}
	err := cmd.Start()
	if err != nil {
		return &Result{}, err
	}
	if c.LowPriority {
		lowerAfterStart(cmd)
	}
	var stalled atomic.Bool
	if watch != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			t := time.NewTicker(min(time.Second, c.StallTimeout))
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					if watch.Stalled() {
						stalled.Store(true)
						cancel()
						return
					}
				}
			}
		}()
	}
	err = cmd.Wait()
	res := &Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if stalled.Load() {
		return res, &StalledError{Program: filepath.Base(c.Path), Limit: c.StallTimeout}
	}
	if runCtx.Err() != nil && c.Timeout > 0 {
		return res, &TimeoutError{Program: filepath.Base(c.Path), Limit: c.Timeout}
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && IsMissingRuntime(res.ExitCode) {
		return res, &RuntimeMissingError{Program: filepath.Base(c.Path), Code: res.ExitCode}
	}
	if errors.As(err, &ee) {
		return res, &ExitError{Program: filepath.Base(c.Path), Code: res.ExitCode, Stderr: tail(errb.String(), 2000)}
	}
	return res, err
}

// RuntimeMissingKey is the reason shown when Windows could not start a
// program because a DLL it needs (the Microsoft C++ runtime) is missing.
const RuntimeMissingKey = "error.runtime_missing"

// IsMissingRuntime: Windows refused to start the program because of a missing
// (0xC0000135) or outdated (0xC0000139) DLL.
func IsMissingRuntime(code int) bool {
	c := uint32(code)
	return c == 0xC0000135 || c == 0xC0000139
}

// RuntimeMissingError is a program Windows could not start for a missing DLL.
type RuntimeMissingError struct {
	Program string
	Code    int
}

func (e *RuntimeMissingError) Error() string {
	return fmt.Sprintf("%s, 0x%08X (a runtime file it needs is missing)", e.Program, uint32(e.Code))
}

// StalledErrorKey is the reason shown when a program stopped writing anything
// for its stall limit and was stopped.
const StalledErrorKey = "error.engine_stalled"

// StalledError is a program killed because it wrote nothing for Limit.
type StalledError struct {
	Program string
	Limit   time.Duration
}

func (e *StalledError) Error() string {
	return fmt.Sprintf("%s stopped responding (no output for %s) and was stopped", e.Program, DescribeLimit(e.Limit))
}

// TimeoutError is a program killed because it ran longer than Limit.
type TimeoutError struct {
	Program string
	Limit   time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s did not finish within %s", e.Program, DescribeLimit(e.Limit))
}

// DescribeLimit is "10 min", or "30 s" below a minute.
func DescribeLimit(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%.0f min", d.Minutes())
	}
	return fmt.Sprintf("%.0f s", d.Seconds())
}

// ExitError is a program that ended with a non-zero exit code.
type ExitError struct {
	Program string
	Code    int
	Stderr  string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s exited with %d: %s", e.Program, e.Code, strings.TrimSpace(e.Stderr))
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// lineWriter keeps all output and hands complete lines (split at \n or \r,
// as FFmpeg writes progress with \r) to fn.
type lineWriter struct {
	buf     *bytes.Buffer
	fn      func(string)
	touch   func()
	pending []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if w.touch != nil {
		w.touch()
	}
	if w.fn == nil {
		return len(p), nil
	}
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexAny(w.pending, "\r\n")
		if i < 0 {
			break
		}
		if i > 0 {
			w.fn(string(w.pending[:i]))
		}
		w.pending = w.pending[i+1:]
	}
	return len(p), nil
}
