package cadoc

import (
	"crypto/rand"
	"fmt"
	"math"
	"strings"
	"time"
)

// HeaderDimensionAssociation is ACadSharp's Header.DimensionAssociation
// ($DIMASSOC).
type HeaderDimensionAssociation int16

const (
	HeaderDimensionAssociationCreateExplodedDimensions       HeaderDimensionAssociation = 0
	HeaderDimensionAssociationCreateNonAssociativeDimensions HeaderDimensionAssociation = 1
	HeaderDimensionAssociationCreateAssociativeDimensions    HeaderDimensionAssociation = 2
)

// HeaderVariable describes one $NAME variable for generic header IO.
type HeaderVariable struct {
	Name  string
	Codes []int
	// Field is the Header field name.
	Field string
	// IsName: the value names a table entry; empty values are not assigned.
	IsName bool
	// Ignored variables are read but not written (ACadSharp's
	// DxfReferenceType.Ignored).
	Ignored bool
	Get     func(h *Header) []any
	Set     func(h *Header, values []any)
}

var headerVariableIndex map[string]*HeaderVariable

// LookupHeaderVariable finds a variable by name ("$ANGBASE"), case-insensitive.
func LookupHeaderVariable(name string) (*HeaderVariable, bool) {
	if headerVariableIndex == nil {
		m := make(map[string]*HeaderVariable, len(HeaderVariables))
		for i := range HeaderVariables {
			m[strings.ToUpper(HeaderVariables[i].Name)] = &HeaderVariables[i]
		}
		headerVariableIndex = m
	}
	v, ok := headerVariableIndex[strings.ToUpper(name)]
	return v, ok
}

// SetVersion changes Version and the matching $ACADMAINTVER.
func (h *Header) SetVersion(v Version) {
	h.Version = v
	h.MaintenanceVersion = v.MaintenanceVersion()
}

func newGUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("{%X-%X-%X-%X-%X}", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func b2i16(b bool) int16 {
	if b {
		return 1
	}
	return 0
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int16:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case int:
		return float64(x)
	case uint8:
		return float64(x)
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

func asInt(v any) int64 {
	switch x := v.(type) {
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case uint8:
		return int64(x)
	case uint16:
		return int64(x)
	case uint64:
		return int64(x)
	case float64:
		return int64(x)
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

func asBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return asInt(v) != 0
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func asHandle(v any) uint64 {
	switch x := v.(type) {
	case uint64:
		return x
	case string:
		var h uint64
		fmt.Sscanf(x, "%X", &h)
		return h
	}
	return uint64(asInt(v))
}

// ToJulianDate converts a time to the Julian day number DXF date variables
// use (CadUtils.ToJulianCalendar). The zero time gives 0.
func ToJulianDate(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	year, month, day := t.Year(), int(t.Month()), t.Day()
	fraction := float64(day) + float64(t.Hour())/24 + float64(t.Minute())/1440 +
		(float64(t.Second())+float64(t.Nanosecond()/1e6)/1000)/86400
	if month < 3 {
		year--
		month += 12
	}
	a := year / 100
	b := 2 - a + a/4
	var c int
	if year < 0 {
		c = int(365.25*float64(year) - 0.75)
	} else {
		c = int(365.25 * float64(year))
	}
	d := int(30.6001 * float64(month+1))
	return float64(b+c+d+1720995) + fraction
}

// FromJulianDate converts a Julian day number to a time (CadUtils.FromJulianCalendar);
// values outside year 1..9999 give the zero time.
func FromJulianDate(date float64) time.Time {
	if math.IsNaN(date) || date < 1721426 || date > 5373484 {
		return time.Time{}
	}
	julian := math.Trunc(date)
	fraction := date - julian
	temp := int((julian - 1867216.25) / 36524.25)
	julian = julian + 1 + float64(temp) - float64(int(float64(temp)/4.0))
	a := int(julian) + 1524
	b := int((float64(a) - 122.1) / 365.25)
	c := int(365.25 * float64(b))
	d := int(float64(a-c) / 30.6001)
	months := d - 13
	if d < 14 {
		months = d - 1
	}
	years := b - 4715
	if months > 2 {
		years = b - 4716
	}
	days := a - c - int(30.6001*float64(d))
	hours := int(fraction * 24)
	fraction -= float64(hours) / 24
	minutes := int(fraction * 1440)
	fraction -= float64(minutes) / 1440
	secs := fraction * 86400
	s := int(secs)
	ms := int((secs - float64(s)) * 1000)
	return time.Date(years, time.Month(months), days, hours, minutes, s, ms*1e6, time.Local)
}

func durationDays(d time.Duration) float64 { return d.Hours() / 24 }

func daysDuration(days float64) time.Duration {
	if math.IsNaN(days) || math.IsInf(days, 0) || math.Abs(days) > 100000 {
		return 0
	}
	return time.Duration(days * 24 * float64(time.Hour))
}
