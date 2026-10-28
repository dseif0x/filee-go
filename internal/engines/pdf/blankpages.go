// Removes pages that contain no painting operations at all. Deliberately
// stricter than "no text": charts, borders, pictures, annotations and even a
// painted white rectangle keep a page. LibreOffice Calc can append empty
// tiles when a spreadsheet's print range is larger than its content.

package pdf

import (
	"context"
	"os"
	"path/filepath"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// paintOps can put a visible mark on a page.
var paintOps = map[string]bool{
	"S": true, "s": true, "f": true, "F": true, "f*": true, "B": true, "B*": true, "b": true, "b*": true,
	"Do": true, "BI": true, "sh": true,
	// Text counts even when empty or invisible: uncertain pages must stay.
	"Tj": true, "TJ": true, "'": true, "\"": true,
}

// RemoveBlankPages removes pages without painting operators from path,
// replacing it only after the cleaned PDF was saved. A wholly blank document
// is left alone. It returns the number of pages removed.
func RemoveBlankPages(ctx context.Context, path string) (int, error) {
	pc, err := readContext(ctx, path)
	if err != nil {
		return 0, err
	}
	var keep []int
	for n := 1; n <= pc.PageCount; n++ {
		if !isBlankPage(ctx, pc, n) {
			keep = append(keep, n-1)
		}
	}
	removed := pc.PageCount - len(keep)
	if removed == 0 || len(keep) == 0 {
		return 0, nil
	}
	out, err := extractPages(ctx, pc, keep)
	if err != nil {
		return 0, err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".blank.pdf")
	defer os.Remove(tmp)
	if err := writeContext(ctx, out, tmp); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	return removed, nil
}

// isBlankPage: unreadable content is not blank, uncertainty must never delete a page.
func isBlankPage(ctx context.Context, pc *pdfContext, n int) bool {
	d, _, _, err := pc.PageDict(ctx, n, false)
	if err != nil || d == nil {
		return false
	}
	if o, _ := pc.Dereference(d["Annots"]); o != nil {
		if a, ok := o.(types.Array); ok && len(a) > 0 {
			return false
		}
	}
	content, err := pc.PageContent(d, n)
	if err != nil {
		// A page without content streams is blank; one that can't be read is not.
		_, has := d["Contents"]
		return !has
	}
	paints := false
	scanContent(content, func(op contentOp) bool {
		if paintOps[op.Op] {
			paints = true
			return false
		}
		return true
	})
	return !paints
}
