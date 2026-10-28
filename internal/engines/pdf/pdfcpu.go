// pdfcpu (Apache-2.0) for everything that edits PDFs without redrawing them:
// copying, splitting, merging and turning pages, crop boxes, metadata,
// encryption and the finishing stamps.

package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func init() {
	// No configuration folder or font cache on the user's disk.
	api.DisableConfigDir()
}

// PdfcpuVersion names the library.
const PdfcpuVersion = "pdfcpu 0.16"

func pdfConf() *model.Configuration {
	c := model.NewDefaultConfiguration()
	c.ValidationMode = model.ValidationRelaxed
	return c
}

// isPasswordError tells a PDF that needs a password to open from others.
func isPasswordError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "password") || strings.Contains(s, "pdfcpu: please provide")
}

// readContext opens a PDF to read or change. One locked only against changes
// opens (pdfcpu decrypts it); one that needs a password to open fails with
// the message every PDF conversion gives.
func readContext(ctx context.Context, path string) (*model.Context, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pc, err := api.ReadContext(ctx, f, pdfConf())
	if err != nil {
		if isPasswordError(err) {
			return nil, ErrPasswordProtected
		}
		return nil, fmt.Errorf("could not read the PDF: %w", err)
	}
	if err := pc.EnsurePageCount(); err != nil {
		return nil, fmt.Errorf("could not read the PDF's pages: %w", err)
	}
	// The file is written decrypted (a finishing password encrypts it anew).
	pc.Encrypt, pc.EncKey, pc.E = nil, nil, nil
	// The document information stays as it was (pdfcpu would sign it as its producer).
	pc.PreserveInfoDict = true
	return pc, nil
}

// writeContext saves a PDF next to path and moves it into place, so a failed
// write leaves nothing half-written.
func writeContext(ctx context.Context, pc *model.Context, path string) error {
	tmp := path + ".tmp-" + filepath.Base(os.TempDir())
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = api.WriteContext(ctx, pc, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("could not write the PDF: %w", err)
	}
	return os.Rename(tmp, path)
}

// PageCount is the number of pages of a PDF.
func PageCount(path string) (int, error) {
	pc, err := readContext(context.Background(), path)
	if err != nil {
		return 0, err
	}
	return pc.PageCount, nil
}

// extractPages copies pages (0-based, in order, repeats allowed) into a new
// document, without its outlines and forms.
func extractPages(ctx context.Context, pc *model.Context, pages []int) (*model.Context, error) {
	nrs := make([]int, len(pages))
	for i, p := range pages {
		nrs[i] = p + 1
	}
	out, err := pdfcpu.ExtractPages(ctx, pc, nrs, false)
	if err != nil {
		return nil, fmt.Errorf("could not copy the pages: %w", err)
	}
	if err := out.EnsurePageCount(); err != nil {
		return nil, err
	}
	return out, nil
}

func normaliseRotation(r int) int { return ((r % 360) + 360) % 360 }

// rotatePage adds degrees (a multiple of 90) to page n's (1-based) turn.
func rotatePage(ctx context.Context, pc *model.Context, n, degrees int) error {
	if normaliseRotation(degrees) == 0 {
		return nil
	}
	d, _, inh, err := pc.PageDict(ctx, n, false)
	if err != nil {
		return err
	}
	current := 0
	if inh != nil {
		current = inh.Rotate
	}
	d["Rotate"] = types.Integer(normaliseRotation(current + degrees))
	return nil
}

// rotateAll turns every page.
func rotateAll(ctx context.Context, pc *model.Context, degrees int) error {
	for n := 1; n <= pc.PageCount; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := rotatePage(ctx, pc, n, degrees); err != nil {
			return err
		}
	}
	return nil
}

// stripMetadata drops the document information and every XMP packet
// (document, pages, pictures, forms) and the private data of the apps that
// made the file.
func stripMetadata(ctx context.Context, pc *model.Context) error {
	pc.Info = nil
	x := pc.XRefTable
	x.DirectInfoDict, x.CatalogXMPMeta = nil, nil
	x.Title, x.Author, x.Subject, x.Creator, x.Producer, x.CreationDate, x.ModDate, x.Keywords = "", "", "", "", "", "", "", ""
	if pc.Properties != nil {
		clear(pc.Properties)
	}
	root, err := pc.Catalog()
	if err != nil {
		return err
	}
	root.Delete("Metadata")
	root.Delete("PieceInfo")
	for n := 1; n <= pc.PageCount; n++ {
		if d, _, _, err := pc.PageDict(ctx, n, false); err == nil {
			d.Delete("PieceInfo")
		}
	}
	for _, e := range pc.Table {
		if e == nil || e.Free {
			continue
		}
		switch o := e.Object.(type) {
		case types.Dict:
			o.Delete("Metadata")
		case types.StreamDict:
			o.Delete("Metadata")
		}
	}
	return nil
}

// copyWritable copies a file; the copy is writable whatever the source was.
func copyWritable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// MergePdfs writes the pages of every input, in order, into output.
func MergePdfs(ctx context.Context, inputs []string, output string) error {
	if len(inputs) == 0 {
		return errors.New("no PDFs to merge")
	}
	for _, in := range inputs {
		// The same message as the other conversions for a locked file.
		if _, err := readContext(ctx, in); err != nil {
			return err
		}
	}
	tmp := output + ".merging.pdf"
	if err := api.MergeCreateFile(ctx, inputs, tmp, false, pdfConf()); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("could not merge the PDFs: %w", err)
	}
	return os.Rename(tmp, output)
}
