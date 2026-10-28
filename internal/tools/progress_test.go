package tools

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestEstimateRisesTowardsItsBound(t *testing.T) {
	if v := EstimateValue(0, 2, 0.1, 0.9); v != 0.1 {
		t.Error(v)
	}
	if v := EstimateValue(2, 2, 0, 1); v < 0.63 || v > 0.64 {
		t.Error(v)
	}
	if v := EstimateValue(1e9, 2, 0, 0.9); v > 0.9 {
		t.Error(v)
	}
	var got atomic.Int64
	e := StartEstimate(func(float64) { got.Add(1) }, time.Second, 0, 1)
	time.Sleep(400 * time.Millisecond)
	e.Stop()
	e.Stop()
	if got.Load() == 0 {
		t.Error("no reports")
	}
	StartEstimate(nil, time.Second, 0, 1).Stop()
}

func TestTransferRate(t *testing.T) {
	r := NewTransferRate(5*time.Second, 0)
	r.Add(0, 0)
	if _, ok := r.Remaining(0, 100); ok {
		t.Error("speed before it settled")
	}
	r.Add(time.Second, 1000)
	if r.BytesPerSecond() != 1000 {
		t.Error(r.BytesPerSecond())
	}
	for i := 2; i <= 20; i++ {
		r.Add(time.Duration(i)*time.Second, int64(i)*2000-2000)
	}
	if s := r.BytesPerSecond(); s != 2000 {
		t.Error("the window forgets the slow start", s)
	}
	if d, ok := r.Remaining(38000, 42000); !ok || d != 2*time.Second {
		t.Error(d)
	}
}

func TestStallWatch(t *testing.T) {
	w := NewStallWatch(50 * time.Millisecond)
	if w.Stalled() {
		t.Error("stalled at once")
	}
	time.Sleep(80 * time.Millisecond)
	if !w.Stalled() {
		t.Error("not stalled")
	}
	w.Touch()
	if w.Stalled() {
		t.Error("touch")
	}
}

func TestRunStopsSilentAndLongPrograms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	start := time.Now()
	_, err = Run(context.Background(), Cmd{Path: sh, Args: []string{"-c", "sleep 5"}, StallTimeout: 300 * time.Millisecond})
	var stalled *StalledError
	if !errors.As(err, &stalled) || time.Since(start) > 3*time.Second {
		t.Error("stall:", err)
	}
	_, err = Run(context.Background(), Cmd{Path: sh, Args: []string{"-c", "sleep 5"}, Timeout: 300 * time.Millisecond})
	var timeout *TimeoutError
	if !errors.As(err, &timeout) {
		t.Error("timeout:", err)
	}
	// A chatty program is not stalled.
	var lines atomic.Int64
	res, err := Run(context.Background(), Cmd{Path: sh, Args: []string{"-c", "for i in 1 2 3 4 5; do echo $i; sleep 0.1; done"},
		StallTimeout: 400 * time.Millisecond, LowPriority: true, OnStdoutLine: func(string) { lines.Add(1) }})
	if err != nil || lines.Load() != 5 || res.ExitCode != 0 {
		t.Error(err, lines.Load())
	}
	_, err = Run(context.Background(), Cmd{Path: sh, Args: []string{"-c", "echo oops >&2; exit 3"}})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 3 || exit.Stderr != "oops\n" {
		t.Error(err)
	}
	if DescribeLimit(10*time.Minute) != "10 min" || DescribeLimit(30*time.Second) != "30 s" {
		t.Error("describe")
	}
}

func TestFolderWithPrograms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec bits")
	}
	dir := t.TempDir()
	if FolderWithPrograms(dir, "ffmpeg") != "" {
		t.Error("empty folder")
	}
	mustWrite(t, dir+"/bin/ffmpeg")
	mustWrite(t, dir+"/bin/ffprobe")
	if FolderWithPrograms(dir, "ffmpeg", "ffprobe") != dir+"/bin" || FolderWithPrograms(dir+"/bin/ffmpeg", "ffprobe") != dir+"/bin" {
		t.Error("bin folder")
	}
	if Program(dir+"/bin", "ffprobe") == "" || Program(dir, "ffprobe") != "" {
		t.Error("program")
	}
	mustWrite(t, dir+"/gs/gs10.01/bin/gs")
	mustWrite(t, dir+"/gs/gs10.04/bin/gs")
	d := expandDirs([]string{dir + "/gs/gs*/bin"})
	if len(d) != 2 || d[0] != dir+"/gs/gs10.04/bin" {
		t.Error("newest version first", d)
	}
	t.Setenv("FILEE_ENGINES_DIR", dir)
	Forget()
	if Find(Spec{Names: []string{"gs"}, Dirs: map[string][]string{runtime.GOOS: {"gs/gs*/bin"}}}) != dir+"/gs/gs10.04/bin/gs" {
		t.Error("find below the engines folder")
	}
	Forget()
}
