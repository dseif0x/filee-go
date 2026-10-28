package dxf

import (
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// build resolves references and places entities once all sections are read
// (DxfDocumentBuilder.BuildDocument).
func (r *reader) build() error {
	doc := r.doc
	model, ok := doc.BlockRecords.Get(cadoc.ModelSpaceName)
	if !ok {
		model = cadoc.NewBlockRecord(cadoc.ModelSpaceName)
		_ = doc.BlockRecords.Add(model)
	}
	paper, _ := doc.BlockRecords.Get(cadoc.PaperSpaceName)

	for _, f := range r.fixDicts {
		f()
	}
	for _, p := range r.placed {
		target := model
		if p.hasOwner && p.owner != 0 {
			if rec, ok := r.object(p.owner).(*cadoc.BlockRecord); ok {
				target = rec
			} else if p.paper && paper != nil {
				target = paper
			}
		} else if p.paper {
			if paper == nil {
				paper = cadoc.NewBlockRecord(cadoc.PaperSpaceName)
				_ = doc.BlockRecords.Add(paper)
			}
			target = paper
		}
		target.AddEntity(p.e)
	}
	// R12 files have no ByLayer/ByBlock entries in their LTYPE table, yet
	// entities name them.
	for _, n := range []string{cadoc.ByLayerName, cadoc.ByBlockName, cadoc.ContinuousName} {
		if !doc.LineTypes.Contains(n) {
			lt := cadoc.NewLineType(n)
			if n == cadoc.ContinuousName {
				lt.Description = "Solid line"
			}
			_ = doc.LineTypes.Add(lt)
		}
	}
	for _, f := range r.fixups {
		f()
	}

	// Layouts and their blocks point at each other.
	for _, l := range doc.Layouts() {
		if l.AssociatedBlock != nil {
			l.AssociatedBlock.Layout = l
		}
	}
	for _, b := range doc.BlockRecords.Entries() {
		if b.Layout != nil && b.Layout.AssociatedBlock == nil {
			b.Layout.AssociatedBlock = b
		}
	}

	for _, ts := range doc.TextStyles.Entries() {
		applyTrueTypeXData(ts)
	}

	if r.cfg.CreateDefaults {
		doc.CreateDefaults()
	}
	doc.FixReferences()
	if doc.Header.HandleSeed <= r.maxHandle {
		doc.Header.HandleSeed = r.maxHandle + 1
	}
	return nil
}

// applyTrueTypeXData reads the TrueType family (1000) and style flags (1071)
// a text style keeps in its ACAD xdata.
func applyTrueTypeXData(ts *cadoc.TextStyle) {
	x, ok := ts.GetExtendedData(cadoc.DefaultAppIdName)
	if !ok {
		return
	}
	for _, rec := range x.Records {
		switch rec.Code {
		case 1000:
			if ts.FontFamily == "" {
				ts.FontFamily = rec.String()
			}
		case 1071:
			flags := uint32(rec.Int32())
			var f cadoc.FontFlags
			if flags&0x01000000 != 0 {
				f |= cadoc.FontFlagsItalic
			}
			if flags&0x02000000 != 0 {
				f |= cadoc.FontFlagsBold
			}
			ts.TrueType = f
		}
	}
}

// trueTypeXData builds the ACAD xdata of a TrueType text style.
func trueTypeXData(ts *cadoc.TextStyle) []cadoc.ExtendedDataRecord {
	var flags uint32 = 0x22 // ANSI charset, variable pitch / swiss family (AutoCAD's usual value)
	if ts.TrueType&cadoc.FontFlagsItalic != 0 {
		flags |= 0x01000000
	}
	if ts.TrueType&cadoc.FontFlagsBold != 0 {
		flags |= 0x02000000
	}
	family := ts.FontFamily
	if family == "" {
		family = strings.TrimSuffix(strings.TrimSuffix(ts.Filename, ".ttf"), ".TTF")
	}
	return []cadoc.ExtendedDataRecord{{Code: 1000, Value: family}, {Code: 1071, Value: int32(flags)}}
}
