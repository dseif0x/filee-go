package cadoc

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMeasurements(t *testing.T) {
	al := NewDimensionAligned()
	al.FirstPoint, al.SecondPoint = Vec3{0, 0, 0}, Vec3{3, 4, 0}
	lin := NewDimensionLinear()
	lin.FirstPoint, lin.SecondPoint = Vec3{0, 0, 0}, Vec3{3, 4, 0}
	linV := NewDimensionLinear()
	linV.FirstPoint, linV.SecondPoint, linV.Rotation = Vec3{0, 0, 0}, Vec3{3, 4, 0}, math.Pi/2
	rad := NewDimensionRadius()
	rad.DefinitionPoint, rad.AngleVertex = Vec3{1, 1, 0}, Vec3{3, 1, 0}
	dia := NewDimensionDiameter()
	dia.DefinitionPoint, dia.AngleVertex = Vec3{-2, 0, 0}, Vec3{2, 0, 0}
	a3 := NewDimensionAngular3Pt()
	a3.AngleVertex, a3.FirstPoint, a3.SecondPoint = Vec3{}, Vec3{1, 0, 0}, Vec3{0, 2, 0}
	a3p := NewDimensionAngular3Pt()
	a3p.AngleVertex, a3p.FirstPoint, a3p.SecondPoint = Vec3{}, Vec3{1, 0, 0}, Vec3{-1, 0, 0}
	a2 := NewDimensionAngular2Line()
	a2.FirstPoint, a2.SecondPoint = Vec3{0, 0, 0}, Vec3{1, 0, 0}
	a2.AngleVertex, a2.DefinitionPoint = Vec3{0, 0, 0}, Vec3{1, 1, 0}
	a2.DimensionArc = Vec3{1, 0.4, 0}
	ord := NewDimensionOrdinate()
	ord.FeatureLocation, ord.DefinitionPoint = Vec3{5, 7, 0}, Vec3{0, 0, 0}
	ordX := NewDimensionOrdinate()
	ordX.Flags |= DimensionTypeOrdinateTypeX
	ordX.FeatureLocation, ordX.DefinitionPoint = Vec3{5, 7, 0}, Vec3{0, 0, 0}
	arc := NewDimensionArc()
	arc.Center, arc.FirstPoint, arc.SecondPoint = Vec3{}, Vec3{2, 0, 0}, Vec3{0, 2, 0}
	for _, c := range []struct {
		d    Dimension
		want float64
	}{{al, 5}, {lin, 3}, {linV, 4}, {rad, 2}, {dia, 4}, {a3, math.Pi / 2}, {a3p, math.Pi},
		{a2, math.Pi / 4}, {ord, 7}, {ordX, 5}, {arc, math.Pi}} {
		got, err := c.d.Measurement()
		if err != nil || !near(got, c.want) {
			t.Errorf("%T: %v %v, want %v", c.d, got, err, c.want)
		}
	}
}

func TestDegenerateMeasurements(t *testing.T) {
	a3 := NewDimensionAngular3Pt()
	a3.FirstPoint = Vec3{1, 0, 0}
	a2 := NewDimensionAngular2Line()
	lin := NewDimensionLinear()
	arc := NewDimensionArc()
	arc.SecondPoint = Vec3{1, 1, 0}
	nan := NewDimensionAligned()
	nan.SecondPoint = Vec3{math.NaN(), 0, 0}
	for _, d := range []Dimension{a3, a2, lin, arc, nan} {
		if _, err := d.Measurement(); !errors.Is(err, ErrDegenerateDimension) {
			t.Errorf("%T: err %v", d, err)
		}
		doc := NewDocument()
		if err := UpdateDimensionBlock(doc, d); err == nil {
			if _, ok := d.(*DimensionAngular2Line); !ok {
				t.Errorf("%T: UpdateDimensionBlock should fail", d)
			}
		}
	}
}

func TestUnitFormats(t *testing.T) {
	f := NewUnitStyleFormat()
	f.LinearDecimalPlaces = 3
	f.LinearZeroHandling = ZeroHandlingShowZeroFeetAndInches
	check := func(got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	check(f.ToDecimal(2.5, false), "2.500")
	f.LinearZeroHandling = ZeroHandlingSuppressDecimalTrailingZeroes
	check(f.ToDecimal(2.5, false), "2.5")
	check(f.ToDecimal(3, false), "3")
	f.LinearZeroHandling = ZeroHandlingSuppressDecimalLeadingZeroes
	check(f.ToDecimal(0.25, false), ".250")
	f.LinearZeroHandling = ZeroHandlingSuppressDecimalLeadingAndTrailingZeroes
	check(f.ToDecimal(0.25, false), ".25")
	f.DecimalSeparator = ","
	check(f.ToDecimal(1.125, false), "1,125")
	f.DecimalSeparator = "."
	f.LinearZeroHandling = ZeroHandlingShowZeroFeetAndInches
	f.LinearDecimalPlaces = 2
	check(f.ToScientific(1234.5), "1.23E+03")
	f.LinearDecimalPlaces = 4 // 1/16
	check(f.ToFractional(2.5), "\\A1;2{\\H1x;\\S1/2;}")
	f.FractionType = FractionFormatNone
	check(f.ToFractional(2.5), "2 1/2")
	check(f.ToFractional(3), "3")
	check(f.ToArchitectural(30.5), "2'-6 1/2\"")
	check(f.ToArchitectural(24), "2'-0\"")
	f.LinearZeroHandling = ZeroHandlingShowZeroFeetSuppressZeroInches
	check(f.ToArchitectural(24), "2'")
	f.LinearZeroHandling = ZeroHandlingShowZeroFeetAndInches
	f.LinearDecimalPlaces = 2
	check(f.ToEngineering(30.5), "2'-6.50\"")
	f.AngularDecimalPlaces = 0
	check(f.ToDegrees(math.Pi/4), "45°")
	f.AngularDecimalPlaces = 4
	check(f.ToDegreesMinutesSeconds((30.5+0.25/60)*math.Pi/180), "30°30'15\"")
	f.AngularDecimalPlaces = 2
	check(f.ToDegreesMinutesSeconds(30.5*math.Pi/180), "30°30'")
	check(f.ToGradians(math.Pi/2), "100.00g")
	f.AngularZeroHandling = AngularZeroHandlingSupressTrailingZeroes
	check(f.ToRadians(1.5), "1.5r")
}

func TestMeasurementText(t *testing.T) {
	doc := NewDocument()
	st, _ := doc.DimensionStyles.Get(DefaultDimStyleName)
	st.DecimalPlaces = 2
	st.ZeroHandling = ZeroHandlingShowZeroFeetAndInches
	al := NewDimensionAligned()
	al.Style = st
	al.SecondPoint = Vec3{3, 4, 0}
	if s, _ := MeasurementText(doc, al); s != "5.00" {
		t.Errorf("aligned %q", s)
	}
	st.PostFix = "L=<> mm"
	if s, _ := MeasurementText(doc, al); s != "L=5.00 mm" {
		t.Errorf("postfix %q", s)
	}
	al.Text = "approx <>"
	if s, _ := MeasurementText(doc, al); s != "approx L=5.00 mm" {
		t.Errorf("text override %q", s)
	}
	st.PostFix, al.Text = "", ""
	rad := NewDimensionRadius()
	rad.Style = st
	rad.AngleVertex = Vec3{2, 0, 0}
	if s, _ := MeasurementText(doc, rad); s != "R2.00" {
		t.Errorf("radius %q", s)
	}
	a3 := NewDimensionAngular3Pt()
	a3.Style = st
	a3.FirstPoint, a3.SecondPoint = Vec3{1, 0, 0}, Vec3{0, 1, 0}
	if s, _ := MeasurementText(doc, a3); s != "90°" {
		t.Errorf("angular %q", s)
	}
	// DSTYLE override of DIMDEC (271) to 1.
	al.SetExtendedData(DefaultAppIdName, []ExtendedDataRecord{{Code: 1000, Value: "DSTYLE"},
		{Code: 1002, Value: "{"}, {Code: 1070, Value: int16(271)}, {Code: 1070, Value: int16(1)}, {Code: 1002, Value: "}"}})
	if s, _ := MeasurementText(doc, al); s != "5.0" {
		t.Errorf("override %q", s)
	}
	if st.DecimalPlaces != 2 {
		t.Error("override changed the style")
	}
}

func countKinds(r *BlockRecord) map[string]int {
	m := map[string]int{}
	for _, e := range r.Entities {
		m[e.ObjectName()]++
	}
	return m
}

func TestUpdateDimensionBlock(t *testing.T) {
	doc := NewDocument()
	al := NewDimensionAligned()
	al.FirstPoint, al.SecondPoint, al.DefinitionPoint = Vec3{0, 0, 0}, Vec3{10, 0, 0}, Vec3{10, 5, 0}
	lin := NewDimensionLinear()
	lin.FirstPoint, lin.SecondPoint, lin.DefinitionPoint, lin.Rotation = Vec3{0, 0, 0}, Vec3{10, 3, 0}, Vec3{10, 8, 0}, 0.3
	rad := NewDimensionRadius()
	rad.DefinitionPoint, rad.AngleVertex, rad.TextMiddlePoint = Vec3{0, 0, 0}, Vec3{5, 0, 0}, Vec3{2, 0, 0}
	dia := NewDimensionDiameter()
	dia.DefinitionPoint, dia.AngleVertex, dia.TextMiddlePoint = Vec3{-5, 0, 0}, Vec3{5, 0, 0}, Vec3{0, 0, 0}
	ord := NewDimensionOrdinate()
	ord.FeatureLocation, ord.LeaderEndpoint = Vec3{3, 4, 0}, Vec3{8, 9, 0}
	cases := []struct {
		d    Dimension
		want map[string]int
	}{
		{al, map[string]int{"POINT": 4, "LINE": 3, "SOLID": 2, "MTEXT": 1}},
		{lin, map[string]int{"POINT": 4, "LINE": 3, "SOLID": 2, "MTEXT": 1}},
		{rad, map[string]int{"POINT": 1, "LINE": 3, "MTEXT": 1}},
		{dia, map[string]int{"POINT": 1, "LINE": 3, "MTEXT": 1}},
		{ord, map[string]int{"POINT": 2, "LINE": 3, "MTEXT": 1}},
	}
	for _, c := range cases {
		doc.ModelSpace().AddEntity(c.d)
		for i := 0; i < 2; i++ {
			if err := UpdateDimensionBlock(doc, c.d); err != nil {
				t.Fatalf("%T: %v", c.d, err)
			}
		}
		b := c.d.Dimension().Block
		if b == nil || !b.IsAnonymous() || !strings.HasPrefix(b.Name, "*D") {
			t.Fatalf("%T: block %v", c.d, b)
		}
		got := countKinds(b)
		for k, n := range c.want {
			if got[k] != n {
				t.Errorf("%T: %s = %d, want %d (%v)", c.d, k, got[k], n, got)
			}
		}
		if r, ok := doc.BlockRecords.Get(b.Name); !ok || r != b {
			t.Errorf("%T: block not in the table", c.d)
		}
		for _, e := range b.Entities {
			if e.Entity().Layer == nil {
				t.Errorf("%T: entity without layer", c.d)
			}
		}
	}
	n := 0
	for _, r := range doc.BlockRecords.Entries() {
		if strings.HasPrefix(r.Name, "*D") {
			n++
		}
	}
	if n != len(cases) {
		t.Errorf("%d dimension blocks, want %d", n, len(cases))
	}
	if l, ok := doc.Layers.Get(DefpointsLayerName); !ok || l.Plots() {
		t.Error("Defpoints layer missing or plotting")
	}
	// Angular two-line dimensions are left alone; three-point get an empty block.
	a2 := NewDimensionAngular2Line()
	a2.FirstPoint, a2.SecondPoint, a2.DefinitionPoint = Vec3{}, Vec3{1, 0, 0}, Vec3{1, 1, 0}
	if err := UpdateDimensionBlock(doc, a2); err != nil || a2.Block != nil {
		t.Errorf("angular2line: %v %v", err, a2.Block)
	}
	a3 := NewDimensionAngular3Pt()
	a3.FirstPoint, a3.SecondPoint = Vec3{1, 0, 0}, Vec3{0, 1, 0}
	if err := UpdateDimensionBlock(doc, a3); err != nil || a3.Block == nil || len(a3.Block.Entities) != 0 {
		t.Errorf("angular3pt: %v", err)
	}
	// Arrow blocks give inserts.
	arrow := NewBlockRecord("_ARROW")
	_ = doc.BlockRecords.Add(arrow)
	st, _ := doc.DimensionStyles.Get(DefaultDimStyleName)
	st.DimArrow1, st.DimArrow2 = arrow, arrow
	al.Style = st
	_ = UpdateDimensionBlock(doc, al)
	if got := countKinds(al.Block); got["INSERT"] != 2 || got["SOLID"] != 0 {
		t.Errorf("arrow blocks: %v", got)
	}
}
