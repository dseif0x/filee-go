package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/app"
	"github.com/dseif0x/filee-go/internal/convert"
)

type fake struct{}

func (fake) ID() string   { return "magick" }
func (fake) Name() string { return "ImageMagick" }
func (fake) Edges() []convert.Edge {
	return []convert.Edge{convert.E("jpg", "png"), convert.E("jpg", "webp")}
}
func (fake) MaxParallelism() int    { return 0 }
func (fake) Status() convert.Status { return convert.Available("", "7.1") }
func (fake) Convert(_ context.Context, st *convert.Step, _ convert.Progress) ([]string, error) {
	if strings.Contains(st.InputPath, "bad") {
		return nil, convert.Refuse("error.empty_file", "")
	}
	p := st.Output.Allocate(st.To, "")
	if p == "" {
		return nil, nil
	}
	return []string{p}, os.WriteFile(p, []byte(st.To), 0o644)
}

func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	data := t.TempDir()
	var out, errs bytes.Buffer
	code := run(context.Background(), args, &out, &errs, func() (*app.App, error) {
		return app.New(app.Options{DataDir: data, Catalog: convert.NewCatalog(fake{}), WorkRoot: t.TempDir()})
	})
	return code, out.String(), errs.String()
}

func TestOptions(t *testing.T) {
	cases := map[string]string{
		"frobnicate":                     "Unknown command 'frobnicate'.",
		"convert":                        "Give at least one file or folder to convert.",
		"convert a.jpg":                  "Use either --to <format> or --preset <preset>.",
		"convert a.jpg --to png -p x":    "Use either --to <format> or --preset <preset>.",
		"convert a.jpg --to png -q 101":  "--quality must be a number from 1 to 100.",
		"convert a.jpg --to png --bogus": "Unknown option '--bogus'.",
		"watch a b --to png":             "Give exactly one folder to watch.",
		"watch a --to png --settle x":    "--settle must be a number of seconds.",
		"convert a.jpg --to png -q 80":   "",
	}
	for line, want := range cases {
		if got := parseOptions(strings.Fields(line)).err; got != want {
			t.Errorf("%q: %q", line, got)
		}
	}
	if o := parseOptions([]string{"convert", "--help"}); o.command != "help" || o.inputs[0] != "convert" {
		t.Fatal(o)
	}
}

func TestConvertPrintsEveryFileAndExits(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.jpg")
	bad := filepath.Join(dir, "bad.jpg")
	_ = os.WriteFile(good, []byte("x"), 0o644)
	_ = os.WriteFile(bad, []byte("x"), 0o644)
	out := filepath.Join(dir, "out")
	code, stdout, _ := cli(t, "convert", good, bad, "--to", "png", "-o", out)
	if code != exitSomeFailed || !strings.Contains(stdout, "ok    ") || !strings.Contains(stdout, "fail  ") ||
		!strings.Contains(stdout, "Converted 1 of 2 file(s), 1 failed.") {
		t.Fatal(code, stdout)
	}
	if _, err := os.Stat(filepath.Join(out, "a.png")); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = cli(t, "convert", filepath.Join(dir, "*.jpg"), "--preset", "to-webp", "--json")
	var doc struct {
		Converted, Failed int
		Files             []struct{ State, Error string }
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != exitSomeFailed || doc.Converted != 1 || doc.Failed != 1 || doc.Files[1].Error == "" {
		t.Fatal(code, stdout, err)
	}
	code, _, _ = cli(t, "convert", good, "--to", "png", "--skip", "-o", out, "--quiet")
	if code != exitSuccess {
		t.Fatal(code)
	}
	if code, _, errs := cli(t, "convert", filepath.Join(dir, "nope.jpg"), "--to", "png"); code != exitUsage || !strings.Contains(errs, "not found") {
		t.Fatal(code, errs)
	}
	empty := t.TempDir()
	if code, _, _ := cli(t, "convert", empty, "--to", "png"); code != exitNothing {
		t.Fatal(code)
	}
	if code, _, errs := cli(t, "convert", good, "--preset", "rotate"); code != exitUsage || !strings.Contains(errs, "is a group") || !strings.Contains(errs, "rotate-left") {
		t.Fatal(errs)
	}
	if code, _, errs := cli(t, "convert", good, "--to", "raw"); code != exitUsage || !strings.Contains(errs, "can't write it") {
		t.Fatal(errs)
	}
}

func TestListings(t *testing.T) {
	if code, out, _ := cli(t, "formats", "jpeg"); code != 0 || !strings.Contains(out, "png, webp") {
		t.Fatal(out)
	}
	if code, out, _ := cli(t, "formats"); code != 0 || !strings.Contains(out, "Image:") || !strings.Contains(out, "(read only)") {
		t.Fatal(out)
	}
	if _, out, _ := cli(t, "presets"); !strings.Contains(out, "to-png") || strings.Contains(out, "tool-crop") {
		t.Fatal(out)
	}
	if _, out, _ := cli(t, "engines"); !strings.Contains(out, "magick") || !strings.Contains(out, "7.1") {
		t.Fatal(out)
	}
	if _, out, _ := cli(t, "help", "watch"); !strings.Contains(out, "--settle") {
		t.Fatal(out)
	}
	if code, _, _ := cli(t, "app"); code != exitUsage {
		t.Fatal("the stub GUI ran")
	}
}

func TestWatchConvertsUntilCancelled(t *testing.T) {
	dir := t.TempDir()
	data := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	var out, errs syncBuffer
	done := make(chan int)
	go func() {
		done <- run(ctx, []string{"watch", dir, "--to", "png", "--settle", "0.1", "--move-originals"}, &out, &errs, func() (*app.App, error) {
			return app.New(app.Options{DataDir: data, Catalog: convert.NewCatalog(fake{}), WorkRoot: t.TempDir()})
		})
	}()
	_ = os.WriteFile(filepath.Join(dir, "w.jpg"), []byte("x"), 0o644)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "originals", "w.jpg")); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatal(code, errs.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "converted", "w.png")); err != nil {
		t.Fatal(err, out.String())
	}
}
