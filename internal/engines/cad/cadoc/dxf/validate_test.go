package dxf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/engines/cad/cadoc"
)

// checkStructure validates a written DXF: sections in order, every handle
// unique, $HANDSEED above every handle, owner (330) and pointer handles
// resolvable, every TABLE/BLOCK/OBJECT well formed.
func checkStructure(t *testing.T, data []byte) {
	t.Helper()
	if err := structureErrors(data); err != nil {
		t.Fatal(err)
	}
}

func structureErrors(data []byte) error {
	s := newStream(data, newCodec(true, nil))
	type obj struct {
		name     string
		handle   uint64
		refs     []uint64
		owner    uint64
		hasOwner bool
	}
	var objs []*obj
	var cur *obj
	handles := map[uint64]string{}
	var seed uint64
	var sections []string
	inHeader := false
	var lastVar string
	inXData := false
	inData := false
	for s.next() {
		if s.code == 0 {
			inXData = false
			if s.str == tokSection {
				s.next()
				sections = append(sections, s.str)
				inHeader = s.str == tokHeader
				inData = s.str == tokAcdsData
				cur = nil
				continue
			}
			if s.str == tokEndSection || s.str == tokEOF || s.str == tokEndTable {
				cur = nil
				inHeader = false
				continue
			}
			if s.str == tokClass || inData {
				cur = nil
				continue
			}
			cur = &obj{name: s.str}
			objs = append(objs, cur)
			continue
		}
		if inHeader {
			if s.code == 9 {
				lastVar = s.str
			} else if lastVar == "$HANDSEED" && s.code == 5 {
				seed = s.h
			}
			continue
		}
		if cur == nil {
			continue
		}
		if s.code == 1001 {
			inXData = true
		}
		if inXData {
			continue
		}
		switch {
		case s.code == 5 || s.code == 105:
			if cur.handle == 0 {
				cur.handle = s.h
				if prev, dup := handles[s.h]; dup {
					return fmt.Errorf("handle %X used by %s and %s", s.h, prev, cur.name)
				}
				handles[s.h] = cur.name
			}
		case s.code == 330 && !cur.hasOwner:
			cur.owner, cur.hasOwner = s.h, true
		case kindOf(s.code) == kindHandle && s.code >= 320:
			if s.h != 0 {
				cur.refs = append(cur.refs, s.h)
			}
		}
	}
	if s.err != nil {
		return s.err
	}
	want := []string{tokHeader, tokClasses, tokTables, tokBlocks, tokEntities, tokObjects}
	if len(sections) == len(want)+1 && sections[len(want)] == tokAcdsData {
		sections = sections[:len(want)]
	}
	if strings.Join(sections, ",") != strings.Join(want, ",") {
		return fmt.Errorf("sections %v, want %v", sections, want)
	}
	var max uint64
	for h := range handles {
		max = maxU(max, h)
	}
	if seed <= max {
		return fmt.Errorf("$HANDSEED %X not above the largest handle %X", seed, max)
	}
	for _, o := range objs {
		if o.handle == 0 {
			return fmt.Errorf("%s without a handle", o.name)
		}
		if o.hasOwner && o.owner != 0 {
			if _, ok := handles[o.owner]; !ok {
				return fmt.Errorf("%s %X: owner %X does not exist", o.name, o.handle, o.owner)
			}
		}
		for _, r := range o.refs {
			if _, ok := handles[r]; !ok {
				return fmt.Errorf("%s %X: reference %X does not exist", o.name, o.handle, r)
			}
		}
	}
	return nil
}

func maxU(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// roundTrip writes doc and reads it back.
func roundTrip(t *testing.T, doc *cadoc.Document, cfg WriteConfig) (*cadoc.Document, []byte) {
	t.Helper()
	var buf bytes.Buffer
	var notes []string
	if err := Write(&buf, doc, cfg, func(l Level, m string) {
		if l >= LevelWarning {
			notes = append(notes, m)
		}
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, n := range notes {
		t.Logf("write: %s", n)
	}
	checkStructure(t, buf.Bytes())
	back, err := ReadBytes(buf.Bytes(), ReadConfig{Failsafe: false}, func(l Level, m string) {
		if l >= LevelWarning {
			t.Logf("read: %s", m)
		}
	})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	return back, buf.Bytes()
}
