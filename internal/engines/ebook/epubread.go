package ebook

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// EPUB 2 / 3 → Book: META-INF/container.xml → the OPF package → spine →
// XHTML chapters (each starts a new page), pictures from the package, title /
// authors / language / cover from the metadata. Books with DRM (Adobe
// ADEPT, Apple FairPlay, …) are refused; font obfuscation is not DRM.

// fontObfuscation algorithms only obfuscate embedded fonts (IDPF and Adobe).
var fontObfuscation = map[string]bool{
	"http://www.idpf.org/2008/embedding": true,
	"http://ns.adobe.com/pdf/enc#RC":     true,
}

func openEpubZip(path string) (*zip.ReadCloser, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, errors.New("this is not a valid EPUB file (it is not a ZIP package)")
	}
	return z, nil
}

func readEpubBook(ctx context.Context, path, work string) (*Book, error) {
	folder, err := newFolder(work, "epub")
	if err != nil {
		return nil, err
	}
	z, err := openEpubZip(path)
	if err != nil {
		return nil, err
	}
	if err := checkDRM(&z.Reader); err != nil {
		z.Close()
		return nil, err
	}
	err = extractZip(&z.Reader, folder)
	z.Close()
	if err != nil {
		return nil, err
	}
	rootFile, err := opfRootFile(folder)
	if err != nil {
		return nil, err
	}
	pkg, err := loadOpf(rootFile)
	if err != nil {
		return nil, err
	}
	var chapters []string
	for _, it := range pkg.spine {
		switch {
		case it.MediaType == "application/xhtml+xml" || it.MediaType == "text/html" || it.MediaType == "application/xml" ||
			it.MediaType == "" && strings.HasSuffix(strings.ToLower(it.Path), "html"):
			chapters = append(chapters, it.Path)
		case strings.HasPrefix(it.MediaType, "image/"):
			// A picture in the spine (comics, covers) is a page of its own.
			if p, err := imagePage(it.Path); err == nil {
				chapters = append(chapters, p)
			}
		}
	}
	if len(chapters) == 0 {
		return nil, errors.New("the EPUB has no readable chapters")
	}
	d, err := readChapters(ctx, chapters, filepath.Join(folder, "~media"))
	if err != nil {
		return nil, err
	}
	if pkg.meta.Title != "" {
		d.Title = pkg.meta.Title
	}
	meta := pkg.meta
	return &Book{d, &meta}, nil
}

// checkDRM refuses encrypted content: anything in encryption.xml except font obfuscation, or Apple's sinf.xml.
func checkDRM(z *zip.Reader) error {
	var enc *zip.File
	for _, f := range z.File {
		switch f.Name {
		case "META-INF/sinf.xml":
			return errDRM
		case "META-INF/encryption.xml":
			enc = f
		}
	}
	if enc == nil {
		return nil
	}
	rc, err := enc.Open()
	if err != nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(rc, 16<<20))
	rc.Close()
	root, _, err := parseXML(b)
	if err != nil {
		return nil // unreadable: the chapters will tell whether they are readable
	}
	for _, m := range root.DescendantsLocal("EncryptionMethod") {
		if !fontObfuscation[m.A("Algorithm")] {
			return errDRM
		}
	}
	return nil
}

// imagePage writes an XHTML page next to a picture that the spine lists directly.
func imagePage(image string) (string, error) {
	page := filepath.Join(filepath.Dir(image), "~"+strings.TrimSuffix(filepath.Base(image), filepath.Ext(image))+"-"+randomHex(8)+".xhtml")
	body := `<html><body><p style="text-align:center"><img src="` + url.PathEscape(filepath.Base(image)) + `"/></p></body></html>`
	return page, os.WriteFile(page, []byte(body), 0o644)
}
