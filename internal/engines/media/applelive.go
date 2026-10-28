package media

// Apple Live Photo pairing: the content identifier the photo (Apple maker
// note) and its MOV (QuickTime metadata) share, read from a pair and written
// into a new one, with the movie's timed "still image time" marking the photo.

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"strings"

	"github.com/google/uuid"
)

const (
	contentIdentifier = "com.apple.quicktime.content.identifier"
	stillImageTime    = "com.apple.quicktime.still-image-time"
	maxMovieHeader    = 16 << 20
	// Tag 17 of Apple's maker note: the Live Photo's content identifier.
	contentIdentifierTag = 17
	// "Signed 8-bit integer" among QuickTime's well-known data types.
	wellKnownInt8 = 65
)

// ImageIdentifier is the content identifier in a photo's Apple maker note, or "".
func ImageIdentifier(path string) string {
	tiff := PhotoExif(path)
	if tiff == nil {
		return ""
	}
	return ReadMakerNote(TiffMakerNote(tiff))
}

// ReadMakerNote reads tag 17 of an Apple maker note; "" when it has none.
func ReadMakerNote(d []byte) string {
	if len(d) < 16 || !bytes.HasPrefix(d, []byte("Apple iOS\x00")) {
		return ""
	}
	var o binary.ByteOrder
	switch string(d[12:14]) {
	case "II":
		o = binary.LittleEndian
	case "MM":
		o = binary.BigEndian
	default:
		return ""
	}
	n := int(o.Uint16(d[14:]))
	if 16+n*12 > len(d) {
		return ""
	}
	for i := 0; i < n; i++ {
		at := 16 + i*12
		if o.Uint16(d[at:]) != contentIdentifierTag || o.Uint16(d[at+2:]) != 2 {
			continue
		}
		// Apple maker-note offsets are relative to the note itself.
		l, off := int64(o.Uint32(d[at+4:])), int64(o.Uint32(d[at+8:]))
		if l < 36 || l > 128 || off > int64(len(d)) || l > int64(len(d))-off {
			return ""
		}
		id, err := uuid.Parse(strings.TrimRight(string(d[off:off+l]), "\x00"))
		if err != nil {
			return ""
		}
		return id.String()
	}
	return ""
}

// MakerNote is an Apple maker note holding only identifier, laid out as an
// iPhone writes its own: "Apple iOS", version 1, big-endian, one entry.
func MakerNote(identifier string) []byte {
	id := []byte(strings.ToUpper(uuid.MustParse(identifier).String()) + "\x00")
	const valueOffset = 14 + 2 + 12 + 4
	b := []byte("Apple iOS\x00\x00\x01MM")
	b = append(b, u16(1)...)
	b = append(b, u16(contentIdentifierTag)...)
	b = append(b, u16(2)...)
	b = append(b, u32(uint32(len(id)))...)
	b = append(b, u32(valueOffset)...)
	b = append(b, u32(0)...)
	return append(b, id...)
}

// MovieIdentifier is the content identifier in a movie's QuickTime metadata, or "".
func MovieIdentifier(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, moov, err := ReadMovieHeader(f, maxMovieHeader)
	if err != nil {
		return "", err
	}
	children, err := Children(moov, 0)
	if err != nil {
		return "", err
	}
	// Apple's movie-level meta is a plain container; other writers put a full-box meta under udta.
	var metas [][]byte
	for _, c := range children {
		switch c.Type {
		case "meta":
			metas = append(metas, c.Payload)
		case "udta":
			if u, err := Children(c.Payload, 0); err == nil {
				for _, m := range u {
					if m.Type == "meta" {
						metas = append(metas, m.Payload)
					}
				}
			}
		}
	}
	for _, data := range metas {
		skip := 0
		if len(data) >= 4 && bytes.Equal(data[:4], []byte{0, 0, 0, 0}) {
			skip = 4
		}
		atoms, err := Children(data, skip)
		if err != nil {
			continue
		}
		keys, values := firstChild(atoms, "keys"), firstChild(atoms, "ilst")
		if keys == nil || values == nil || len(keys) < 8 {
			continue
		}
		list, err := Children(keys, 8)
		if err != nil {
			continue
		}
		index := 0
		for i, k := range list {
			if k.Type == "mdta" && string(k.Payload) == contentIdentifier {
				index = i + 1
				break
			}
		}
		if index == 0 {
			continue
		}
		vals, err := Children(values, 0)
		if err != nil {
			continue
		}
		for _, v := range vals {
			if v.Type != string(u32(uint32(index))) {
				continue
			}
			inner, err := Children(v.Payload, 0)
			if err != nil {
				continue
			}
			if item := firstChild(inner, "data"); len(item) > 8 {
				if id, err := uuid.Parse(string(item[8:])); err == nil {
					return id.String(), nil
				}
			}
		}
	}
	return "", nil
}

// TagMovie makes the MOV at path (FFmpeg's, movie header at the end) the video
// of a Live Photo, as an iPhone's are: the content identifier in the movie
// metadata and a timed metadata track whose one sample (still-image-time =
// -1) is shown at stillSeconds. Only the movie header is rewritten: the sample
// goes into a small mdat where the header was, so no chunk offset moves.
func TagMovie(path, identifier string, stillSeconds float64) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	moov, payload, err := ReadMovieHeader(f, maxMovieHeader)
	if err != nil {
		return err
	}
	total, err := streamLength(f)
	if err != nil {
		return err
	}
	if moov.End() != total {
		return invalid("the movie header must end the file")
	}
	children, err := Children(payload, 0)
	if err != nil {
		return err
	}
	var mvhd []byte
	for _, c := range children {
		if c.Type == "mvhd" {
			mvhd = c.Payload
		}
	}
	if len(mvhd) < 100 {
		return invalid("invalid movie header")
	}
	long := mvhd[0] == 1
	be := binary.BigEndian
	var timescale uint32
	var duration uint64
	var created uint32
	if long {
		timescale = be.Uint32(mvhd[20:])
		duration = be.Uint64(mvhd[24:])
	} else {
		timescale = be.Uint32(mvhd[12:])
		duration = uint64(be.Uint32(mvhd[16:]))
		created = be.Uint32(mvhd[4:])
	}
	trackID := be.Uint32(mvhd[len(mvhd)-4:])
	if timescale == 0 || trackID == 0 || trackID == math.MaxUint32 {
		return invalid("invalid movie timescale or next track id")
	}
	be.PutUint32(mvhd[len(mvhd)-4:], trackID+1)

	var video []byte
	for _, c := range children {
		if c.Type == "trak" && HandlerOf(c.Payload) == "vide" {
			video = c.Payload
			break
		}
	}
	if video == nil {
		return invalid("the movie has no video track")
	}
	vc, err := Children(video, 0)
	if err != nil {
		return err
	}
	tkhd := firstChild(vc, "tkhd")
	if len(tkhd) < 24 {
		return invalid("invalid track header")
	}
	videoID := be.Uint32(tkhd[12:])
	if tkhd[0] == 1 {
		videoID = be.Uint32(tkhd[20:])
	}

	// The still is one tick long and starts at its frame: it must lie inside the movie.
	still := uint64(math.RoundToEven(max(0, stillSeconds) * float64(timescale)))
	if duration > 0 {
		still = min(still, duration-1)
	}
	if still >= math.MaxUint32 {
		return invalid("the still image time is outside the movie")
	}
	// One sample: a 9-byte item of local key 1 (still-image-time), the int8 -1 an iPhone writes.
	sample := MakeBox(localKey(1), []byte{0xff})
	mdat := MakeBox("mdat", sample)
	track := stillImageTrack(trackID, videoID, created, timescale, uint32(still), uint64(moov.Start)+8, uint32(len(sample)))

	lastTrack := -1
	for i, c := range children {
		if c.Type == "trak" {
			lastTrack = i
		}
	}
	var boxes [][]byte
	for i, c := range children {
		// An identifier the source carried is replaced, never kept next to the new one.
		if c.Type != "meta" {
			boxes = append(boxes, MakeBox(c.Type, c.Payload))
		}
		if i == lastTrack {
			boxes = append(boxes, track)
		}
	}
	boxes = append(boxes, movieMetadata(identifier))
	if err := f.Truncate(moov.Start); err != nil {
		return err
	}
	if _, err := f.WriteAt(append(mdat, MakeBox("moov", boxes...)...), moov.Start); err != nil {
		return err
	}
	return f.Close()
}

func localKey(id uint32) string { return string(u32(id)) }

// pascal is a QuickTime component name: a length byte, then the characters.
func pascal(s string) []byte { return append([]byte{byte(len(s))}, s...) }

// movieMetadata is movie-level metadata as Apple writes it: a plain meta
// container with an "mdta" handler, the key and its UTF-8 value.
func movieMetadata(identifier string) []byte {
	return MakeBox("meta",
		MakeBox("hdlr", make([]byte, 8), []byte("mdta"), make([]byte, 14)),
		FullBox("keys", 0, 0, u32(1), MakeBox("mdta", []byte(contentIdentifier))),
		MakeBox("ilst", MakeBox(localKey(1), MakeBox("data", u32(1), u32(0), []byte(strings.ToUpper(identifier))))))
}

// stillImageTrack is the timed metadata track of the still image time, laid
// out like an iPhone's.
func stillImageTrack(id, videoID, created, timescale, still uint32, offset uint64, sampleSize uint32) []byte {
	var identity []byte
	for _, v := range []uint32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000} {
		identity = append(identity, u32(v)...)
	}
	tkhd := FullBox("tkhd", 0, 0xf, u32(created), u32(created), u32(id), u32(0), u32(still+1), make([]byte, 8),
		u16(0), u16(0), u16(0), u16(0), identity, u32(0), u32(0))
	edit := func(length uint32, mediaTime int32) []byte {
		return append(append(append(u32(length), u32(uint32(mediaTime))...), u16(1)...), u16(0)...)
	}
	edits := [][]byte{edit(1, 0)}
	if still > 0 {
		edits = [][]byte{edit(still, -1), edit(1, 0)}
	}
	elst := FullBox("elst", 0, 0, append([][]byte{u32(uint32(len(edits)))}, edits...)...)
	entry := MakeBox("mebx", make([]byte, 6), u16(1),
		MakeBox("keys", MakeBox(localKey(1),
			MakeBox("keyd", []byte("mdta"), []byte(stillImageTime)),
			MakeBox("dtyp", u32(0), u32(wellKnownInt8)))))
	var chunk []byte
	if offset <= math.MaxUint32 {
		chunk = FullBox("stco", 0, 0, u32(1), u32(uint32(offset)))
	} else {
		chunk = FullBox("co64", 0, 0, u32(1), u64(offset))
	}
	stbl := MakeBox("stbl",
		FullBox("stsd", 0, 0, u32(1), entry),
		FullBox("stts", 0, 0, u32(1), u32(1), u32(1)),
		FullBox("stsc", 0, 0, u32(1), u32(1), u32(1), u32(1)),
		FullBox("stsz", 0, 0, u32(0), u32(1), u32(sampleSize)),
		chunk)
	minf := MakeBox("minf",
		MakeBox("gmhd", FullBox("gmin", 0, 0, u16(0x40), u16(0x8000), u16(0x8000), u16(0x8000), u16(0), u16(0))),
		FullBox("hdlr", 0, 0, []byte("dhlr"), []byte("alis"), []byte("appl"), u32(0), u32(0), pascal("Core Media Data Handler")),
		MakeBox("dinf", FullBox("dref", 0, 0, u32(1), FullBox("alis", 0, 1))),
		stbl)
	mdia := MakeBox("mdia",
		FullBox("mdhd", 0, 0, u32(created), u32(created), u32(timescale), u32(1), u16(0x55c4), u16(0)),
		FullBox("hdlr", 0, 0, []byte("mhlr"), []byte("meta"), []byte("appl"), u32(1), u32(0), pascal("Core Media Metadata")),
		minf)
	return MakeBox("trak", tkhd, MakeBox("edts", elst), MakeBox("tref", MakeBox("cdsc", u32(videoID))), mdia)
}
