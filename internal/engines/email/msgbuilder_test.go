package email

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"time"
	"unicode/utf16"
)

// msgBuilder writes small Outlook .msg files (MS-OXMSG): the property
// streams, strings and binaries in "__substg1.0_" streams, one storage per
// recipient and attachment, and an empty named property map.
type msgBuilder struct {
	subject, senderName, senderEmail string
	sent                             time.Time
	body, html, messageID            *string
	rtf                              []byte // PR_RTF_COMPRESSED
	recipients                       []msgRecipient
	attachments                      []msgAttachment
	attachedMessages                 []*msgBuilder
}

type msgRecipient struct {
	name, email string
	typ         int
}

type msgAttachment struct {
	fileName  string
	data      []byte
	mimeType  string
	contentID string
}

func ptr(s string) *string { return &s }

type msgProperties struct {
	node    *cfbNode
	entries bytes.Buffer
}

func (p *msgProperties) entry(id, typ uint16, value []byte) {
	binary.Write(&p.entries, binary.LittleEndian, uint32(id)<<16|uint32(typ))
	binary.Write(&p.entries, binary.LittleEndian, uint32(6))
	v := make([]byte, 8)
	copy(v, value)
	p.entries.Write(v)
}

func (p *msgProperties) str(id uint16, s string) {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	p.node.stream(fmt.Sprintf("__substg1.0_%04X%04X", id, ptUnicode), b)
	p.entry(id, ptUnicode, le32(len(b)+2))
}

func (p *msgProperties) bin(id uint16, b []byte) {
	p.node.stream(fmt.Sprintf("__substg1.0_%04X%04X", id, ptBinary), b)
	p.entry(id, ptBinary, le32(len(b)))
}

func (p *msgProperties) long(id uint16, v int) { p.entry(id, ptLong, le32(v)) }

func (p *msgProperties) save(header []byte) {
	p.node.stream("__properties_version1.0", append(header, p.entries.Bytes()...))
}

func le32(v int) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return b
}

func (m *msgBuilder) save(path string) error {
	root := &cfbNode{name: "Root Entry", storage: true}
	m.write(root, true)
	names := root.storageNode("__nameid_version1.0")
	for _, s := range []string{"__substg1.0_00020102", "__substg1.0_00030102", "__substg1.0_00040102"} {
		names.stream(s, nil)
	}
	return os.WriteFile(path, buildCFB(root), 0o644)
}

func (m *msgBuilder) write(node *cfbNode, top bool) {
	p := &msgProperties{node: node}
	p.str(0x001A, "IPM.Note")
	p.str(0x0037, m.subject)
	p.str(0x0C1A, m.senderName)
	p.str(0x0C1F, m.senderEmail)
	p.str(0x5D01, m.senderEmail)
	sent := m.sent
	if sent.IsZero() {
		sent = time.Date(2026, 10, 1, 0, 30, 0, 0, time.UTC)
	}
	ft := make([]byte, 8)
	binary.LittleEndian.PutUint64(ft, uint64(sent.UnixNano()/100+116444736000000000))
	p.entry(0x0039, ptTime, ft)
	p.long(0x3FDE, 65001)
	if m.body != nil {
		p.str(0x1000, *m.body)
	}
	if m.html != nil {
		p.bin(0x1013, []byte(*m.html))
	}
	if m.rtf != nil {
		p.bin(0x1009, m.rtf)
	}
	if m.messageID != nil {
		p.str(0x1035, *m.messageID)
	}
	for i, r := range m.recipients {
		rp := &msgProperties{node: node.storageNode(fmt.Sprintf("__recip_version1.0_#%08X", i))}
		rp.long(0x0C15, r.typ)
		rp.str(0x3001, r.name)
		rp.str(0x3003, r.email)
		rp.str(0x39FE, r.email)
		rp.save(make([]byte, 8))
	}
	count := 0
	for _, a := range m.attachments {
		ap := &msgProperties{node: node.storageNode(fmt.Sprintf("__attach_version1.0_#%08X", count))}
		ap.long(0x3705, 1)
		ap.bin(0x3701, a.data)
		ap.str(0x3707, a.fileName)
		ap.str(0x370E, a.mimeType)
		if a.contentID != "" {
			ap.str(0x3712, a.contentID)
		}
		ap.save(make([]byte, 8))
		count++
	}
	for _, inner := range m.attachedMessages {
		s := node.storageNode(fmt.Sprintf("__attach_version1.0_#%08X", count))
		ap := &msgProperties{node: s}
		ap.long(0x3705, 5)
		ap.str(0x3001, inner.subject)
		ap.entry(0x3701, ptObject, nil)
		ap.save(make([]byte, 8))
		inner.write(s.storageNode("__substg1.0_3701000D"), false)
		count++
	}
	header := make([]byte, 8)
	header = append(header, le32(len(m.recipients))...)
	header = append(header, le32(count)...)
	header = append(header, le32(len(m.recipients))...)
	header = append(header, le32(count)...)
	if top {
		header = append(header, make([]byte, 8)...)
	}
	p.save(header)
}
