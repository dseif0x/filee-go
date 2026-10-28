// Command macapp assembles Filee.app from darwin executables, on any OS:
//
//	go run ./scripts/macapp -version 1.2.3 -arm64 dist/filee-darwin-arm64 \
//	    -amd64 dist/filee-darwin-amd64 -icon internal/gui/filee.png -out dist/Filee.app [-zip dist/Filee.zip]
//
// The two executables are merged into one universal binary (lipo is not
// needed); Info.plist, the icon (.icns made from the PNG), the Services menu
// translations and entitlements.plist (for codesign, next to the app) are
// written. Signing is left to codesign on a Mac (see build-macos-app.sh).
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dseif0x/filee-go/internal/macos/bundle"
)

func main() {
	version := flag.String("version", "0.1.0-dev", "app version")
	arm64 := flag.String("arm64", "", "darwin/arm64 executable")
	amd64 := flag.String("amd64", "", "darwin/amd64 executable")
	icon := flag.String("icon", "", "square PNG for the app icon")
	out := flag.String("out", "dist/Filee.app", "the .app to write")
	zipPath := flag.String("zip", "", "also write the app into this .zip")
	flag.Parse()
	if err := build(*version, *arm64, *amd64, *icon, *out, *zipPath); err != nil {
		fmt.Fprintln(os.Stderr, "macapp:", err)
		os.Exit(1)
	}
}

func build(version, arm64, amd64, icon, out, zipPath string) error {
	var slices [][]byte
	for _, p := range []string{arm64, amd64} {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		slices = append(slices, b)
	}
	if len(slices) == 0 {
		return fmt.Errorf("give -arm64 and/or -amd64")
	}
	exe := slices[0]
	if len(slices) > 1 {
		var err error
		if exe, err = bundle.Universal(slices...); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	contents := filepath.Join(out, "Contents")
	write := func(rel string, data []byte, mode os.FileMode) error {
		p := filepath.Join(contents, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, data, mode)
	}
	if err := write("MacOS/Filee", exe, 0o755); err != nil {
		return err
	}
	if err := write("Info.plist", []byte(bundle.InfoPlist(version, "Filee")), 0o644); err != nil {
		return err
	}
	if err := write("PkgInfo", []byte("APPL????"), 0o644); err != nil {
		return err
	}
	for _, lang := range bundle.Localizations() {
		if err := write("Resources/"+lang+".lproj/ServicesMenu.strings", []byte(bundle.ServicesStrings(bundle.ServiceTitles[lang])), 0o644); err != nil {
			return err
		}
	}
	if icon != "" {
		f, err := os.Open(icon)
		if err != nil {
			return err
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			return err
		}
		icns, err := bundle.ICNS(img)
		if err != nil {
			return err
		}
		if err := write("Resources/filee.icns", icns, 0o644); err != nil {
			return err
		}
	}
	ent := filepath.Join(filepath.Dir(out), "entitlements.plist")
	if err := os.WriteFile(ent, []byte(bundle.Entitlements()), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d architecture(s), %.1f MB)\n", out, len(slices), float64(len(exe))/(1<<20))
	if zipPath != "" {
		return zipApp(out, zipPath)
	}
	return nil
}

// zipApp packs the app with its folder name, keeping the executable bit (ditto -c -k --keepParent does the same).
func zipApp(app, zipPath string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	parent := filepath.Dir(app)
	err = filepath.WalkDir(app, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(parent, p)
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = rel
		if d.IsDir() {
			h.Name += "/"
		} else {
			h.Method = zip.Deflate
		}
		w, err := zw.CreateHeader(h)
		if err != nil || d.IsDir() {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
	if err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	fmt.Println("wrote", zipPath)
	return nil
}
