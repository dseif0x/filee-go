package email

import (
	"bytes"
	"encoding/binary"
	"sort"
	"strings"
	"unicode/utf16"
)

// A minimal compound file writer (MS-CFB version 3) for the MSG tests:
// 512-byte sectors, streams under 4096 bytes in the mini stream.

type cfbNode struct {
	name     string
	data     []byte
	children []*cfbNode
	storage  bool
}

func (n *cfbNode) storageNode(name string) *cfbNode {
	c := &cfbNode{name: name, storage: true}
	n.children = append(n.children, c)
	return c
}

func (n *cfbNode) stream(name string, data []byte) {
	n.children = append(n.children, &cfbNode{name: name, data: data})
}

const (
	cfbFree   = 0xFFFFFFFF
	cfbEnd    = 0xFFFFFFFE
	cfbFatSec = 0xFFFFFFFD
	cfbNone   = 0xFFFFFFFF
)

func cfbLess(a, b string) bool {
	if len(utf16.Encode([]rune(a))) != len(utf16.Encode([]rune(b))) {
		return len(utf16.Encode([]rune(a))) < len(utf16.Encode([]rune(b)))
	}
	return strings.ToUpper(a) < strings.ToUpper(b)
}

func buildCFB(root *cfbNode) []byte {
	// Flatten the tree: directory entries in order, children linked as a right-leaning chain.
	type dirEntry struct {
		node               *cfbNode
		left, right, child uint32
		start              uint32
		size               uint64
	}
	var entries []*dirEntry
	var add func(n *cfbNode) uint32
	add = func(n *cfbNode) uint32 {
		e := &dirEntry{node: n, left: cfbNone, right: cfbNone, child: cfbNone}
		entries = append(entries, e)
		id := uint32(len(entries) - 1)
		kids := append([]*cfbNode(nil), n.children...)
		sort.Slice(kids, func(i, j int) bool { return cfbLess(kids[i].name, kids[j].name) })
		var prev *dirEntry
		for _, k := range kids {
			kid := add(k)
			if prev == nil {
				e.child = kid
			} else {
				prev.right = kid
			}
			prev = entries[kid]
		}
		return id
	}
	add(root)

	var sectors [][]byte // regular sectors in order
	var fat []uint32
	alloc := func(data []byte) uint32 {
		if len(data) == 0 {
			return cfbEnd
		}
		start := uint32(len(sectors))
		for off := 0; off < len(data); off += 512 {
			s := make([]byte, 512)
			copy(s, data[off:min(len(data), off+512)])
			sectors = append(sectors, s)
			fat = append(fat, uint32(len(sectors)))
		}
		fat[len(fat)-1] = cfbEnd
		return start
	}
	// Mini stream and mini FAT.
	var mini []byte
	var miniFat []uint32
	for _, e := range entries {
		if e.node.storage || e.node == root {
			continue
		}
		e.size = uint64(len(e.node.data))
		if len(e.node.data) >= 4096 {
			continue
		}
		if len(e.node.data) == 0 {
			e.start = cfbEnd
			continue
		}
		e.start = uint32(len(mini) / 64)
		for off := 0; off < len(e.node.data); off += 64 {
			chunk := make([]byte, 64)
			copy(chunk, e.node.data[off:min(len(e.node.data), off+64)])
			mini = append(mini, chunk...)
			miniFat = append(miniFat, uint32(len(mini)/64))
		}
		miniFat[len(miniFat)-1] = cfbEnd
	}
	for _, e := range entries {
		if !e.node.storage && e.node != root && len(e.node.data) >= 4096 {
			e.start = alloc(e.node.data)
		}
	}
	rootEntry := entries[0]
	rootEntry.start = alloc(mini)
	rootEntry.size = uint64(len(mini))
	var mf bytes.Buffer
	for _, v := range miniFat {
		binary.Write(&mf, binary.LittleEndian, v)
	}
	miniFatStart := alloc(mf.Bytes())
	miniFatSectors := (mf.Len() + 511) / 512

	var dir bytes.Buffer
	for _, e := range entries {
		b := make([]byte, 128)
		name := utf16.Encode([]rune(e.node.name))
		for i, c := range name {
			binary.LittleEndian.PutUint16(b[i*2:], c)
		}
		binary.LittleEndian.PutUint16(b[64:], uint16((len(name)+1)*2))
		switch {
		case e.node == root:
			b[66] = 5
		case e.node.storage:
			b[66] = 1
		default:
			b[66] = 2
		}
		b[67] = 1
		binary.LittleEndian.PutUint32(b[68:], e.left)
		binary.LittleEndian.PutUint32(b[72:], e.right)
		binary.LittleEndian.PutUint32(b[76:], e.child)
		start := e.start
		if e.node.storage && e.node != root {
			start = 0
		}
		binary.LittleEndian.PutUint32(b[116:], start)
		binary.LittleEndian.PutUint64(b[120:], e.size)
		dir.Write(b)
	}
	for dir.Len()%512 != 0 {
		b := make([]byte, 128)
		binary.LittleEndian.PutUint32(b[68:], cfbNone)
		binary.LittleEndian.PutUint32(b[72:], cfbNone)
		binary.LittleEndian.PutUint32(b[76:], cfbNone)
		dir.Write(b)
	}
	dirStart := alloc(dir.Bytes())

	// FAT sectors at the end: each holds 128 entries, including the FAT sectors themselves.
	fatSectors := 1
	for (len(fat)+fatSectors+127)/128 > fatSectors {
		fatSectors++
	}
	fatStart := uint32(len(fat))
	for i := 0; i < fatSectors; i++ {
		fat = append(fat, cfbFatSec)
	}
	for len(fat)%128 != 0 {
		fat = append(fat, cfbFree)
	}
	var fb bytes.Buffer
	for _, v := range fat {
		binary.Write(&fb, binary.LittleEndian, v)
	}
	for off := 0; off < fb.Len(); off += 512 {
		sectors = append(sectors, fb.Bytes()[off:off+512])
	}

	h := make([]byte, 512)
	copy(h, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	binary.LittleEndian.PutUint16(h[24:], 0x3E)
	binary.LittleEndian.PutUint16(h[26:], 3)
	binary.LittleEndian.PutUint16(h[28:], 0xFFFE)
	binary.LittleEndian.PutUint16(h[30:], 9)
	binary.LittleEndian.PutUint16(h[32:], 6)
	binary.LittleEndian.PutUint32(h[44:], uint32(fatSectors))
	binary.LittleEndian.PutUint32(h[48:], dirStart)
	binary.LittleEndian.PutUint32(h[56:], 4096)
	if miniFatStart == cfbEnd {
		binary.LittleEndian.PutUint32(h[60:], cfbEnd)
	} else {
		binary.LittleEndian.PutUint32(h[60:], miniFatStart)
	}
	binary.LittleEndian.PutUint32(h[64:], uint32(miniFatSectors))
	binary.LittleEndian.PutUint32(h[68:], cfbEnd)
	for i := 0; i < 109; i++ {
		v := uint32(cfbFree)
		if i < fatSectors {
			v = fatStart + uint32(i)
		}
		binary.LittleEndian.PutUint32(h[76+i*4:], v)
	}
	out := h
	for _, s := range sectors {
		out = append(out, s...)
	}
	return out
}
