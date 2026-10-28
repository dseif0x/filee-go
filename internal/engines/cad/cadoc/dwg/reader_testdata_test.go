package dwg

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// The testdata pairs are DWG files and the DXF of the same drawing; the
// sample DXF files are stripped of their binary chunks (310/311) to keep
// testdata small.
var readerTestdata = []struct {
	name, dwg, dxf string
	r14            bool
}{
	// R14 (R13 format family) against the 2000 DXF of the drawing.
	{"AC1014", "reader_sample_AC1014.dwg.gz", "reader_sample_AC1015_nobin.dxf.gz", true},
	// R2007: Reed-Solomon and LZ77 AC21 sections, string streams.
	{"AC1021", "reader_sample_AC1021.dwg", "reader_sample_AC1021_nobin.dxf.gz", false},
	// R2018 files: R2004 pages and LZ77 AC18, R2010 object streams.
	{"geoloc", "reader_geoloc.dwg", "reader_geoloc.dxf.gz", false},
	{"dynblock", "reader_dynblock.dwg", "reader_dynblock.dxf.gz", false},
}

func TestReaderTestdata(t *testing.T) {
	for _, c := range readerTestdata {
		t.Run(c.name, func(t *testing.T) {
			doc := readDWG(t, c.dwg, readTestFile(t, filepath.Join("testdata", c.dwg)))
			ref := readDXF(t, c.dxf, readTestFile(t, filepath.Join("testdata", c.dxf)))
			d := &docDiff{t: t, name: c.name, r14: c.r14}
			compareDocs(d, doc, ref)
			compareHeader(d, doc.Header, ref.Header)
			d.report(40)
			checkInvariants(t, doc)
		})
	}
}

// TestReaderSpotChecks checks values of the AC1021 sample against its DXF.
func TestReaderSpotChecks(t *testing.T) {
	doc := readDWG(t, "AC1021", readTestFile(t, filepath.Join("testdata", "reader_sample_AC1021.dwg")))
	h := doc.Header
	if h.Version != cadoc.AC1021 || h.InsUnits != cadoc.UnitsType(1) || h.LineTypeScale != 1 {
		t.Errorf("header: %v $INSUNITS %v $LTSCALE %v", h.Version, h.InsUnits, h.LineTypeScale)
	}
	o, ok := doc.GetObject(0x626)
	if !ok {
		t.Fatal("LINE 626 not found")
	}
	l, ok := o.(*cadoc.Line)
	if !ok {
		t.Fatalf("626 is %T", o)
	}
	want := [2]cadoc.Vec3{{X: 329.9765594765796, Y: 3.482445332433286}, {X: 319.2761894008183, Y: 22.01602996344096}}
	if !nearVec(l.StartPoint, want[0]) || !nearVec(l.EndPoint, want[1]) {
		t.Errorf("LINE 626: %v %v", l.StartPoint, l.EndPoint)
	}
	if l.LayerName() != "Layer1" || !l.Color.IsByBlock() || !l.Transparency.IsByBlock() || l.LineWeight != cadoc.LineWeightByBlock {
		t.Errorf("LINE 626: layer %s color %v transparency %v weight %v", l.LayerName(), l.Color, l.Transparency.Value(), l.LineWeight)
	}
	if r, ok := l.Owner.(*cadoc.BlockRecord); !ok || r.Handle != 0x529 {
		t.Errorf("LINE 626 owner %v", l.Owner)
	}
	if doc.SummaryInfo == nil || doc.Header.CodePage != "ANSI_1252" {
		t.Errorf("summary %v code page %q", doc.SummaryInfo, doc.Header.CodePage)
	}
}

// checkInvariants checks what code using a document relies on: model and
// paper space with layouts, owners, a root dictionary and unique handles
// below the handle seed.
func checkInvariants(t *testing.T, doc *cadoc.Document) {
	t.Helper()
	model, paper := doc.ModelSpace(), doc.PaperSpace()
	if model == nil || paper == nil {
		t.Fatalf("model space %v paper space %v", model, paper)
	}
	if model.Layout == nil || model.Layout.AssociatedBlock != model {
		t.Errorf("model space layout %v", model.Layout)
	}
	if paper.Layout == nil || paper.Layout.AssociatedBlock != paper {
		t.Errorf("paper space layout %v", paper.Layout)
	}
	if doc.RootDictionary == nil || doc.RootDictionary.Owner != nil {
		t.Fatal("root dictionary")
	}
	seen := map[uint64]cadoc.Object{}
	doc.Walk(func(o cadoc.Object) {
		b := o.Base()
		if b.Handle == 0 {
			t.Errorf("%s without handle", o.ObjectName())
		} else if prev, dup := seen[b.Handle]; dup {
			t.Errorf("handle %X used by %s and %s", b.Handle, prev.ObjectName(), o.ObjectName())
		}
		seen[b.Handle] = o
		if b.Handle >= doc.Header.HandleSeed {
			t.Errorf("handle %X above $HANDSEED %X", b.Handle, doc.Header.HandleSeed)
		}
		if e, ok := o.(cadoc.Entity); ok && e.Entity().Layer == nil {
			t.Errorf("%s %X without layer", o.ObjectName(), b.Handle)
		}
	})
	for _, r := range doc.BlockRecords.Entries() {
		if r.Owner != cadoc.Object(doc.BlockRecords) {
			t.Errorf("block %s owner %v", r.Name, r.Owner)
		}
		for _, e := range r.Entities {
			if e.Base().Owner != cadoc.Object(r) {
				t.Errorf("%s %X owner %v, in block %s", e.ObjectName(), e.Base().Handle, e.Base().Owner, r.Name)
			}
		}
	}
	for _, l := range doc.Layouts() {
		if l.AssociatedBlock == nil {
			t.Errorf("layout %s without block", l.Name)
		}
	}
}

func TestReaderUnsupportedVersions(t *testing.T) {
	for _, sig := range []string{"AC1009", "AC1006", "AC2.10", "MC0.0"} {
		_, err := ReadBytes(append([]byte(sig), make([]byte, 64)...), ReadConfig{Failsafe: true}, nil)
		if !errors.Is(err, ErrNotSupported) {
			t.Errorf("%s: %v", sig, err)
		}
	}
	if _, err := ReadBytes([]byte("not a dwg file at all"), ReadConfig{Failsafe: true}, nil); !errors.Is(err, ErrNotSupported) {
		t.Errorf("garbage: %v", err)
	}
	if v := FileVersion([]byte("AC1032\x00\x00")); v != cadoc.AC1032 {
		t.Errorf("FileVersion %v", v)
	}
}

// TestReaderDamaged checks that damaged files fail cleanly or are read in
// failsafe mode without panics.
func TestReaderDamaged(t *testing.T) {
	data := readTestFile(t, filepath.Join("testdata", "reader_geoloc.dwg"))
	for _, n := range []int{0x30, 0x100, len(data) / 3, len(data) / 2, len(data) - 100} {
		cut := data[:n]
		if _, err := ReadBytes(cut, ReadConfig{Failsafe: true}, nil); err == nil && n < 0x200 {
			t.Errorf("truncated at %d: no error", n)
		}
	}
	// Flip bytes inside the objects: failsafe reading skips broken objects.
	broken := bytes.Clone(data)
	for i := len(broken) / 2; i < len(broken)/2+64; i++ {
		broken[i] ^= 0x5A
	}
	var notes []string
	_, err := ReadBytes(broken, ReadConfig{Failsafe: true}, func(level int, msg string) {
		notes = append(notes, fmt.Sprintf("%d %s", level, msg))
	})
	if err != nil && !strings.HasPrefix(err.Error(), "dwg:") {
		t.Errorf("damaged: %v", err)
	}
}
