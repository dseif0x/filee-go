package geo

// Garmin FIT activity records. The file and record limits are checked before
// decoding; fields outside the activity profile (including developer fields)
// are consumed without adding them to the output.

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
)

const (
	fitMaxFileBytes = 64 << 20
	fitMaxRecords   = 2_000_000
	fitEpochOffset  = 631065600
)

type fitRecord struct {
	time                      *time.Time
	latitude, longitude       *float64
	elevation                 *float64
	heartRate, cadence, power *int
	speed, distance           *float64
}

type fitActivity struct {
	records []fitRecord
	// sessionStart is the earliest session start.
	sessionStart *time.Time
	// sports are the session sports in file order, each once.
	sports []string
}

func (a *fitActivity) addSession(start *time.Time, sport *int) {
	if start != nil && (a.sessionStart == nil || start.Before(*a.sessionStart)) {
		a.sessionStart = start
	}
	if sport != nil {
		if name := fitSportName(*sport); !slices.Contains(a.sports, name) {
			a.sports = append(a.sports, name)
		}
	}
}

var fitFileNames = map[int]string{
	1: "device", 2: "settings", 3: "sport", 4: "activity", 5: "workout", 6: "course", 7: "schedules", 9: "weight",
	10: "totals", 11: "goals", 14: "blood_pressure", 15: "monitoring_a", 20: "activity_summary", 28: "monitoring_daily",
	32: "monitoring_b", 34: "segment", 35: "segment_list", 40: "exd_configuration", 247: "mfg_range_min", 254: "mfg_range_max",
}

var fitSportNames = map[int]string{
	0: "generic", 1: "running", 2: "cycling", 3: "transition", 4: "fitness_equipment", 5: "swimming", 6: "basketball",
	7: "soccer", 8: "tennis", 9: "american_football", 10: "training", 11: "walking", 12: "cross_country_skiing",
	13: "alpine_skiing", 14: "snowboarding", 15: "rowing", 16: "mountaineering", 17: "hiking", 18: "multisport",
	19: "paddling", 20: "flying", 21: "e_biking", 22: "motorcycling", 23: "boating", 24: "driving", 25: "golf",
	26: "hang_gliding", 27: "horseback_riding", 28: "hunting", 29: "fishing", 30: "inline_skating", 31: "rock_climbing",
	32: "sailing", 33: "ice_skating", 34: "sky_diving", 35: "snowshoeing", 36: "snowmobiling", 37: "stand_up_paddleboarding",
	38: "surfing", 39: "wakeboarding", 40: "water_skiing", 41: "kayaking", 42: "rafting", 43: "windsurfing", 44: "kitesurfing",
	45: "tactical", 46: "jumpmaster", 47: "boxing", 48: "floor_climbing", 49: "baseball", 53: "diving", 56: "shooting",
	58: "winter_sport", 59: "grinding", 62: "hiit", 63: "video_gaming", 64: "racket", 65: "wheelchair_push_walk",
	66: "wheelchair_push_run", 67: "meditation", 68: "para_sport", 69: "disc_golf", 70: "team_sport", 71: "cricket",
	72: "rugby", 73: "hockey", 74: "lacrosse", 75: "volleyball", 76: "water_tubing", 77: "wakesurfing", 78: "water_sport",
	79: "archery", 80: "mixed_martial_arts", 81: "motor_sports", 82: "snorkeling", 83: "dance", 84: "jump_rope",
	85: "pool_apnea", 86: "mobility", 87: "geocaching", 88: "canoeing", 254: "all",
}

func fitSportName(sport int) string {
	if n, ok := fitSportNames[sport]; ok {
		return n
	}
	return strconv.Itoa(sport)
}

// fitToDataset makes one activity one point or one track; records without positions stay in the CSV only.
func fitToDataset(a *fitActivity, path string) (*Dataset, error) {
	var positioned []fitRecord
	for _, r := range a.records {
		if r.latitude != nil && r.longitude != nil {
			positioned = append(positioned, r)
		}
	}
	if len(positioned) == 0 {
		return nil, convert.Refuse("error.fit_no_positions", "")
	}
	var props []Prop
	if len(a.sports) > 0 {
		props = append(props, Prop{"type", strings.Join(a.sports, ", ")})
	}
	var start *time.Time
	if len(positioned) == 1 {
		start = positioned[0].time
	} else if a.sessionStart != nil {
		start = a.sessionStart
	} else {
		for _, r := range a.records {
			if r.time != nil {
				start = r.time
				break
			}
		}
	}
	if start != nil {
		props = append(props, Prop{"time", formatTime(*start)})
	}
	points := make([]Coord, len(positioned))
	anyTime := false
	times := make([]*time.Time, len(positioned))
	for i, r := range positioned {
		z := math.NaN()
		if r.elevation != nil {
			z = *r.elevation
		}
		points[i] = Coord{*r.longitude, *r.latitude, z}
		times[i] = r.time
		anyTime = anyTime || r.time != nil
	}
	var g Geometry
	if len(points) == 1 {
		g = &Point{points[0]}
	} else {
		line := &LineString{Points: points}
		if anyTime {
			line.Times = times
		}
		g = line
	}
	ds := newDataset(baseName(path))
	ds.SparseHeights = true
	ds.Features = append(ds.Features, &Feature{Geometry: g, Properties: props})
	return ds, nil
}

func fitDamaged(detail string) error { return convert.Refuse("error.fit_damaged", detail) }

type fitField struct{ number, size, typ byte }

type fitDefinition struct {
	message        int
	bigEndian      bool
	fields         []fitField
	developerBytes int
}

// readFit reads one complete, CRC-checked FIT activity in file order.
func readFit(ctx context.Context, path string, maxBytes int64, maxRecords int) (*fitActivity, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	header := make([]byte, 14)
	headerLength, _ := io.ReadFull(f, header)
	if headerLength < 12 || header[0] < 12 || string(header[8:12]) != ".FIT" {
		return nil, convert.Refuse("error.fit_not_fit", "")
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	length := st.Size()
	if length > maxBytes {
		return nil, convert.Refuse("error.fit_too_large", "64 MB")
	}
	size := int(header[0])
	if size >= 14 && headerLength >= 14 {
		if crc := binary.LittleEndian.Uint16(header[12:]); crc != 0 && crc != fitCrc(header[:12]) {
			return nil, fitDamaged("header CRC")
		}
	}
	dataLength := int64(binary.LittleEndian.Uint32(header[4:8]))
	end := int64(size) + dataLength
	if end+2 > length {
		return nil, fitDamaged("cut short")
	}
	data := make([]byte, length)
	if _, err := f.ReadAt(data, 0); err != nil && err != io.EOF {
		return nil, err
	}
	if fitCrc(data[:end]) != binary.LittleEndian.Uint16(data[end:]) {
		return nil, fitDamaged("CRC")
	}
	if end+2 < length {
		if length-end-2 >= 12 && bytes.Equal(data[end+10:end+14], []byte(".FIT")) {
			return nil, convert.Refuse("error.fit_chained", "")
		}
		return nil, fitDamaged("extra bytes")
	}

	activity := &fitActivity{}
	var definitions [16]*fitDefinition
	position := size
	limit := int(end)
	var lastTime *uint32
	var fileType *int
	fileIDSeen := false
	recordCount := 0
	require := func(n int) error {
		if n > limit-position {
			return fitDamaged("cut short")
		}
		return nil
	}
	for position < limit {
		if recordCount&0x3FF == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		recordHeader := data[position]
		position++
		compressed := recordHeader&0x80 != 0
		local := int(recordHeader & 15)
		if compressed {
			local = int(recordHeader>>5) & 3
		}
		if !compressed && recordHeader&0x40 != 0 {
			if err := require(5); err != nil {
				return nil, err
			}
			position++ // reserved
			architecture := data[position]
			position++
			if architecture > 1 {
				return nil, fitDamaged("architecture")
			}
			bigEndian := architecture == 1
			message := int(fitRead16(data[position:], bigEndian))
			position += 2
			fieldCount := int(data[position])
			position++
			if err := require(fieldCount * 3); err != nil {
				return nil, err
			}
			fields := make([]fitField, fieldCount)
			for i := range fields {
				fields[i] = fitField{data[position], data[position+1], data[position+2]}
				position += 3
			}
			developerBytes := 0
			if recordHeader&0x20 != 0 {
				if err := require(1); err != nil {
					return nil, err
				}
				count := int(data[position])
				position++
				if err := require(count * 3); err != nil {
					return nil, err
				}
				for i := 0; i < count; i++ {
					developerBytes += int(data[position+1])
					position += 3
				}
			}
			definitions[local] = &fitDefinition{message, bigEndian, fields, developerBytes}
			continue
		}
		def := definitions[local]
		if def == nil {
			return nil, fitDamaged("undefined local message " + strconv.Itoa(local))
		}
		var timestamp *uint32
		if compressed && lastTime != nil {
			previous := *lastTime
			offset := uint32(recordHeader & 31)
			reconstructed := previous&^31 + offset
			if offset < previous&31 {
				reconstructed += 32
			}
			if reconstructed >= 0x10000000 {
				timestamp = &reconstructed
			}
		}
		var lat, lon, height, enhancedHeight, speed, enhancedSpeed, distance *float64
		var heartRate, cadence, power, sport *int
		var startTime *uint32
		for _, field := range def.fields {
			if err := require(int(field.size)); err != nil {
				return nil, err
			}
			value, ok := fitReadNumber(data[position:position+int(field.size)], field.typ, def.bigEndian)
			position += int(field.size)
			// A value is read in the base type the file defines for it. Unless it
			// is a whole number within the range of the field's Profile type
			// (short of that type's invalid value), it is invalid whatever that base type is.
			if !ok {
				continue
			}
			if lo, hi, has := fitProfileRange(def.message, int(field.number)); has && !(value >= lo && value <= hi && value == math.Floor(value)) {
				continue
			}
			if field.number == 253 && value >= 0x10000000 && value < 0xFFFFFFFF {
				t := uint32(value)
				timestamp, lastTime = &t, &t
			}
			v := value
			switch {
			case def.message == 0 && field.number == 0:
				if !fileIDSeen && v <= 254 {
					i := int(v)
					fileType = &i
				}
			case def.message == 18 && field.number == 2:
				if v >= 0x10000000 && v < 0xFFFFFFFF {
					t := uint32(v)
					startTime = &t
				}
			case def.message == 18 && field.number == 5:
				if v <= 254 {
					i := int(v)
					sport = &i
				}
			case def.message == 20:
				switch field.number {
				case 0:
					lat = ptrF(v * 180 / 2147483648.0)
				case 1:
					lon = ptrF(v * 180 / 2147483648.0)
				case 2:
					height = ptrF(v/5 - 500)
				case 3:
					heartRate = ptrI(int(v))
				case 4:
					cadence = ptrI(int(v))
				case 5:
					distance = ptrF(v / 100)
				case 6:
					speed = ptrF(v / 1000)
				case 7:
					power = ptrI(int(v))
				case 73:
					enhancedSpeed = ptrF(v / 1000)
				case 78:
					enhancedHeight = ptrF(v/5 - 500)
				}
			}
		}
		if err := require(def.developerBytes); err != nil {
			return nil, err
		}
		position += def.developerBytes
		if timestamp != nil {
			lastTime = timestamp
		}
		switch def.message {
		case 0:
			fileIDSeen = true
		case 18:
			activity.addSession(fitDate(startTime), sport)
		case 20:
			if recordCount >= maxRecords {
				return nil, convert.Refuse("error.fit_too_large", "2000000 records")
			}
			recordCount++
			if lat == nil || lon == nil || *lat < -90 || *lat > 90 || *lon < -180 || *lon > 180 {
				lat, lon = nil, nil
			}
			elevation := enhancedHeight
			if elevation == nil {
				elevation = height
			}
			recordSpeed := enhancedSpeed
			if recordSpeed == nil {
				recordSpeed = speed
			}
			// A record with nothing a CSV row or a track uses (a height alone is neither) is counted but not kept.
			if timestamp == nil && lat == nil && heartRate == nil && cadence == nil && power == nil && recordSpeed == nil && distance == nil {
				break
			}
			activity.records = append(activity.records, fitRecord{fitDate(timestamp), lat, lon, elevation, heartRate, cadence, power, recordSpeed, distance})
		}
	}
	if !fileIDSeen || fileType == nil {
		return nil, convert.Refuse("error.fit_no_file_id", "")
	}
	if *fileType != 4 {
		name, ok := fitFileNames[*fileType]
		if !ok {
			name = "type " + strconv.Itoa(*fileType)
		}
		return nil, convert.Refuse("error.fit_not_activity", name)
	}
	return activity, nil
}

func ptrF(v float64) *float64 { return &v }
func ptrI(v int) *int         { return &v }

// fitProfileRange is the valid raw values of each field read here: its Profile
// type's range short of that type's invalid value.
func fitProfileRange(message, number int) (float64, float64, bool) {
	switch {
	case message == 20 && (number == 0 || number == 1):
		return math.MinInt32, math.MaxInt32 - 1, true
	case number == 253 || message == 18 && number == 2 || message == 20 && (number == 5 || number == 73 || number == 78):
		return 0, math.MaxUint32 - 1, true
	case message == 20 && (number == 2 || number == 6 || number == 7):
		return 0, math.MaxUint16 - 1, true
	case message == 0 && number == 0 || message == 18 && number == 5 || message == 20 && (number == 3 || number == 4):
		return 0, math.MaxUint8 - 1, true
	}
	return 0, 0, false
}

func fitDate(seconds *uint32) *time.Time {
	if seconds == nil {
		return nil
	}
	t := time.Unix(int64(*seconds)+fitEpochOffset, 0).UTC()
	return &t
}

func fitRead16(b []byte, bigEndian bool) uint16 {
	if bigEndian {
		return binary.BigEndian.Uint16(b)
	}
	return binary.LittleEndian.Uint16(b)
}

func fitReadNumber(b []byte, baseType byte, bigEndian bool) (float64, bool) {
	t := baseType & 0x1F
	width := 0
	switch t {
	case 0, 1, 2, 7, 10, 13:
		width = 1
	case 3, 4, 11:
		width = 2
	case 5, 6, 8, 12:
		width = 4
	case 9, 14, 15, 16:
		width = 8
	}
	if width == 0 || len(b) < width || len(b)%width != 0 || t == 7 {
		return 0, false
	}
	b = b[:width]
	var order binary.ByteOrder = binary.LittleEndian
	if bigEndian {
		order = binary.BigEndian
	}
	var raw uint64
	switch width {
	case 1:
		raw = uint64(b[0])
	case 2:
		raw = uint64(order.Uint16(b))
	case 4:
		raw = uint64(order.Uint32(b))
	default:
		raw = order.Uint64(b)
	}
	if t == 8 || t == 9 {
		if width == 4 && raw == math.MaxUint32 || width == 8 && raw == math.MaxUint64 {
			return 0, false
		}
		var v float64
		if t == 8 {
			v = float64(math.Float32frombits(uint32(raw)))
		} else {
			v = math.Float64frombits(raw)
		}
		return v, isFinite(v) // NaN and infinities pass no range check
	}
	zInvalid := t == 10 || t == 11 || t == 12 || t == 16
	if zInvalid && raw == 0 {
		return 0, false
	}
	if t == 1 || t == 3 || t == 5 || t == 14 {
		var signed, maxSigned int64
		switch width {
		case 1:
			signed, maxSigned = int64(int8(raw)), math.MaxInt8
		case 2:
			signed, maxSigned = int64(int16(raw)), math.MaxInt16
		case 4:
			signed, maxSigned = int64(int32(raw)), math.MaxInt32
		default:
			signed, maxSigned = int64(raw), math.MaxInt64
		}
		if signed == maxSigned {
			return 0, false
		}
		return float64(signed), true
	}
	var maxUnsigned uint64 = math.MaxUint64
	switch width {
	case 1:
		maxUnsigned = math.MaxUint8
	case 2:
		maxUnsigned = math.MaxUint16
	case 4:
		maxUnsigned = math.MaxUint32
	}
	if raw == maxUnsigned && !zInvalid {
		return 0, false
	}
	return float64(raw), true
}

// writeFitCsv writes one CSV row per kept record, keeping file order and empty sensor cells.
func writeFitCsv(a *fitActivity, path string) error {
	if len(a.records) == 0 {
		return convert.Refuse("error.fit_no_records", "")
	}
	header := []string{"time", "longitude", "latitude", "elevation", "heart_rate", "cadence", "power", "speed", "distance"}
	rows := make(chan []string)
	go func() {
		defer close(rows)
		for _, r := range a.records {
			row := make([]string, 9)
			if r.time != nil {
				row[0] = formatTime(*r.time)
			}
			if r.longitude != nil {
				row[1] = degrees(*r.longitude)
			}
			if r.latitude != nil {
				row[2] = degrees(*r.latitude)
			}
			if r.elevation != nil {
				row[3] = height(*r.elevation)
			}
			if r.heartRate != nil {
				row[4] = strconv.Itoa(*r.heartRate)
			}
			if r.cadence != nil {
				row[5] = strconv.Itoa(*r.cadence)
			}
			if r.power != nil {
				row[6] = strconv.Itoa(*r.power)
			}
			if r.speed != nil {
				row[7] = number(*r.speed)
			}
			if r.distance != nil {
				row[8] = number(*r.distance)
			}
			rows <- row
		}
	}()
	err := writeCSVRows(path, header, rows)
	for range rows {
	}
	return err
}

// fitCrc is FIT's 16-entry nibble CRC over the complete supplied bytes.
func fitCrc(b []byte) uint16 {
	table := [16]uint16{0x0000, 0xCC01, 0xD801, 0x1400, 0xF001, 0x3C00, 0x2800, 0xE401,
		0xA001, 0x6C00, 0x7800, 0xB401, 0x5000, 0x9C01, 0x8801, 0x4400}
	var crc uint16
	for _, v := range b {
		crc = (crc >> 4) ^ table[(crc^uint16(v))&0xF]
		crc = (crc >> 4) ^ table[(crc^uint16(v>>4))&0xF]
	}
	return crc
}
