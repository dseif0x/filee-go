package geo

// Coordinate reference systems, just enough to put every file into longitude /
// latitude: geographic systems (degrees) and the projections most projected
// data uses (UTM and other Transverse Mercator, Mercator and Web Mercator,
// Lambert Conformal Conic, Albers) read from a .prj file (ESRI or OGC WKT) or
// an EPSG code. A system outside that list is refused with its name rather
// than guessed, because coordinates in the wrong system land on the wrong
// place on Earth. Datums are not shifted: WGS 84, NAD83, ETRS89 and the other
// systems tied to ITRF agree within a metre or two, and anything else is
// converted as if it were WGS 84 and the file gets a notice (Approximate).

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/dseif0x/filee-go/internal/convert"
)

const toDegreesFactor = 180 / math.Pi

// Crs is a coordinate system that can turn its positions into longitude / latitude in degrees.
type Crs interface {
	// Name for messages, e.g. "NAD83 / UTM zone 18N".
	Name() string
	// IsGeographic: degrees of longitude / latitude (no projection).
	IsGeographic() bool
	// IsIdentity: positions are already longitude / latitude in degrees from Greenwich.
	IsIdentity() bool
	// Approximate: the datum is not one that agrees with WGS 84 within a few
	// metres, so positions are converted as if it were.
	Approximate() bool
	// ToLonLat is the position in degrees of longitude (X) and latitude (Y); the height is kept.
	ToLonLat(Coord) Coord
}

var wgs84 Crs = &geographicCrs{name: "WGS 84", toDegrees: 1}

func unsupportedCrs(detail string) error { return convert.Refuse("error.geo_unsupported_crs", detail) }

// crsFromWkt reads a .prj / WKT definition; a refusal if it is unknown or unsupported.
func crsFromWkt(wkt string) (Crs, error) {
	if len(wkt) > 1<<16 {
		return nil, unsupportedCrs("(definition too long)")
	}
	root, err := parseWktNode(wkt)
	if err != nil {
		return nil, unsupportedCrs(err.Error())
	}
	nameOr := func(n *wktNode, fallback string) string {
		if t, ok := n.text(0); ok {
			return t
		}
		return fallback
	}
	switch root.name {
	case "GEOGCRS", "GEODCRS", "PROJCRS", "PROJECTEDCRS", "BOUNDCRS", "COMPOUNDCRS":
		// WKT2 (ISO 19162) uses other keywords for the same things; PROJ writes it into .prj files only rarely.
		return nil, unsupportedCrs(nameOr(root, root.name))
	case "GEOGCS":
		return crsFromGeographic(root), nil
	case "PROJCS":
		return crsFromProjected(root)
	case "COMPD_CS":
		if p := root.child("PROJCS"); p != nil {
			return crsFromProjected(p)
		}
		if g := root.child("GEOGCS"); g != nil {
			return crsFromGeographic(g), nil
		}
		return nil, unsupportedCrs(nameOr(root, "COMPD_CS"))
	case "GEOCCS", "LOCAL_CS", "VERT_CS":
		return nil, unsupportedCrs(nameOr(root, root.name))
	}
	return nil, unsupportedCrs(root.name)
}

// crsFromEpsg is the system of an EPSG code, or nil if it is not one Filee knows.
func crsFromEpsg(code int) Crs {
	switch code {
	case 4326:
		return wgs84
	case 4269, 4258, 4283, 7844, 4490, 6318, 4167, 4612, 6668, 4674, 4171, 9777:
		// NAD83, ETRS89, GDA94, GDA2020, CGCS2000, NAD83(NSRS2007), NZGD2000, JGD2000, JGD2011, SIRGAS 2000, RGF93, Korea 2000
		return &geographicCrs{name: fmt.Sprintf("EPSG:%d", code), toDegrees: 1}
	case 3857, 900913, 3785, 102100, 102113:
		return newMercator("WGS 84 / Pseudo-Mercator", sphere(6378137), 0, 1, 0, 0, 1)
	}
	switch {
	case code >= 32601 && code <= 32660:
		return utm(code, code-32600, false, ellipsoidWgs84, "WGS 84")
	case code >= 32701 && code <= 32760:
		return utm(code, code-32700, true, ellipsoidWgs84, "WGS 84")
	case code >= 26901 && code <= 26923:
		return utm(code, code-26900, false, ellipsoidGrs80, "NAD83")
	case code >= 25828 && code <= 25838:
		return utm(code, code-25800, false, ellipsoidGrs80, "ETRS89")
	}
	return nil
}

// crsFromName reads a GeoJSON 2008 crs name such as "EPSG:4326" or "urn:ogc:def:crs:OGC:1.3:CRS84", or nil.
func crsFromName(name string) Crs {
	text := strings.TrimSpace(name)
	upper := strings.ToUpper(text)
	if strings.Contains(upper, "CRS84") || strings.Contains(upper, "CRS:84") {
		return wgs84
	}
	digits := len(text) - 1
	for digits >= 0 && text[digits] >= '0' && text[digits] <= '9' {
		digits--
	}
	if digits < len(text)-1 && strings.Contains(upper, "EPSG") {
		if code, err := strconv.Atoi(text[digits+1:]); err == nil {
			return crsFromEpsg(code)
		}
	}
	return nil
}

func utm(code, zone int, south bool, e *ellipsoid, datum string) Crs {
	hemisphere := "N"
	northing := 0.0
	if south {
		hemisphere, northing = "S", 10000000
	}
	return newTransverseMercator(fmt.Sprintf("%s / UTM zone %d%s (EPSG:%d)", datum, zone, hemisphere, code), e,
		0, float64(zone*6-183)/toDegreesFactor, 0.9996, 500000, northing, 1)
}

func crsFromGeographic(n *wktNode) Crs {
	name, ok := n.text(0)
	if !ok {
		name = "geographic"
	}
	datumNode := n.child("DATUM")
	datum, ok := datumNode.text(0)
	if !ok {
		datum = name
	}
	compatible := wgs84Compatible(datum, datumNode.child("SPHEROID"))
	unit, ok := n.child("UNIT").number(1)
	if !ok {
		unit = math.Pi / 180
	}
	meridian, _ := n.child("PRIMEM").number(1)
	// Angles in radians (UNIT factor) → degrees. PRIMEM is in degrees from
	// Greenwich whatever the unit of the system: GDAL and ESRI both write the
	// Paris meridian as 2.33722917 next to UNIT["Grad"], not as 2.5969 grads.
	toDeg := unit * toDegreesFactor
	if math.Abs(toDeg-1) < 1e-9 {
		toDeg = 1
	}
	return &geographicCrs{name: name, toDegrees: toDeg, meridian: meridian, approximate: !compatible}
}

func crsFromProjected(n *wktNode) (Crs, error) {
	name, ok := n.text(0)
	if !ok {
		name = "projected"
	}
	geographic := n.child("GEOGCS")
	if geographic == nil {
		return nil, unsupportedCrs(name)
	}
	datum := geographic.child("DATUM")
	spheroid := datum.child("SPHEROID")
	axis, ok := spheroid.number(1)
	if spheroid == nil || !ok || axis <= 0 {
		return nil, unsupportedCrs(name + " (no ellipsoid)")
	}
	// The projection's own longitudes are counted from the prime meridian and
	// its angles in the unit of the geographic system. Neither is applied here,
	// and ignoring them puts the data far from where it is (NTF Paris Lambert II
	// is 2.3 degrees west of its true place), so a system with another prime
	// meridian or angular unit is refused.
	if pm := geographic.child("PRIMEM"); pm != nil {
		if v, _ := pm.number(1); math.Abs(v) > 1e-9 {
			pmName, _ := pm.text(0)
			return nil, unsupportedCrs(fmt.Sprintf("%s (prime meridian %s %s°)", name, pmName, strconv.FormatFloat(v, 'g', -1, 64)))
		}
	}
	if u := geographic.child("UNIT"); u != nil {
		if f, ok := u.number(1); ok && math.Abs(f*toDegreesFactor-1) > 1e-9 {
			uName, _ := u.text(0)
			return nil, unsupportedCrs(fmt.Sprintf("%s (angular unit %s)", name, uName))
		}
	}
	inverse, _ := spheroid.number(2)
	e := fromInverseFlattening(axis, inverse)
	datumName, ok := datum.text(0)
	if !ok {
		datumName, _ = geographic.text(0)
	}
	approximate := !wgs84Compatible(datumName, spheroid)
	unit := 1.0
	if units := n.children("UNIT"); len(units) > 0 {
		if v, ok := units[len(units)-1].number(1); ok {
			unit = v
		}
	}
	if unit <= 0 {
		return nil, unsupportedCrs(name + " (unit)")
	}
	projection, ok := n.child("PROJECTION").text(0)
	if !ok {
		return nil, unsupportedCrs(name)
	}
	parameters := map[string]float64{}
	for _, p := range n.children("PARAMETER") {
		t, ok1 := p.text(0)
		v, ok2 := p.number(1)
		if !ok1 || !ok2 {
			continue
		}
		if _, dup := parameters[asciiKey(t)]; !dup {
			parameters[asciiKey(t)] = v
		}
	}
	get := func(k string, fallback float64) float64 {
		if v, ok := parameters[k]; ok {
			return v
		}
		return fallback
	}
	angle := func(k string) float64 { return get(k, 0) / toDegreesFactor }
	falseEasting := get("falseeasting", 0) * unit
	falseNorthing := get("falsenorthing", 0) * unit
	centre := angle("centralmeridian")
	origin := angle("latitudeoforigin")

	var result projectedCrs
	switch asciiKey(projection) {
	case "transversemercator", "gausskruger", "gausskrugerzone", "tmerc":
		result = newTransverseMercator(name, e, origin, centre, get("scalefactor", 1), falseEasting, falseNorthing, unit)
	case "mercatorauxiliarysphere", "popularvisualisationpseudomercator", "popularvisualizationpseudomercator":
		result = newMercator(name, sphere(axis), centre, 1, falseEasting, falseNorthing, unit)
	case "mercator", "mercator1sp", "mercator2sp":
		lower := strings.ToLower(name)
		body := e
		if strings.Contains(lower, "pseudo") || strings.Contains(lower, "web_mercator") || strings.Contains(lower, "web mercator") {
			body = sphere(axis)
		}
		k0 := get("scalefactor", 1)
		if _, ok := parameters["standardparallel1"]; ok {
			k0 = mercatorScale(body, angle("standardparallel1"))
		}
		result = newMercator(name, body, centre, k0, falseEasting, falseNorthing, unit)
	case "lambertconformalconic", "lambertconformalconic2sp", "lambertconformalconic1sp", "lambertconformalconicbelgium":
		first := origin
		if _, ok := parameters["standardparallel1"]; ok {
			first = angle("standardparallel1")
		}
		second := first
		if _, ok := parameters["standardparallel2"]; ok {
			second = angle("standardparallel2")
		}
		result = newLambert(name, e, origin, centre, first, second, get("scalefactor", 1), falseEasting, falseNorthing, unit)
	case "albersconicequalarea", "albers", "albersequalareaconic":
		result = newAlbers(name, e, origin, centre, angle("standardparallel1"), angle("standardparallel2"), falseEasting, falseNorthing, unit)
	default:
		return nil, unsupportedCrs(fmt.Sprintf("%s (%s)", name, projection))
	}
	result.setApproximate(approximate)
	return result, nil
}

func mercatorScale(e *ellipsoid, parallel float64) float64 {
	s := math.Sin(parallel)
	return math.Cos(parallel) / math.Sqrt(1-e.e2*s*s)
}

// Datums that agree with WGS 84 within about two metres (ITRF-based systems), by name.
var compatibleDatums = []string{
	"WGS1984", "WGS84", "NORTHAMERICAN1983", "NAD83", "ETRS89", "ETRS1989", "EUROPEANTERRESTRIALREFERENCESYSTEM1989",
	"GRS1980", "KOREA2000", "CHINA2000", "CGCS2000", "CHINAGEODETICCOORDINATESYSTEM2000", "JGD2011", "JGD2000",
	"JAPANGEODETICDATUM", "GDA94", "GDA2020", "GEOCENTRICDATUMOFAUSTRALIA", "NZGD2000", "NEWZEALANDGEODETICDATUM2000",
	"SIRGAS2000", "SIRGAS", "RGF93", "RESEAUGEODESIQUEFRANCAIS1993", "ITRF", "HARN", "NSRS2007",
}

// wgs84Compatible: a datum named in compatibleDatums, or on the WGS 84 / GRS
// 80 ellipsoid (modern national datums are realisations of ITRF on GRS 80).
func wgs84Compatible(datum string, spheroid *wktNode) bool {
	if axis, ok := spheroid.number(1); ok {
		if inverse, ok := spheroid.number(2); ok && math.Abs(axis-6378137) < 0.01 && math.Abs(inverse-298.2572) < 0.001 {
			return true
		}
	}
	k := strings.ToUpper(asciiKey(strings.TrimPrefix(datum, "D_")))
	if k == "" {
		return false
	}
	for _, c := range compatibleDatums {
		if strings.Contains(k, c) {
			return true
		}
	}
	return false
}

// ---- systems ----

type geographicCrs struct {
	name                string
	toDegrees, meridian float64
	approximate         bool
}

func (g *geographicCrs) Name() string       { return g.name }
func (g *geographicCrs) IsGeographic() bool { return true }
func (g *geographicCrs) IsIdentity() bool   { return g.toDegrees == 1 && g.meridian == 0 }
func (g *geographicCrs) Approximate() bool  { return g.approximate }
func (g *geographicCrs) ToLonLat(p Coord) Coord {
	if g.IsIdentity() {
		return p
	}
	return Coord{p.X*g.toDegrees + g.meridian, p.Y * g.toDegrees, p.Z}
}

type projectedCrs interface {
	Crs
	setApproximate(bool)
}

// projected is a projection given by the inverse formula that maps metres to radians (longitude, latitude).
type projected struct {
	name        string
	unit        float64
	approximate bool
	inverse     func(x, y float64) (lon, lat float64)
}

func (p *projected) Name() string          { return p.name }
func (p *projected) IsGeographic() bool    { return false }
func (p *projected) IsIdentity() bool      { return false }
func (p *projected) Approximate() bool     { return p.approximate }
func (p *projected) setApproximate(a bool) { p.approximate = a }
func (p *projected) ToLonLat(c Coord) Coord {
	lon, lat := p.inverse(c.X*p.unit, c.Y*p.unit)
	return Coord{lon * toDegreesFactor, lat * toDegreesFactor, c.Z}
}

// newTransverseMercator uses Krüger's series in the third flattening n
// (Karney 2011), accurate to nanometres far from the central meridian.
func newTransverseMercator(name string, e *ellipsoid, latitudeOrigin, centralMeridian, scale, falseEasting, falseNorthing, unit float64) *projected {
	n := e.flattening / (2 - e.flattening)
	n2 := n * n
	n3, n4, n5, n6 := n2*n, n2*n2, n2*n2*n, n2*n2*n2
	radius := e.semiMajor / (1 + n) * (1 + n2/4 + n4/64 + n6/256)
	var beta, delta [7]float64
	beta[1] = n/2 - 2*n2/3 + 37*n3/96 - n4/360 - 81*n5/512 + 96199*n6/604800
	beta[2] = n2/48 + n3/15 - 437*n4/1440 + 46*n5/105 - 1118711*n6/3870720
	beta[3] = 17*n3/480 - 37*n4/840 - 209*n5/4480 + 5569*n6/90720
	beta[4] = 4397*n4/161280 - 11*n5/504 - 830251*n6/7257600
	beta[5] = 4583*n5/161280 - 108847*n6/3991680
	beta[6] = 20648693 * n6 / 638668800
	delta[1] = 2*n - 2*n2/3 - 2*n3 + 116*n4/45 + 26*n5/45 - 2854*n6/675
	delta[2] = 7*n2/3 - 8*n3/5 - 227*n4/45 + 2704*n5/315 + 2323*n6/945
	delta[3] = 56*n3/15 - 136*n4/35 - 1262*n5/105 + 73814*n6/2835
	delta[4] = 4279*n4/630 - 332*n5/35 - 399572*n6/14175
	delta[5] = 4174*n5/315 - 144838*n6/6237
	delta[6] = 601676 * n6 / 22275
	originArc := 0.0
	if latitudeOrigin != 0 {
		originArc = e.meridianArc(latitudeOrigin)
	}
	return &projected{name: name, unit: unit, inverse: func(x, y float64) (float64, float64) {
		xi := (y - falseNorthing + scale*originArc) / (scale * radius)
		eta := (x - falseEasting) / (scale * radius)
		xi2, eta2 := xi, eta
		for j := 1; j <= 6; j++ {
			jj := float64(2 * j)
			xi2 -= beta[j] * math.Sin(jj*xi) * math.Cosh(jj*eta)
			eta2 -= beta[j] * math.Cos(jj*xi) * math.Sinh(jj*eta)
		}
		chi := math.Asin(math.Sin(xi2) / math.Cosh(eta2))
		lat := chi
		for j := 1; j <= 6; j++ {
			lat += delta[j] * math.Sin(float64(2*j)*chi)
		}
		return centralMeridian + math.Atan2(math.Sinh(eta2), math.Cos(xi2)), lat
	}}
}

func newMercator(name string, e *ellipsoid, centralMeridian, scale, falseEasting, falseNorthing, unit float64) *projected {
	return &projected{name: name, unit: unit, inverse: func(x, y float64) (float64, float64) {
		a := e.semiMajor * scale
		t := math.Exp(-(y - falseNorthing) / a)
		lat := math.Pi/2 - 2*math.Atan(t)
		if e.e > 0 {
			lat = iterateLatitude(lat, t, e.e)
		}
		return centralMeridian + (x-falseEasting)/a, lat
	}}
}

func iterateLatitude(lat, t, e float64) float64 {
	for i := 0; i < 12; i++ {
		s := e * math.Sin(lat)
		next := math.Pi/2 - 2*math.Atan(t*math.Pow((1-s)/(1+s), e/2))
		done := math.Abs(next-lat) < 1e-14
		lat = next
		if done {
			break
		}
	}
	return lat
}

func sign(v float64) float64 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func newLambert(name string, e *ellipsoid, latitudeOrigin, centralMeridian, first, second, scale, falseEasting, falseNorthing, unit float64) *projected {
	t := func(lat float64) float64 {
		s := e.e * math.Sin(lat)
		return math.Tan(math.Pi/4-lat/2) / math.Pow((1-s)/(1+s), e.e/2)
	}
	m := func(lat float64) float64 { return math.Cos(lat) / math.Sqrt(1-e.e2*math.Pow(math.Sin(lat), 2)) }
	m1, t1 := m(first), t(first)
	var n float64
	if math.Abs(first-second) < 1e-12 {
		n = math.Sin(first)
	} else {
		n = (math.Log(m1) - math.Log(m(second))) / (math.Log(t1) - math.Log(t(second)))
	}
	f := e.semiMajor * scale * m1 / (n * math.Pow(t1, n))
	rho0 := f * math.Pow(t(latitudeOrigin), n)
	return &projected{name: name, unit: unit, inverse: func(x, y float64) (float64, float64) {
		dx := x - falseEasting
		dy := rho0 - (y - falseNorthing)
		sg := sign(n)
		rho := sg * math.Sqrt(dx*dx+dy*dy)
		theta := math.Atan2(sg*dx, sg*dy)
		tt := math.Pow(rho/f, 1/n)
		lat := math.Pi/2 - 2*math.Atan(tt)
		if e.e > 0 {
			lat = iterateLatitude(lat, tt, e.e)
		}
		return centralMeridian + theta/n, lat
	}}
}

func newAlbers(name string, e *ellipsoid, latitudeOrigin, centralMeridian, first, second, falseEasting, falseNorthing, unit float64) *projected {
	m := func(lat float64) float64 { return math.Cos(lat) / math.Sqrt(1-e.e2*math.Pow(math.Sin(lat), 2)) }
	q := func(lat float64) float64 {
		s := math.Sin(lat)
		if e.e < 1e-12 {
			return 2 * s
		}
		return (1 - e.e2) * (s/(1-e.e2*s*s) - math.Log((1-e.e*s)/(1+e.e*s))/(2*e.e))
	}
	m1, q1 := m(first), q(first)
	var n float64
	if math.Abs(first-second) < 1e-12 {
		n = math.Sin(first)
	} else {
		n = (m1*m1 - m(second)*m(second)) / (q(second) - q1)
	}
	c := m1*m1 + n*q1
	rho0 := e.semiMajor * math.Sqrt(math.Max(0, c-n*q(latitudeOrigin))) / n
	return &projected{name: name, unit: unit, inverse: func(x, y float64) (float64, float64) {
		dx := x - falseEasting
		dy := rho0 - (y - falseNorthing)
		sg := sign(n)
		rho := math.Sqrt(dx*dx + dy*dy)
		theta := math.Atan2(sg*dx, sg*dy)
		a := e.semiMajor
		qq := (c - rho*rho*n*n/(a*a)) / n
		lat := math.Asin(math.Max(-1, math.Min(1, qq/2)))
		if e.e > 1e-12 {
			polar := q(math.Pi / 2)
			if math.Abs(math.Abs(qq)-polar) < 1e-10 {
				return centralMeridian + theta/n, sign(qq) * math.Pi / 2
			}
			for i := 0; i < 20; i++ {
				s := math.Sin(lat)
				w := 1 - e.e2*s*s
				step := w * w / (2 * math.Cos(lat)) * (qq/(1-e.e2) - s/w + math.Log((1-e.e*s)/(1+e.e*s))/(2*e.e))
				lat += step
				if math.Abs(step) < 1e-14 {
					break
				}
			}
		}
		return centralMeridian + theta/n, lat
	}}
}

// ---- ellipsoid ----

type ellipsoid struct{ semiMajor, flattening, e2, e float64 }

var (
	ellipsoidWgs84 = fromInverseFlattening(6378137, 298.257223563)
	ellipsoidGrs80 = fromInverseFlattening(6378137, 298.257222101)
)

func newEllipsoid(semiMajor, flattening float64) *ellipsoid {
	e2 := flattening * (2 - flattening)
	return &ellipsoid{semiMajor, flattening, e2, math.Sqrt(e2)}
}

func fromInverseFlattening(semiMajor, inverse float64) *ellipsoid {
	f := 0.0
	if inverse > 1 {
		f = 1 / inverse
	}
	return newEllipsoid(semiMajor, f)
}

func sphere(radius float64) *ellipsoid { return newEllipsoid(radius, 0) }

// meridianArc is the distance along the meridian from the equator to lat (radians), in metres.
func (e *ellipsoid) meridianArc(lat float64) float64 {
	e2 := e.e2
	e4, e6 := e2*e2, e2*e2*e2
	return e.semiMajor * ((1-e2/4-3*e4/64-5*e6/256)*lat -
		(3*e2/8+3*e4/32+45*e6/1024)*math.Sin(2*lat) +
		(15*e4/256+45*e6/1024)*math.Sin(4*lat) -
		35*e6/3072*math.Sin(6*lat))
}

// ---- WKT definitions ----

// wktNode is a node of a WKT definition: NAME[arg, arg, NAME[...]]; items are strings, float64s or nodes.
type wktNode struct {
	name  string
	items []any
}

const wktMaxDepth = 24

func (n *wktNode) text(i int) (string, bool) {
	if n == nil || i >= len(n.items) {
		return "", false
	}
	s, ok := n.items[i].(string)
	return s, ok
}

func (n *wktNode) number(i int) (float64, bool) {
	if n == nil || i >= len(n.items) {
		return 0, false
	}
	v, ok := n.items[i].(float64)
	return v, ok
}

func (n *wktNode) children(name string) []*wktNode {
	if n == nil {
		return nil
	}
	var out []*wktNode
	for _, it := range n.items {
		if c, ok := it.(*wktNode); ok && strings.EqualFold(c.name, name) {
			out = append(out, c)
		}
	}
	return out
}

func (n *wktNode) child(name string) *wktNode {
	if c := n.children(name); len(c) > 0 {
		return c[0]
	}
	return nil
}

func parseWktNode(text string) (*wktNode, error) {
	p := &wktParser{text: []rune(text)}
	n, err := p.node(0)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, errors.New("empty definition")
	}
	return n, nil
}

type wktParser struct {
	text []rune
	pos  int
}

func (p *wktParser) skip() {
	for p.pos < len(p.text) && unicode.IsSpace(p.text[p.pos]) {
		p.pos++
	}
}

func isASCIIAlnum(r rune) bool { return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

func (p *wktParser) node(depth int) (*wktNode, error) {
	if depth > wktMaxDepth {
		return nil, errors.New("definition nested too deeply")
	}
	p.skip()
	start := p.pos
	for p.pos < len(p.text) && (isASCIIAlnum(p.text[p.pos]) || p.text[p.pos] == '_') {
		p.pos++
	}
	if p.pos == start {
		return nil, nil
	}
	n := &wktNode{name: strings.ToUpper(string(p.text[start:p.pos]))}
	p.skip()
	if p.pos >= len(p.text) || (p.text[p.pos] != '[' && p.text[p.pos] != '(') {
		return n, nil
	}
	closing := ']'
	if p.text[p.pos] == '(' {
		closing = ')'
	}
	p.pos++
	for {
		p.skip()
		if p.pos >= len(p.text) {
			return nil, errors.New("missing closing bracket")
		}
		ch := p.text[p.pos]
		switch {
		case ch == closing:
			p.pos++
			return n, nil
		case ch == ',':
			p.pos++
		case ch == '"':
			var b strings.Builder
			p.pos++
			for {
				if p.pos >= len(p.text) {
					return nil, errors.New("missing closing quote")
				}
				if p.text[p.pos] == '"' {
					if p.pos+1 < len(p.text) && p.text[p.pos+1] == '"' {
						b.WriteRune('"')
						p.pos += 2
						continue
					}
					p.pos++
					break
				}
				b.WriteRune(p.text[p.pos])
				p.pos++
			}
			n.items = append(n.items, b.String())
		case ch >= '0' && ch <= '9' || ch == '-' || ch == '+' || ch == '.':
			from := p.pos
			for p.pos < len(p.text) && (p.text[p.pos] >= '0' && p.text[p.pos] <= '9' || strings.ContainsRune("-+.eE", p.text[p.pos])) {
				p.pos++
			}
			s := string(p.text[from:p.pos])
			v, err := strconv.ParseFloat(s, 64)
			if err != nil && !isRangeError(err) {
				return nil, fmt.Errorf("bad number '%s'", s)
			}
			n.items = append(n.items, v)
		default:
			c, err := p.node(depth + 1)
			if err != nil {
				return nil, err
			}
			if c == nil {
				return nil, fmt.Errorf("unexpected '%c'", ch)
			}
			// A word with no brackets, like the axis direction NORTH, is kept as text.
			if len(c.items) == 0 {
				n.items = append(n.items, c.name)
			} else {
				n.items = append(n.items, c)
			}
		}
	}
}

func isRangeError(err error) bool {
	var ne *strconv.NumError
	return errors.As(err, &ne) && ne.Err == strconv.ErrRange
}
