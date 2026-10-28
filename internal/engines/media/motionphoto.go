package media

// Android Motion Photos: where the video saved with a camera picture is, read
// from the picture's own metadata, and new ones written the way a Pixel does.
// Google Motion Photo 1.0 JPEGs (XMP container directory, the video
// appended), legacy MicroVideo JPEGs (an offset from the end of the file) and
// HEIC / AVIF pictures with the video in an mpvd box.

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

var xmpHeader = []byte("http://ns.adobe.com/xap/1.0/\x00")

const (
	nsCamera    = "http://ns.google.com/photos/1.0/camera/"
	nsContainer = "http://ns.google.com/photos/1.0/container/"
	nsItem      = "http://ns.google.com/photos/1.0/container/item/"
)

// JPEG metadata comes before the picture data; the walk never goes further than this.
const maxMetadataBytes = 8 << 20

// WriteMotionPhoto writes a Motion Photo 1.0 JPEG: jpeg with the Motion Photo
// XMP after its JFIF and Exif segments, then the MP4 video appended.
func WriteMotionPhoto(jpeg []byte, video, output string, presentationUs int64) error {
	st, err := os.Stat(video)
	if err != nil {
		return err
	}
	payload := MotionPhotoXmp(st.Size(), presentationUs)
	at, err := XmpPosition(jpeg)
	if err != nil {
		return err
	}
	if len(payload)+2 > 0xffff {
		return invalid("XMP segment too large")
	}
	out, err := os.Create(output)
	if err != nil {
		return err
	}
	defer out.Close()
	w := io.Writer(out)
	if _, err := w.Write(jpeg[:at]); err != nil {
		return err
	}
	hdr := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(hdr[2:], uint16(len(payload)+2))
	w.Write(hdr)
	w.Write(payload)
	if _, err := w.Write(jpeg[at:]); err != nil {
		return err
	}
	in, err := os.Open(video)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := copyRange(w, in, st.Size()); err != nil {
		return err
	}
	return out.Close()
}

// MotionPhotoXmp is the XMP segment's payload laid out as a Pixel writes it,
// plus the older MicroVideo properties for older Google Photos.
func MotionPhotoXmp(videoLength, presentationUs int64) []byte {
	l := strconv.FormatInt(videoLength, 10)
	t := strconv.FormatInt(presentationUs, 10)
	x := `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""
        xmlns:GCamera="` + nsCamera + `"
        xmlns:Container="` + nsContainer + `"
        xmlns:Item="` + nsItem + `"
      GCamera:MotionPhoto="1"
      GCamera:MotionPhotoVersion="1"
      GCamera:MotionPhotoPresentationTimestampUs="` + t + `"
      GCamera:MicroVideo="1"
      GCamera:MicroVideoVersion="1"
      GCamera:MicroVideoOffset="` + l + `"
      GCamera:MicroVideoPresentationTimestampUs="` + t + `">
      <Container:Directory>
        <rdf:Seq>
          <rdf:li rdf:parseType="Resource">
            <Container:Item
              Item:Semantic="Primary"
              Item:Mime="image/jpeg"/>
          </rdf:li>
          <rdf:li rdf:parseType="Resource">
            <Container:Item
              Item:Mime="video/mp4"
              Item:Semantic="MotionPhoto"
              Item:Length="` + l + `"
              Item:Padding="0"/>
          </rdf:li>
        </rdf:Seq>
      </Container:Directory>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`
	return append(bytes.Clone(xmpHeader), x...)
}

// XmpPosition is where the XMP segment goes: after the start marker and the
// JFIF (APP0) and Exif (APP1) segments.
func XmpPosition(jpeg []byte) (int, error) {
	if len(jpeg) < 4 || jpeg[0] != 0xff || jpeg[1] != 0xd8 {
		return 0, invalid("the Motion Photo's picture is not a JPEG")
	}
	at := 2
	for at+4 <= len(jpeg) && jpeg[at] == 0xff {
		m := jpeg[at+1]
		exif := m == 0xe1 && bytes.HasPrefix(jpeg[at+4:], []byte("Exif\x00"))
		if m != 0xe0 && !exif {
			break
		}
		end := at + 2 + int(binary.BigEndian.Uint16(jpeg[at+2:]))
		if end > len(jpeg) {
			return 0, invalid("invalid JPEG segment length")
		}
		at = end
	}
	return at, nil
}

// FindMotionVideo finds the declared video (never by searching the bytes for
// an MP4 signature); ok is false for a picture without one. An error means
// the picture says it has a video that isn't there (stale metadata).
func FindMotionVideo(r io.ReadSeeker) (offset, length int64, ok bool, err error) {
	total, err := streamLength(r)
	if err != nil {
		return 0, 0, false, err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, 0, false, err
	}
	var soi [2]byte
	if n, _ := io.ReadFull(r, soi[:]); n == 2 && soi[0] == 0xff && soi[1] == 0xd8 {
		return findJpegVideo(r, total)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, 0, false, err
	}
	for pos := int64(0); pos < total; {
		b, err := ReadBoxHeader(r)
		if err != nil {
			return 0, 0, false, err
		}
		if b.Type == "mpvd" {
			// Samsung writes its own trailer after the box, so the box need not end the file.
			if b.Length <= int64(b.HeaderLength) {
				return 0, 0, false, invalid("invalid Motion Photo video box")
			}
			o, l, err := checkedMovie(r, b.Start+int64(b.HeaderLength), b.Length-int64(b.HeaderLength))
			return o, l, err == nil, err
		}
		pos = b.End()
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return 0, 0, false, err
		}
	}
	return 0, 0, false, nil
}

func findJpegVideo(r io.ReadSeeker, total int64) (int64, int64, bool, error) {
	readByte := func() int {
		var b [1]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return -1
		}
		return int(b[0])
	}
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= min(total, maxMetadataBytes) {
			return 0, 0, false, nil
		}
		if readByte() != 0xff {
			return 0, 0, false, invalid("invalid JPEG marker")
		}
		m := readByte()
		for m == 0xff {
			m = readByte()
		}
		if m == 0xda || m == 0xd9 {
			return 0, 0, false, nil
		}
		if m < 0 {
			return 0, 0, false, io.ErrUnexpectedEOF
		}
		var sz [2]byte
		if _, err := io.ReadFull(r, sz[:]); err != nil {
			return 0, 0, false, err
		}
		n := int64(binary.BigEndian.Uint16(sz[:])) - 2
		pos, _ = r.Seek(0, io.SeekCurrent)
		if n < 0 || n > total-pos {
			return 0, 0, false, invalid("invalid JPEG segment length")
		}
		if m != 0xe1 {
			if _, err := r.Seek(n, io.SeekCurrent); err != nil {
				return 0, 0, false, err
			}
			continue
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return 0, 0, false, err
		}
		if !bytes.HasPrefix(data, xmpHeader) {
			continue
		}
		vl, found, err := xmpVideoLength(data)
		if err != nil {
			return 0, 0, false, err
		}
		if !found {
			continue
		}
		if vl == 0 {
			return 0, 0, false, nil
		}
		// The video ends the file; Samsung's trailer after it is cut off by checkedMovie.
		start := total - vl
		cur, _ := r.Seek(0, io.SeekCurrent)
		if vl < 16 || start < cur {
			return 0, 0, false, invalid("Motion Photo video length is outside the file")
		}
		o, l, err := checkedMovie(r, start, vl)
		return o, l, err == nil, err
	}
}

// xmlNode is a tiny namespace-resolved DOM of an XMP packet.
type xmlNode struct {
	name     xml.Name
	attrs    []xml.Attr
	children []*xmlNode
	text     strings.Builder
}

func parseXML(data []byte) (*xmlNode, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	root := &xmlNode{}
	stack := []*xmlNode{root}
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: XMP: %v", ErrInvalidData, err)
		}
		switch v := t.(type) {
		case xml.StartElement:
			n := &xmlNode{name: v.Name, attrs: v.Attr}
			top := stack[len(stack)-1]
			top.children = append(top.children, n)
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			stack[len(stack)-1].text.Write(v)
		case xml.Directive:
			// No DTDs: entities could read local files or blow up.
			return nil, fmt.Errorf("%w: XMP has a DTD", ErrInvalidData)
		}
	}
	return root, nil
}

func (n *xmlNode) walk(f func(*xmlNode)) {
	for _, c := range n.children {
		f(c)
		c.walk(f)
	}
}

func (n *xmlNode) attr(space, local string) (string, bool) {
	for _, a := range n.attrs {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

// property is the first attribute of that name, else the first element's text.
func (root *xmlNode) property(space, local string) (string, bool) {
	var v string
	found := false
	root.walk(func(n *xmlNode) {
		if !found {
			v, found = n.attr(space, local)
		}
	})
	if found {
		return v, true
	}
	root.walk(func(n *xmlNode) {
		if !found && n.name.Space == space && n.name.Local == local {
			v, found = n.text.String(), true
		}
	})
	return v, found
}

func (n *xmlNode) field(local string) string {
	if v, ok := n.attr(nsItem, local); ok {
		return v
	}
	for _, c := range n.children {
		if c.name.Space == nsItem && c.name.Local == local {
			return c.text.String()
		}
	}
	return ""
}

// xmpVideoLength is the length from the end of the file the XMP gives the
// video (0 for a picture marked as not moving); found is false when this
// packet says nothing about motion.
func xmpVideoLength(data []byte) (int64, bool, error) {
	// Some writers pad the packet with NUL bytes after its end, which XML doesn't allow.
	end := max(len(xmpHeader), bytes.LastIndexByte(data, '>')+1)
	root, err := parseXML(data[len(xmpHeader):end])
	if err != nil {
		return 0, false, err
	}
	if motion, ok := root.property(nsCamera, "MotionPhoto"); ok {
		if strings.TrimSpace(motion) != "1" {
			return 0, true, nil
		}
		// The video is the last item of the directory (a Pixel lists a gain map before it).
		var last *xmlNode
		root.walk(func(n *xmlNode) {
			if n.name.Space == nsContainer && n.name.Local == "Item" {
				last = n
			}
		})
		if last == nil || last.field("Semantic") != "MotionPhoto" {
			return 0, false, invalid("Motion Photo has no valid video directory entry")
		}
		if m := last.field("Mime"); m != "video/mp4" && m != "video/quicktime" {
			return 0, false, invalid("Motion Photo has no valid video directory entry")
		}
		l, err := parseDigits(last.field("Length"))
		if err != nil {
			return 0, false, invalid("Motion Photo has no valid video directory entry")
		}
		return l, true, nil
	}
	// MicroVideo (before Motion Photo 1.0): an offset back from the end of the file.
	if mv, ok := root.property(nsCamera, "MicroVideo"); ok && strings.TrimSpace(mv) == "1" {
		o, _ := root.property(nsCamera, "MicroVideoOffset")
		l, err := parseDigits(o)
		if err != nil {
			return 0, false, invalid("MicroVideo has no valid offset")
		}
		return l, true, nil
	}
	return 0, false, nil
}

// parseDigits reads a plain decimal number (no sign, no spaces), as NumberStyles.None.
func parseDigits(s string) (int64, error) {
	if s == "" || strings.TrimLeft(s, "0123456789") != "" {
		return 0, invalid("not a number")
	}
	return strconv.ParseInt(s, 10, 64)
}

// checkedMovie is the video at start, up to the end of its last whole box: an
// MP4 must start there (ftyp) and hold a movie header. A trailer after it
// (Samsung's SEF) is left out.
func checkedMovie(r io.ReadSeeker, start, length int64) (int64, int64, error) {
	end := start + length
	at := start
	movie := false
	var h [16]byte
	for end-at >= 8 {
		if _, err := r.Seek(at, io.SeekStart); err != nil {
			return 0, 0, err
		}
		if _, err := io.ReadFull(r, h[:8]); err != nil {
			return 0, 0, err
		}
		size := int64(binary.BigEndian.Uint32(h[:]))
		typ := string(h[4:8])
		if size == 1 && end-at >= 16 {
			if _, err := io.ReadFull(r, h[8:16]); err != nil {
				return 0, 0, err
			}
			l := binary.BigEndian.Uint64(h[8:])
			if l > 1<<62 {
				size = -1
			} else {
				size = int64(l)
			}
		} else if size == 0 {
			size = end - at
		}
		if size < 8 || size > end-at || (at == start && typ != "ftyp") {
			break
		}
		movie = movie || typ == "moov"
		at += size
	}
	if at == start {
		return 0, 0, invalid("the picture no longer contains its declared Motion Photo video")
	}
	if !movie {
		return 0, 0, invalid("the Motion Photo video has no movie header")
	}
	return start, at - start, nil
}
