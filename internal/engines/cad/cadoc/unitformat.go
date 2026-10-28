package cadoc

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// UnitStyleFormat is ACadSharp's UnitStyleFormat: how dimension values are
// turned into text.
type UnitStyleFormat struct {
	LinearDecimalPlaces  int16
	AngularDecimalPlaces int16
	LinearZeroHandling   ZeroHandling
	AngularZeroHandling  AngularZeroHandling
	DecimalSeparator     string
	FeetInchesSeparator  string
	DegreesSymbol        string
	MinutesSymbol        string
	SecondsSymbol        string
	RadiansSymbol        string
	GradiansSymbol       string
	FeetSymbol           string
	InchesSymbol         string
	FractionHeightScale  float64
	FractionType         FractionFormat
}

// NewUnitStyleFormat returns ACadSharp's defaults.
func NewUnitStyleFormat() UnitStyleFormat {
	return UnitStyleFormat{LinearDecimalPlaces: 2, LinearZeroHandling: ZeroHandlingSuppressDecimalTrailingZeroes,
		DecimalSeparator: ".", FeetInchesSeparator: "-", DegreesSymbol: "°", MinutesSymbol: "'",
		SecondsSymbol: "\"", RadiansSymbol: "r", GradiansSymbol: "g", FeetSymbol: "'", InchesSymbol: "\"",
		FractionHeightScale: 1, FractionType: FractionFormatHorizontal}
}

// UnitStyleFormat returns the format of a dimension style (GetUnitStyleFormat).
func (d *DimensionStyle) UnitStyleFormat() UnitStyleFormat {
	f := NewUnitStyleFormat()
	f.LinearDecimalPlaces = max(d.DecimalPlaces, 0)
	f.AngularDecimalPlaces = d.AngularDecimalPlaces
	if f.AngularDecimalPlaces < 0 {
		f.AngularDecimalPlaces = f.LinearDecimalPlaces
	}
	sep := d.DecimalSeparator
	if sep == 0 {
		sep = '.'
	}
	f.DecimalSeparator = string(sep)
	if d.ToleranceScaleFactor > 0 {
		f.FractionHeightScale = d.ToleranceScaleFactor
	}
	f.FractionType = d.FractionFormat
	f.LinearZeroHandling = d.ZeroHandling
	f.AngularZeroHandling = d.AngularZeroHandling
	return f
}

// AlternateUnitStyleFormat is GetAlternateUnitStyleFormat.
func (d *DimensionStyle) AlternateUnitStyleFormat() UnitStyleFormat {
	f := d.UnitStyleFormat()
	f.LinearDecimalPlaces = max(d.AlternateUnitDecimalPlaces, 0)
	f.AngularDecimalPlaces = f.LinearDecimalPlaces
	f.LinearZeroHandling = d.AlternateUnitZeroHandling
	return f
}

func (f UnitStyleFormat) suppressZeroFeet() bool {
	return f.LinearZeroHandling == ZeroHandlingSuppressZeroFeetAndInches ||
		f.LinearZeroHandling == ZeroHandlingSuppressZeroFeetShowZeroInches
}

func (f UnitStyleFormat) suppressZeroInches() bool {
	return f.LinearZeroHandling == ZeroHandlingSuppressZeroFeetAndInches ||
		f.LinearZeroHandling == ZeroHandlingShowZeroFeetSuppressZeroInches
}

// zeroFormat returns decimals and whether leading / trailing zeros are
// suppressed (GetZeroHandlingFormat's "#.##" / "0.00").
func (f UnitStyleFormat) zeroFormat(angular bool) (decimals int, noLeading, noTrailing bool) {
	if angular {
		a := f.AngularZeroHandling
		return int(max(f.AngularDecimalPlaces, 0)),
			a == AngularZeroHandlingSuppressLeadingZeroes || a == AngularZeroHandlingSupressAll,
			a == AngularZeroHandlingSupressTrailingZeroes || a == AngularZeroHandlingSupressAll
	}
	z := f.LinearZeroHandling
	return int(max(f.LinearDecimalPlaces, 0)),
		z == ZeroHandlingSuppressDecimalLeadingZeroes || z == ZeroHandlingSuppressDecimalLeadingAndTrailingZeroes,
		z == ZeroHandlingSuppressDecimalTrailingZeroes || z == ZeroHandlingSuppressDecimalLeadingAndTrailingZeroes
}

// roundAway rounds half away from zero, like .NET custom numeric formats.
func roundAway(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	r := math.Round(v*p) / p
	if r == 0 {
		return 0
	}
	return r
}

// formatNumber emulates a .NET "0.00" / "#.##" style format.
func formatNumber(v float64, decimals int, noLeading, noTrailing bool, sep string) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	s := strconv.FormatFloat(roundAway(v, decimals), 'f', decimals, 64)
	if noTrailing && strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if noLeading {
		switch {
		case strings.HasPrefix(s, "0."):
			s = s[1:]
		case strings.HasPrefix(s, "-0."):
			s = "-" + s[2:]
		}
	}
	if s == "" || s == "-" {
		s = "0"
	}
	return strings.Replace(s, ".", sep, 1)
}

// ToDecimal formats a linear (or, with angular, an angular) value.
func (f UnitStyleFormat) ToDecimal(v float64, angular bool) string {
	d, l, t := f.zeroFormat(angular)
	return formatNumber(v, d, l, t, f.DecimalSeparator)
}

// ToScientific formats like "0.##E+00".
func (f UnitStyleFormat) ToScientific(v float64) string {
	d, l, t := f.zeroFormat(false)
	exp := 0
	if v != 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		exp = int(math.Floor(math.Log10(math.Abs(v))))
	}
	mant := v / math.Pow(10, float64(exp))
	if math.Abs(roundAway(mant, d)) >= 10 {
		exp++
		mant /= 10
	}
	sign := "+"
	if exp < 0 {
		sign, exp = "-", -exp
	}
	return fmt.Sprintf("%sE%s%02d", formatNumber(mant, d, l, t, "."), sign, exp)
}

func fraction(v float64, precision int) (numerator, denominator int) {
	numerator = int(math.RoundToEven((v - math.Trunc(v)) * float64(precision)))
	a, b := numerator, precision
	for b != 0 {
		a, b = b, a%b
	}
	if a <= 0 {
		a = 1
	}
	return numerator / a, precision / a
}

func fmtScale(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func (f UnitStyleFormat) feetInchesZero(feet int) string {
	if feet == 0 {
		if f.suppressZeroFeet() {
			return "0" + f.InchesSymbol
		}
		if f.suppressZeroInches() {
			return "0" + f.FeetSymbol
		}
		return "0" + f.FeetSymbol + f.FeetInchesSeparator + "0" + f.InchesSymbol
	}
	if f.suppressZeroInches() {
		return strconv.Itoa(feet) + f.FeetSymbol
	}
	return strconv.Itoa(feet) + f.FeetSymbol + f.FeetInchesSeparator + "0" + f.InchesSymbol
}

// ToArchitectural formats inches as feet-inches with fractions.
func (f UnitStyleFormat) ToArchitectural(v float64) string {
	feet := int(v / 12)
	inchesDec := v - 12*float64(feet)
	inches := int(inchesDec)
	if math.Abs(inchesDec) < 1e-12 {
		return f.feetInchesZero(feet)
	}
	num, den := fraction(inchesDec, 1<<uint(max(f.LinearDecimalPlaces, 0)))
	if num == 0 {
		if inches == 0 {
			return f.feetInchesZero(feet)
		}
		if feet == 0 {
			if f.suppressZeroFeet() {
				return strconv.Itoa(inches) + f.InchesSymbol
			}
			return "0" + f.FeetSymbol + f.FeetInchesSeparator + strconv.Itoa(inches) + f.InchesSymbol
		}
		return strconv.Itoa(feet) + f.FeetSymbol + f.FeetInchesSeparator + strconv.Itoa(inches) + f.InchesSymbol
	}
	feetStr := ""
	if !(f.suppressZeroFeet() && feet == 0) {
		feetStr = strconv.Itoa(feet) + f.FeetSymbol + f.FeetInchesSeparator
	}
	switch f.FractionType {
	case FractionFormatDiagonal:
		return fmt.Sprintf("\\A1;%s%d{\\H%sx;\\S%d#%d;}%s", feetStr, inches, fmtScale(f.FractionHeightScale), num, den, f.InchesSymbol)
	case FractionFormatNone:
		return fmt.Sprintf("%s%d %d/%d%s", feetStr, inches, num, den, f.InchesSymbol)
	}
	return fmt.Sprintf("\\A1;%s%d{\\H%sx;\\S%d/%d;}%s", feetStr, inches, fmtScale(f.FractionHeightScale), num, den, f.InchesSymbol)
}

// ToEngineering formats inches as feet-decimal inches.
func (f UnitStyleFormat) ToEngineering(v float64) string {
	feet := int(v / 12)
	inches := v - 12*float64(feet)
	if math.Abs(inches) < 1e-12 {
		return f.feetInchesZero(feet)
	}
	in := f.ToDecimal(inches, false)
	if feet == 0 {
		if f.suppressZeroFeet() {
			return in + f.InchesSymbol
		}
		return "0" + f.FeetSymbol + f.FeetInchesSeparator + in + f.InchesSymbol
	}
	return strconv.Itoa(feet) + f.FeetSymbol + f.FeetInchesSeparator + in + f.InchesSymbol
}

// ToFractional formats a value with a fraction.
func (f UnitStyleFormat) ToFractional(v float64) string {
	n := int(v)
	num, den := fraction(v, 1<<uint(max(f.LinearDecimalPlaces, 0)))
	if num == 0 {
		return strconv.Itoa(n)
	}
	switch f.FractionType {
	case FractionFormatDiagonal:
		return fmt.Sprintf("\\A1;%d{\\H%sx;\\S%d#%d;}", n, fmtScale(f.FractionHeightScale), num, den)
	case FractionFormatNone:
		prefix := ""
		if n != 0 {
			prefix = strconv.Itoa(n) + " "
		}
		return fmt.Sprintf("%s%d/%d", prefix, num, den)
	}
	return fmt.Sprintf("\\A1;%d{\\H%sx;\\S%d/%d;}", n, fmtScale(f.FractionHeightScale), num, den)
}

// ToDegrees formats an angle in radians as decimal degrees.
func (f UnitStyleFormat) ToDegrees(angle float64) string {
	return f.ToDecimal(angle*180/math.Pi, true) + f.DegreesSymbol
}

// ToDegreesMinutesSeconds formats an angle in radians as d°m's".
func (f UnitStyleFormat) ToDegreesMinutesSeconds(angle float64) string {
	deg := angle * 180 / math.Pi
	min := (deg - math.Trunc(deg)) * 60
	sec := (min - math.Trunc(min)) * 60
	switch f.AngularDecimalPlaces {
	case 0:
		return fmt.Sprintf("%d%s", int(math.Round(deg)), f.DegreesSymbol)
	case 1, 2:
		return fmt.Sprintf("%d%s%d%s", int(deg), f.DegreesSymbol, int(math.Round(min)), f.MinutesSymbol)
	case 3, 4:
		return fmt.Sprintf("%d%s%d%s%d%s", int(deg), f.DegreesSymbol, int(min), f.MinutesSymbol, int(math.Round(sec)), f.SecondsSymbol)
	}
	s := formatNumber(sec, int(f.AngularDecimalPlaces)-4, false, false, f.DecimalSeparator)
	return fmt.Sprintf("%d%s%d%s%s%s", int(deg), f.DegreesSymbol, int(min), f.MinutesSymbol, s, f.SecondsSymbol)
}

// ToGradians formats an angle in radians as gradians.
func (f UnitStyleFormat) ToGradians(angle float64) string {
	return f.ToDecimal(angle*200/math.Pi, true) + f.GradiansSymbol
}

// ToRadians formats an angle in radians.
func (f UnitStyleFormat) ToRadians(angle float64) string {
	return f.ToDecimal(angle, true) + f.RadiansSymbol
}
