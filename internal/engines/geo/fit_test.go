package geo

// Garmin FIT activities, built in memory (no device recordings are checked in).

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/xdom"
)

type fitTestField struct {
	number, typ byte
	value       []byte
}

func u8(n, v byte) fitTestField { return fitTestField{n, 0, []byte{v}} }
func u16(n byte, v uint16, big bool) fitTestField {
	b := make([]byte, 2)
	if big {
		binary.BigEndian.PutUint16(b, v)
	} else {
		binary.LittleEndian.PutUint16(b, v)
	}
	return fitTestField{n, 0x84, b}
}
func u32(n byte, v uint32, big bool) fitTestField {
	b := make([]byte, 4)
	if big {
		binary.BigEndian.PutUint32(b, v)
	} else {
		binary.LittleEndian.PutUint32(b, v)
	}
	return fitTestField{n, 0x86, b}
}
func s32(n byte, v int32, big bool) fitTestField {
	f := u32(n, uint32(v), big)
	f.typ = 0x85
	return f
}

type fitBuilder struct {
	data []byte
	defs map[int][]fitTestField
	dev  map[int][]int
}

func newFitBuilder() *fitBuilder {
	return &fitBuilder{defs: map[int][]fitTestField{}, dev: map[int][]int{}}
}

func (b *fitBuilder) define(local int, message uint16, big bool, developer [][]byte, fields ...fitTestField) *fitBuilder {
	header := byte(0x40 | local)
	if len(developer) > 0 {
		header |= 0x20
	}
	arch := byte(0)
	m := binary.LittleEndian.AppendUint16(nil, message)
	if big {
		arch = 1
		m = binary.BigEndian.AppendUint16(nil, message)
	}
	b.data = append(b.data, header, 0, arch)
	b.data = append(b.data, m...)
	b.data = append(b.data, byte(len(fields)))
	for _, f := range fields {
		b.data = append(b.data, f.number, byte(len(f.value)), f.typ)
	}
	var sizes []int
	if len(developer) > 0 {
		b.data = append(b.data, byte(len(developer)))
		for i, d := range developer {
			b.data = append(b.data, byte(i+1), byte(len(d)), 0)
			sizes = append(sizes, len(d))
		}
	}
	b.defs[local], b.dev[local] = fields, sizes
	return b
}

func (b *fitBuilder) message(local int, fields []fitTestField, developer ...[]byte) *fitBuilder {
	return b.add(byte(local), fields, developer)
}

func (b *fitBuilder) compressed(local int, offset byte, fields []fitTestField) *fitBuilder {
	return b.add(byte(0x80|local<<5|int(offset&31)), fields, nil)
}

func (b *fitBuilder) add(header byte, fields []fitTestField, developer [][]byte) *fitBuilder {
	b.data = append(b.data, header)
	for _, f := range fields {
		b.data = append(b.data, f.value...)
	}
	for _, d := range developer {
		b.data = append(b.data, d...)
	}
	return b
}

func (b *fitBuilder) fileID(typ byte) *fitBuilder {
	f := u8(0, typ)
	return b.define(15, 0, false, nil, f).message(15, []fitTestField{f})
}

func (b *fitBuilder) session(start uint32, sport byte) *fitBuilder {
	fields := []fitTestField{u32(2, start, false), u8(5, sport)}
	return b.define(2, 18, false, nil, fields...).message(2, fields)
}

func (b *fitBuilder) build(headerSize int, headerCrc bool) []byte {
	out := make([]byte, headerSize+len(b.data)+2)
	out[0], out[1] = byte(headerSize), 0x10
	binary.LittleEndian.PutUint32(out[4:], uint32(len(b.data)))
	copy(out[8:], ".FIT")
	if headerSize == 14 && headerCrc {
		binary.LittleEndian.PutUint16(out[12:], fitCrc(out[:12]))
	}
	copy(out[headerSize:], b.data)
	binary.LittleEndian.PutUint16(out[headerSize+len(b.data):], fitCrc(out[:headerSize+len(b.data)]))
	return out
}

func fitRawData(payload ...byte) []byte {
	b := &fitBuilder{data: payload}
	return b.build(14, true)
}

func saveFit(t testing.TB, data []byte) string {
	path := filepath.Join(t.TempDir(), "ride.fit")
	os.WriteFile(path, data, 0o644)
	return path
}

func readFitBytes(t *testing.T, data []byte) *fitActivity {
	t.Helper()
	a, err := readFit(context.Background(), saveFit(t, data), fitMaxFileBytes, fitMaxRecords)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func fitTime(v uint32) time.Time { return time.Unix(int64(v)+fitEpochOffset, 0).UTC() }

const firstStamp = 0x1000003B

func TestFitCrcMatchesThePublishedVector(t *testing.T) {
	b, _ := hex.DecodeString("0E104308780609002E464954")
	if fitCrc(b) != 0x8596 {
		t.Fatalf("%x", fitCrc(b))
	}
}

func TestFitRecordValuesEnhancedPrecedenceBigEndianAndSessions(t *testing.T) {
	firstFields := []fitTestField{u32(253, firstStamp, false), s32(0, 0x20000000, false), s32(1, 0x40000000, false), u16(2, 3000, false),
		u8(3, 72), u8(4, 88), u32(5, 12345, false), u16(6, 4321, false), u16(7, 210, false), u32(73, 5678, false), u32(78, 4000, false)}
	next := append([]fitTestField{u32(253, 0x10000040, false)}, firstFields[1:]...)
	a := readFitBytes(t, newFitBuilder().fileID(4).define(0, 20, false, nil, firstFields...).message(0, firstFields).
		session(0x10000035, 1).session(0x10000030, 2).message(0, next).build(14, true))
	if len(a.records) != 2 {
		t.Fatal(len(a.records))
	}
	r := a.records[0]
	if !r.time.Equal(fitTime(firstStamp)) || *r.latitude != 45 || *r.longitude != 90 || *r.elevation != 300 || *r.heartRate != 72 ||
		*r.cadence != 88 || *r.power != 210 || *r.speed != 5.678 || *r.distance != 123.45 {
		t.Fatalf("%+v", r)
	}
	if !a.records[1].time.Equal(fitTime(0x10000040)) || !a.sessionStart.Equal(fitTime(0x10000030)) || strings.Join(a.sports, ",") != "running,cycling" {
		t.Fatal(a.sports)
	}
	if fitSportName(200) != "200" || fitSportName(88) != "canoeing" {
		t.Fatal("sport names")
	}
	big := []fitTestField{u32(253, firstStamp, true), s32(0, 0x20000000, true), s32(1, 0x40000000, true), u32(73, 5678, true)}
	b := readFitBytes(t, newFitBuilder().fileID(4).define(0, 20, true, nil, big...).message(0, big).build(14, true)).records[0]
	if !b.time.Equal(*r.time) || *b.latitude != 45 || *b.longitude != 90 || *b.speed != 5.678 {
		t.Fatal("big endian")
	}
}

func TestFitInvalidValuesDoNotRemoveOtherRecordData(t *testing.T) {
	fields := []fitTestField{u32(253, firstStamp, false), s32(0, 0x7FFFFFFF, false), s32(1, 0x40000000, false), u16(2, 0xFFFF, false), u8(3, 0xFF), u8(4, 80)}
	outside := append([]fitTestField{}, fields...)
	outside[1] = s32(0, 0x41000000, false)
	a := readFitBytes(t, newFitBuilder().fileID(4).define(0, 20, false, nil, fields...).message(0, fields).message(0, outside).build(14, true))
	if len(a.records) != 2 {
		t.Fatal(len(a.records))
	}
	for _, r := range a.records {
		if r.latitude != nil || r.longitude != nil || r.elevation != nil || r.heartRate != nil || *r.cadence != 80 || !r.time.Equal(fitTime(firstStamp)) {
			t.Fatalf("%+v", r)
		}
	}
}

func TestFitCompressedTimestampsRollOver(t *testing.T) {
	full := []fitTestField{u32(253, firstStamp, false), s32(0, 0x20000000, false), s32(1, 0x40000000, false)}
	position := []fitTestField{s32(0, 0x30000000, false), s32(1, 0x40000000, false)}
	changed := []fitTestField{u32(253, 0x10000055, false), s32(0, 0x20000000, false), s32(1, 0x40000000, false)}
	a := readFitBytes(t, newFitBuilder().fileID(4).
		define(0, 20, false, nil, full...).message(0, full).
		define(1, 20, false, nil, position...).
		compressed(1, 0x1D, position).compressed(1, 0x05, position).
		message(0, full).compressed(0, 0x06, changed).
		compressed(1, 0x17, position).build(14, true))
	want := []uint32{0x1000003B, 0x1000003D, 0x10000045, 0x1000003B, 0x10000055, 0x10000057}
	lats := []float64{45, 67.5, 67.5, 45, 45, 67.5}
	if len(a.records) != len(want) {
		t.Fatal(len(a.records))
	}
	for i, r := range a.records {
		if !r.time.Equal(fitTime(want[i])) || *r.latitude != lats[i] {
			t.Fatalf("%d: %v %v", i, r.time, *r.latitude)
		}
	}
}

func TestFitHeadersAndDeveloperMessagesLeaveRecordsUnchanged(t *testing.T) {
	fields := []fitTestField{u32(253, firstStamp, false), s32(0, 0x20000000, false), s32(1, 0x40000000, false)}
	for _, c := range []struct {
		size int
		crc  bool
	}{{12, false}, {14, false}, {14, true}} {
		b := newFitBuilder().fileID(4).
			define(3, 207, false, nil, u8(0, 1)).message(3, []fitTestField{u8(0, 1)}).
			define(3, 206, false, nil, u8(0, 1)).message(3, []fitTestField{u8(0, 1)}).
			define(3, 132, false, nil, u8(3, 99)).message(3, []fitTestField{u8(3, 99)}).
			define(0, 20, false, [][]byte{{9, 8}}, fields...).message(0, fields, []byte{9, 8})
		a := readFitBytes(t, b.build(c.size, c.crc))
		if len(a.records) != 1 || !a.records[0].time.Equal(fitTime(firstStamp)) || *a.records[0].latitude != 45 || a.records[0].heartRate != nil {
			t.Fatal(c)
		}
	}
}

func TestFitCorruptionsAreRefusedWithTheFirstMatchingKey(t *testing.T) {
	valid := newFitBuilder().fileID(4).build(14, true)
	flip := func(b []byte, i int) []byte {
		c := append([]byte(nil), b...)
		if i < 0 {
			i += len(c)
		}
		c[i] ^= 1
		return c
	}
	for _, c := range []struct {
		data        []byte
		key, detail string
	}{
		{[]byte{}, "error.fit_not_fit", ""},
		{[]byte("SIMPLE  = FIT"), "error.fit_not_fit", ""},
		{flip(valid, 12), "error.fit_damaged", "header CRC"},
		{valid[:len(valid)-1], "error.fit_damaged", "cut short"},
		{valid[:12], "error.fit_damaged", "cut short"},
		{flip(valid, -1), "error.fit_damaged", "CRC"},
		{append(append([]byte(nil), valid...), 0x42), "error.fit_damaged", "extra bytes"},
		{append(append([]byte(nil), valid...), valid...), "error.fit_chained", ""},
		{fitRawData(0x40, 0, 2, 0, 0, 0), "error.fit_damaged", "architecture"},
		{fitRawData(3), "error.fit_damaged", "undefined local message 3"},
		{newFitBuilder().build(14, true), "error.fit_no_file_id", ""},
		{newFitBuilder().fileID(255).build(14, true), "error.fit_no_file_id", ""},
		{fitRawData(0x40), "error.fit_damaged", "cut short"},
		{fitRawData(0x40, 0, 0, 20, 0, 1, 0, 4, 0x86, 0), "error.fit_damaged", "cut short"},
	} {
		_, err := readFit(context.Background(), saveFit(t, c.data), fitMaxFileBytes, fitMaxRecords)
		var r *convert.RefusedError
		if !errors.As(err, &r) || r.Key != c.key || r.Detail != c.detail {
			t.Fatalf("%s %q: %v", c.key, c.detail, err)
		}
	}
	// A huge declared data size is refused before reading it.
	huge := newFitBuilder().fileID(4).build(14, false)
	binary.LittleEndian.PutUint32(huge[4:], 0x7FFFFFFF)
	if _, err := readFit(context.Background(), saveFit(t, huge), fitMaxFileBytes, fitMaxRecords); err == nil {
		t.Fatal("accepted")
	}
}

func TestFitLimitsAndNonActivityFiles(t *testing.T) {
	fields := []fitTestField{u32(253, firstStamp, false)}
	b := newFitBuilder().fileID(4).define(0, 20, false, nil, fields...)
	for i := 0; i < 5; i++ {
		b.message(0, fields)
	}
	data := b.build(14, true)
	path := saveFit(t, data)
	if _, err := readFit(context.Background(), path, fitMaxFileBytes, 5); err != nil {
		t.Fatal(err)
	}
	var r *convert.RefusedError
	if _, err := readFit(context.Background(), path, fitMaxFileBytes, 4); !errors.As(err, &r) || r.Key != "error.fit_too_large" {
		t.Fatal(err)
	}
	if _, err := readFit(context.Background(), path, int64(len(data)-1), 5); !errors.As(err, &r) || r.Key != "error.fit_too_large" {
		t.Fatal(err)
	}
	for typ, name := range map[byte]string{6: "course", 9: "weight", 100: "type 100"} {
		_, err := readFit(context.Background(), saveFit(t, newFitBuilder().fileID(typ).define(0, 20, false, nil, fields...).message(0, fields).build(14, true)), fitMaxFileBytes, fitMaxRecords)
		if !errors.As(err, &r) || r.Key != "error.fit_not_activity" || r.Detail != name {
			t.Fatal(typ, err)
		}
	}
	// Random bytes only ever give FIT refusals, never a crash.
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 2000; i++ {
		data := append([]byte(nil), valid()...)
		for k := 0; k < 1+rng.Intn(6); k++ {
			data[14+rng.Intn(len(data)-14)] = byte(rng.Intn(256))
		}
		binary.LittleEndian.PutUint16(data[len(data)-2:], fitCrc(data[:len(data)-2]))
		_, err := readFit(context.Background(), saveFit(t, data), fitMaxFileBytes, fitMaxRecords)
		if err != nil && !errors.As(err, &r) {
			t.Fatal(err)
		}
	}
}

func valid() []byte {
	f := []fitTestField{u32(253, firstStamp, false), s32(0, 0x20000000, false), s32(1, 0x40000000, false), u16(2, 3000, false)}
	return newFitBuilder().fileID(4).session(firstStamp, 1).define(0, 20, false, nil, f...).message(0, f).message(0, f).build(14, true)
}

// fitTrack makes a FIT file of records (time, lat, lon, height; 0 = left out).
func fitTrack(t *testing.T, b *fitBuilder, records ...[4]uint32) string {
	for _, r := range records {
		var fields []fitTestField
		if r[0] != 0 {
			fields = append(fields, u32(253, r[0], false))
		}
		if r[1] != 0 {
			fields = append(fields, s32(0, int32(r[1]), false))
		}
		if r[2] != 0 {
			fields = append(fields, s32(1, int32(r[2]), false))
		}
		if r[3] != 0 {
			fields = append(fields, u16(2, uint16(r[3]), false))
		}
		b.define(0, 20, false, nil, fields...).message(0, fields)
	}
	return saveFit(t, b.build(14, true))
}

func three(times, heights bool) [][4]uint32 {
	out := [][4]uint32{{firstStamp, 0x20000000, 0x40000000, 3000}, {firstStamp + 1, 0x30000000, 0x20000000, 3010}, {firstStamp + 2, 0x10000000, 0x30000000, 3020}}
	for i := range out {
		if !times {
			out[i][0] = 0
		}
		if !heights {
			out[i][3] = 0
		}
	}
	return out
}

func xmlRoot(t *testing.T, path string) *xdom.Node {
	data, _ := os.ReadFile(path)
	root, err := xdom.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestFitTrackToGpxKeepsOrderHeightsAndPointTimes(t *testing.T) {
	gpx := convertOK(t, fitTrack(t, newFitBuilder().fileID(4), three(true, true)...), "gpx")[0]
	root := xmlRoot(t, gpx)
	points := root.DescendantsLocal("trkpt")
	if len(root.DescendantsLocal("trk")) != 1 || len(points) != 3 {
		t.Fatal(len(points))
	}
	var lat, lon, ele, tm []string
	for _, p := range points {
		lat, lon = append(lat, p.A("lat")), append(lon, p.A("lon"))
		ele, tm = append(ele, p.ChildLocal("ele").Value()), append(tm, p.ChildLocal("time").Value())
	}
	if strings.Join(lat, " ") != "45 67.5 22.5" || strings.Join(lon, " ") != "90 45 67.5" || strings.Join(ele, " ") != "100 102 104" ||
		tm[0] != formatTime(fitTime(firstStamp)) || tm[2] != formatTime(fitTime(firstStamp+2)) {
		t.Fatal(lat, lon, ele, tm)
	}
}

func TestFitGpxNoticeOnlyWhenNoPointKeepsATime(t *testing.T) {
	if r := convertTo(t, fitTrack(t, newFitBuilder().fileID(4).session(firstStamp-10, 1), three(true, true)...), "gpx"); r.err != nil || r.notice != "" {
		t.Fatal(r.err, r.notice)
	}
	if r := convertTo(t, fitTrack(t, newFitBuilder().fileID(4).session(firstStamp, 1), three(false, true)...), "gpx"); r.notice != "notice.geo_gpx_lossy" {
		t.Fatal(r.notice)
	}
	point := convertOK(t, fitTrack(t, newFitBuilder().fileID(4), [4]uint32{firstStamp, 0x20000000, 0x40000000, 3000}), "gpx")[0]
	if w := xmlRoot(t, point).DescendantsLocal("wpt"); len(w) != 1 || w[0].ChildLocal("time").Value() != formatTime(fitTime(firstStamp)) {
		t.Fatal(len(w))
	}
}

func TestFitWithoutPositionsRefusesSpatialOutputsButKeepsTheTable(t *testing.T) {
	path := fitTrack(t, newFitBuilder().fileID(4), [4]uint32{firstStamp, 0, 0, 3000})
	if key, _ := convertFails(t, path, "geojson"); key != "error.fit_no_positions" {
		t.Fatal(key)
	}
	csv, _ := os.ReadFile(convertOK(t, path, "csv")[0])
	if !strings.Contains(string(csv), formatTime(fitTime(firstStamp))+",,,100,") {
		t.Fatalf("%q", csv)
	}
}

func TestFitKmlTrackAndGeoJSONTimes(t *testing.T) {
	path := fitTrack(t, newFitBuilder().fileID(4), three(true, true)...)
	root := xmlRoot(t, convertOK(t, path, "kml")[0])
	if len(root.DescendantsLocal("Track")) != 1 || len(root.DescendantsLocal("when")) != 3 || root.DescendantsLocal("coord")[0].Value() != "90 45 100" {
		t.Fatal("track")
	}
	entries := convertOK(t, path, "kmz")
	if len(entries) != 1 {
		t.Fatal(entries)
	}
	geojson := convertOK(t, path, "geojson")[0]
	ds, err := readGeoJSON(context.Background(), geojson)
	if err != nil {
		t.Fatal(err)
	}
	if v := ds.Features[0].FindText("coordTimes"); !strings.Contains(v, formatTime(fitTime(firstStamp+1))) {
		t.Fatal(v)
	}
	// Partly missing times keep every position; missing heights are not made up.
	partial := three(true, true)
	partial[1][0] = 0
	partial[1][3] = 0
	g := toGeoJSONFeatures(t, fitTrack(t, newFitBuilder().fileID(4), partial...))[0]
	if string(g.Properties) == "" || !strings.Contains(string(g.Properties), `null`) || !strings.Contains(string(g.geometry(t).Coordinates), "[45,67.5]") {
		t.Fatal(string(g.Properties), string(g.geometry(t).Coordinates))
	}
	// Sparse heights: a shapefile drops them with a notice.
	r := convertTo(t, fitTrack(t, newFitBuilder().fileID(4), partial...), "shp")
	if r.err != nil || r.notice != "notice.geo_heights_dropped" {
		t.Fatal(r.err, r.notice)
	}
}

func TestFitCsvColumnsUnitsAndOnlySubstantiveRecords(t *testing.T) {
	fields := []fitTestField{u32(253, firstStamp, false), s32(0, 0x20000000, false), s32(1, 0x40000000, false), u16(2, 3000, false),
		u8(3, 72), u8(4, 88), u16(7, 210, false), u16(6, 4321, false), u32(5, 12345, false)}
	heightOnly := []fitTestField{u16(2, 2585, false)}
	heightAndHeart := []fitTestField{u16(2, 2560, false), u8(3, 60)}
	a := readFitBytes(t, newFitBuilder().fileID(4).define(0, 20, false, nil, fields...).message(0, fields).
		define(1, 20, false, nil, heightOnly...).message(1, heightOnly).define(2, 20, false, nil).message(2, nil).
		define(3, 20, false, nil, heightAndHeart...).message(3, heightAndHeart).build(14, true))
	if len(a.records) != 2 {
		t.Fatal(len(a.records))
	}
	path := filepath.Join(t.TempDir(), "records.csv")
	if err := writeFitCsv(a, path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "\xEF\xBB\xBFtime,longitude,latitude,elevation,heart_rate,cadence,power,speed,distance\r\n" +
		formatTime(fitTime(firstStamp)) + ",90,45,100,72,88,210,4.321,123.45\r\n,,,12,60,,,,\r\n"
	if string(data) != want {
		t.Fatalf("%q", data)
	}
	var r *convert.RefusedError
	if err := writeFitCsv(&fitActivity{}, filepath.Join(t.TempDir(), "empty.csv")); !errors.As(err, &r) || r.Key != "error.fit_no_records" {
		t.Fatal(err)
	}
}

func TestFitSessionSportAndEarliestStartAreProperties(t *testing.T) {
	path := fitTrack(t, newFitBuilder().fileID(4).session(firstStamp-5, 1).session(firstStamp-9, 2), three(true, true)...)
	p := toGeoJSONFeatures(t, path)[0].props(t)
	if p["type"] != "running, cycling" || p["time"] != formatTime(fitTime(firstStamp-9)) {
		t.Fatal(p)
	}
}
