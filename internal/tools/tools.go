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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
}

var (
	mu    sync.Mutex
	cache = map[string]string{}
)

// Find returns the full path of the program, or "" when it is not installed.
// Results are cached until Forget.
func Find(s Spec) string {
	key := strings.Join(s.Names, "|") + "|" + s.Env
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
	for _, d := range s.Dirs[runtime.GOOS] {
		if !filepath.IsAbs(d) {
			d = filepath.Join(EnginesDir(), d)
		}
		if strings.HasPrefix(d, "~/") {
			if h, err := os.UserHomeDir(); err == nil {
				d = filepath.Join(h, d[2:])
			}
		}
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
}

// Run runs the program and waits; the process is killed when ctx ends. A
// non-zero exit is returned as *ExitError with the end of stderr.
func Run(ctx context.Context, c Cmd) (*Result, error) {
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	hideWindow(cmd)
	if c.Stdin != nil {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &lineWriter{buf: &out, fn: c.OnStdoutLine}
	cmd.Stderr = &lineWriter{buf: &errb, fn: c.OnStderrLine}
	err := cmd.Run()
	res := &Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return res, &ExitError{Program: filepath.Base(c.Path), Code: res.ExitCode, Stderr: tail(errb.String(), 2000)}
	}
	return res, err
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
	pending []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
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
