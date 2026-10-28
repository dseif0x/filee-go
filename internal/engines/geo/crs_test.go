package geo

// Coordinate systems against PROJ: the .prj definitions GDAL writes (ESRI WKT)
// and EPSG codes must put a projected position within a millimetre of where
// PROJ says it is (reference positions from pyproj 3.x); systems Filee can't
// convert are refused with their name; the datum rules; and the WKT parsers' errors.

import (
	"errors"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/convert"
)

type crsCase struct {
	code     int
	wkt      string
	x, y     float64
	lon, lat float64
}

var crsCases = []crsCase{
	{32633, `PROJCS["WGS_1984_UTM_Zone_33N",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",15.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`, 602065.2074110014, 5340353.594303575, 16.3738, 48.2082},
	{32733, `PROJCS["WGS_1984_UTM_Zone_33S",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",10000000.0],PARAMETER["Central_Meridian",15.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`, 816557.7955966464, 6240887.995569803, 18.4241, -33.9249},
	{3857, `PROJCS["WGS_1984_Web_Mercator_Auxiliary_Sphere",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Mercator_Auxiliary_Sphere"],PARAMETER["False_Easting",0.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",0.0],PARAMETER["Standard_Parallel_1",0.0],PARAMETER["Auxiliary_Sphere_Type",0.0],UNIT["Meter",1.0]]`, 15550408.912046732, 4257980.732184108, 139.6917, 35.6895},
	{2154, `PROJCS["RGF_1993_Lambert_93",GEOGCS["GCS_RGF_1993",DATUM["D_RGF_1993",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Lambert_Conformal_Conic"],PARAMETER["False_Easting",700000.0],PARAMETER["False_Northing",6600000.0],PARAMETER["Central_Meridian",3.0],PARAMETER["Standard_Parallel_1",49.0],PARAMETER["Standard_Parallel_2",44.0],PARAMETER["Latitude_Of_Origin",46.5],UNIT["Meter",1.0]]`, 652469.0227091359, 6862035.259420077, 2.3522, 48.8566},
	{5070, `PROJCS["NAD_1983_Contiguous_USA_Albers",GEOGCS["GCS_North_American_1983",DATUM["D_North_American_1983",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Albers"],PARAMETER["False_Easting",0.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",-96.0],PARAMETER["Standard_Parallel_1",29.5],PARAMETER["Standard_Parallel_2",45.5],PARAMETER["Latitude_Of_Origin",23.0],UNIT["Meter",1.0]]`, 688894.5442286755, 2127632.7534185336, -87.6298, 41.8781},
	{2263, `PROJCS["NAD_1983_StatePlane_New_York_Long_Island_FIPS_3104_Feet",GEOGCS["GCS_North_American_1983",DATUM["D_North_American_1983",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Lambert_Conformal_Conic"],PARAMETER["False_Easting",984250.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",-74.0],PARAMETER["Standard_Parallel_1",41.0333333333333],PARAMETER["Standard_Parallel_2",40.6666666666667],PARAMETER["Latitude_Of_Origin",40.1666666666667],UNIT["US survey foot",0.304800609601219]]`, 988212.237182985, 211939.27856831398, -73.9857, 40.7484},
	{3395, `PROJCS["WGS_1984_World_Mercator",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Mercator"],PARAMETER["False_Easting",0.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",0.0],PARAMETER["Standard_Parallel_1",0.0],UNIT["Meter",1.0]]`, 16832542.27920734, -3987387.019596725, 151.2093, -33.8688},
	{3346, `PROJCS["LKS_1994_Lithuania_TM",GEOGCS["GCS_LKS_1994",DATUM["D_Lithuania_1994",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",24.0],PARAMETER["Scale_Factor",0.9998],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`, 582510.367297761, 6061948.683017084, 25.2797, 54.6872},
	{25832, `PROJCS["ETRS_1989_UTM_Zone_32N",GEOGCS["GCS_ETRS_1989",DATUM["D_ETRS_1989",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",9.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`, 465403.2844656688, 5247150.839302885, 8.5417, 47.3769},
	{27700, `PROJCS["British_National_Grid",GEOGCS["GCS_OSGB_1936",DATUM["D_OSGB_1936",SPHEROID["Airy_1830",6377563.396,299.3249646]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",400000.0],PARAMETER["False_Northing",-100000.0],PARAMETER["Central_Meridian",-2.0],PARAMETER["Scale_Factor",0.9996012717],PARAMETER["Latitude_Of_Origin",49.0],UNIT["Meter",1.0]]`, 530043.1949814068, 180358.20862000342, -0.1276, 51.5072},
	{32145, `PROJCS["NAD_1983_StatePlane_Vermont_FIPS_4400",GEOGCS["GCS_North_American_1983",DATUM["D_North_American_1983",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",-72.5],PARAMETER["Scale_Factor",0.999964286],PARAMETER["Latitude_Of_Origin",42.5],UNIT["Meter",1.0]]`, 493787.50490179786, 195543.33416470452, -72.5778, 44.2601},
	{3414, `PROJCS["SVY21_Singapore_TM",GEOGCS["GCS_SVY21",DATUM["D_SVY21",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",28001.642],PARAMETER["False_Northing",38744.572],PARAMETER["Central_Meridian",103.833333333333],PARAMETER["Scale_Factor",1.0],PARAMETER["Latitude_Of_Origin",1.36666666666667],UNIT["Meter",1.0]]`, 26495.534871029464, 37133.86847645403, 103.8198, 1.3521},
	{3994, `PROJCS["WGS_1984_Mercator_41",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Mercator"],PARAMETER["False_Easting",0.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",100.0],PARAMETER["Standard_Parallel_1",-41.0],UNIT["Meter",1.0]]`, -6730814.809125746, -6320521.100279815, 20.0, -60.0},
	{3577, `PROJCS["GDA_1994_Australia_Albers",GEOGCS["GCS_GDA_1994",DATUM["D_GDA_1994",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Albers"],PARAMETER["False_Easting",0.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",132.0],PARAMETER["Standard_Parallel_1",-18.0],PARAMETER["Standard_Parallel_2",-36.0],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`, 176715.9535210848, -2734549.475872395, 133.7751, -25.2744},
	{3111, `PROJCS["GDA_1994_VICGRID94",GEOGCS["GCS_GDA_1994",DATUM["D_GDA_1994",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Lambert_Conformal_Conic"],PARAMETER["False_Easting",2500000.0],PARAMETER["False_Northing",2500000.0],PARAMETER["Central_Meridian",145.0],PARAMETER["Standard_Parallel_1",-36.0],PARAMETER["Standard_Parallel_2",-38.0],PARAMETER["Latitude_Of_Origin",-37.0],UNIT["Meter",1.0]]`, 2496750.963165419, 2409712.430055314, 144.9631, -37.8136},
}

// shiftedByProj: systems whose datum PROJ shifts and Filee doesn't: right only to a few hundred metres.
var shiftedByProj = map[int]bool{27700: true}

func metres(lon1, lat1, lon2, lat2 float64) float64 {
	const r = 6371008.8
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

func refusedDetail(t *testing.T, err error) string {
	t.Helper()
	var r *convert.RefusedError
	if !errors.As(err, &r) || r.Key != "error.geo_unsupported_crs" {
		t.Fatalf("not a CRS refusal: %v", err)
	}
	return r.Detail
}

func TestPrjDefinitionConvertsLikePROJ(t *testing.T) {
	for _, c := range crsCases {
		crs, err := crsFromWkt(c.wkt)
		if err != nil {
			t.Fatalf("%d: %v", c.code, err)
		}
		p := crs.ToLonLat(xy(c.x, c.y))
		if shiftedByProj[c.code] {
			if !crs.Approximate() || metres(p.X, p.Y, c.lon, c.lat) > 300 {
				t.Fatalf("%d: %v", c.code, p)
			}
			continue
		}
		// 1e-8 degrees is about a millimetre.
		if crs.Approximate() || math.Abs(p.X-c.lon) > 5e-9 || math.Abs(p.Y-c.lat) > 5e-9 {
			t.Fatalf("%d: got %.10f %.10f, want %v %v", c.code, p.X, p.Y, c.lon, c.lat)
		}
	}
}

func TestEpsgCodeConvertsLikePROJ(t *testing.T) {
	for _, code := range []int{32633, 32733, 3857, 25832} {
		for _, c := range crsCases {
			if c.code != code {
				continue
			}
			p := crsFromEpsg(code).ToLonLat(xy(c.x, c.y))
			if math.Abs(p.X-c.lon) > 5e-9 || math.Abs(p.Y-c.lat) > 5e-9 {
				t.Fatalf("%d: %v", code, p)
			}
		}
	}
	crs := crsFromEpsg(32633)
	if crs.ToLonLat(Coord{602065.2074110014, 5340353.594303575, 123.5}).Z != 123.5 || crs.ToLonLat(xy(602065.2074110014, 5340353.594303575)).HasZ() {
		t.Fatal("height")
	}
}

func TestCrsNamesOfGeoJSON(t *testing.T) {
	for name, known := range map[string]bool{"urn:ogc:def:crs:OGC:1.3:CRS84": true, "EPSG:4326": true, "urn:ogc:def:crs:EPSG::4326": true,
		"urn:ogc:def:crs:EPSG::3857": true, "EPSG:32633": true, "EPSG:3035": false, "EPSG:": false, "something else": false} {
		if (crsFromName(name) != nil) != known {
			t.Fatal(name)
		}
	}
}

func TestGradsWithTheParisMeridian(t *testing.T) {
	crs, err := crsFromWkt(`GEOGCS["GCS_NTF_Paris",DATUM["Nouvelle_Triangulation_Francaise_(Paris)",SPHEROID["Clarke_1880_IGN",6378249.2,293.466021293627]],PRIMEM["Paris",2.33722917],UNIT["Grad",0.0157079632679489]]`)
	if err != nil {
		t.Fatal(err)
	}
	p := crs.ToLonLat(xy(0, 50))
	if math.Abs(p.X-2.33722917) > 1e-9 || math.Abs(p.Y-45) > 1e-9 || math.Abs(crs.ToLonLat(xy(10, 50)).X-(2.33722917+9)) > 1e-9 || !crs.Approximate() {
		t.Fatal(p)
	}
}

func TestProjectedWithAnotherPrimeMeridianOrAngularUnitIsRefused(t *testing.T) {
	paris := `PROJCS["NTF_Paris_Lambert_Zone_II",GEOGCS["GCS_NTF_Paris",DATUM["Nouvelle_Triangulation_Francaise_(Paris)",SPHEROID["Clarke_1880_IGN",6378249.2,293.466021293627]],PRIMEM["Paris",2.33722917],UNIT["Grad",0.0157079632679489]],PROJECTION["Lambert_Conformal_Conic"],PARAMETER["False_Easting",600000.0],PARAMETER["False_Northing",2200000.0],PARAMETER["Central_Meridian",0.0],PARAMETER["Standard_Parallel_1",52.0],PARAMETER["Scale_Factor",0.99987742],PARAMETER["Latitude_Of_Origin",52.0],UNIT["Meter",1.0]]`
	_, err := crsFromWkt(paris)
	if d := refusedDetail(t, err); !strings.Contains(d, "NTF_Paris_Lambert_Zone_II") || !strings.Contains(d, "prime meridian") {
		t.Fatal(d)
	}
	_, err = crsFromWkt(strings.Replace(paris, `PRIMEM["Paris",2.33722917]`, `PRIMEM["Greenwich",0.0]`, 1))
	if d := refusedDetail(t, err); !strings.Contains(d, "angular unit Grad") {
		t.Fatal(d)
	}
	ferro := `PROJCS["Test",GEOGCS["g",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Ferro",-17.6666667],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["Central_Meridian",31.0],UNIT["Meter",1.0]]`
	_, err = crsFromWkt(ferro)
	if d := refusedDetail(t, err); !strings.Contains(d, "prime meridian Ferro") {
		t.Fatal(d)
	}
	if _, err := crsFromWkt(strings.Replace(ferro, `PRIMEM["Ferro",-17.6666667]`, `PRIMEM["Greenwich",0.0]`, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestDatumsThatAreNotWGS84CompatibleAreApproximate(t *testing.T) {
	for wkt, approximate := range map[string]bool{
		`GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`:                        false,
		`GEOGCS["GCS_North_American_1983",DATUM["D_North_American_1983",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`:  false,
		`GEOGCS["GCS_Korea_2000",DATUM["D_Korea_2000",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`:                    false,
		`GEOGCS["GCS_Korean_Datum_1985",DATUM["D_Korean_Datum_1985",SPHEROID["Bessel_1841",6377397.155,299.1528128]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`:   true,
		`GEOGCS["GCS_European_1950",DATUM["D_European_1950",SPHEROID["International_1924",6378388.0,297.0]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`:            true,
		`GEOGCS["GCS_North_American_1927",DATUM["D_North_American_1927",SPHEROID["Clarke_1866",6378206.4,294.9786982]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`: true,
	} {
		crs, err := crsFromWkt(wkt)
		if err != nil || !crs.IsGeographic() || crs.Approximate() != approximate {
			t.Fatal(wkt, err)
		}
	}
}

func TestSystemsFileeCannotConvertAreRefusedWithTheirName(t *testing.T) {
	for wkt, want := range map[string]string{
		`PROJCS["ETRS89_LAEA_Europe",GEOGCS["GCS_ETRS_1989",DATUM["D_ETRS_1989",SPHEROID["GRS_1980",6378137.0,298.257222101]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Lambert_Azimuthal_Equal_Area"],PARAMETER["False_Easting",4321000.0],PARAMETER["False_Northing",3210000.0],PARAMETER["Central_Meridian",10.0],PARAMETER["Latitude_Of_Origin",52.0],UNIT["Meter",1.0]]`: "Lambert_Azimuthal_Equal_Area",
		`PROJCS["WGS_1984_UPS_North",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Stereographic"],PARAMETER["False_Easting",2000000.0],UNIT["Meter",1.0]]`:                                                                                                                                "Stereographic",
		`PROJCRS["WGS 84 / UTM zone 33N",BASEGEOGCRS["WGS 84"],CONVERSION["UTM zone 33N"]]`:                                                "WGS 84 / UTM zone 33N",
		`GEOCCS["WGS 84 (geocentric)",DATUM["WGS_1984",SPHEROID["WGS_1984",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["metre",1]]`: "WGS 84 (geocentric)",
		`LOCAL_CS["Site grid",UNIT["metre",1]]`:                                                                           "Site grid",
		`PROJCS["No ellipsoid",GEOGCS["x",DATUM["d"]],PROJECTION["Transverse_Mercator"]]`:                                 "No ellipsoid",
		`PROJCS["No projection",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]]]]`: "No projection",
		"not a definition at all":      "NOT",
		`GEOGCS["unclosed"`:            "missing closing bracket",
		`GEOGCS["unterminated string]`: "missing closing quote",
		"":                             "empty definition",
		strings.Repeat("A[", 5000) + strings.Repeat("]", 5000): "nested",
	} {
		_, err := crsFromWkt(wkt)
		if d := refusedDetail(t, err); !strings.Contains(strings.ToLower(d), strings.ToLower(want)) {
			t.Fatalf("%q: %q", want, d)
		}
	}
	_, err := crsFromWkt(`GEOGCS["` + strings.Repeat("x", 100_000) + `"]`)
	refusedDetail(t, err)
}

func TestWktTextWithDoubledQuotesAndSpacing(t *testing.T) {
	crs, err := crsFromWkt(`  GEOGCS [ "My ""quoted"" system" , DATUM [ "D_WGS_1984" , SPHEROID [ "WGS_1984" , 6378137.0 , 298.257223563 ] ] , UNIT [ "Degree" , 0.0174532925199433 ] ]  `)
	if err != nil || crs.Name() != `My "quoted" system` || !crs.IsIdentity() {
		t.Fatal(err)
	}
}

func TestRandomCrsTextOnlyRefuses(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	const alphabet = "GEOCSPROJ[]\",.0123456789-+eE _ABCDHMNTU()"
	for i := 0; i < 3000; i++ {
		b := make([]byte, r.Intn(120))
		for k := range b {
			b[k] = alphabet[r.Intn(len(alphabet))]
		}
		if _, err := crsFromWkt(string(b)); err != nil {
			var refused *convert.RefusedError
			if !errors.As(err, &refused) {
				t.Fatal(err)
			}
		}
	}
}

func TestWktGeometryCanonicalForm(t *testing.T) {
	for in, want := range map[string]string{
		"POINT (1 2)":                "POINT (1 2)",
		"point(1.5 -2.25)":           "POINT (1.5 -2.25)",
		"POINT Z (1 2 3)":            "POINT Z (1 2 3)",
		"POINTZ(1 2 3)":              "POINT Z (1 2 3)",
		"POINT ZM (1 2 3 4)":         "POINT Z (1 2 3)",
		"POINT M (1 2 9)":            "POINT (1 2)",
		"SRID=4326;POINT (1 2)":      "POINT (1 2)",
		"LINESTRING (0 0, 1 1, 2 0)": "LINESTRING (0 0, 1 1, 2 0)",
		"POLYGON ((0 0, 4 0, 4 4, 0 4, 0 0), (1 1, 1 2, 2 2, 2 1, 1 1))":          "POLYGON ((0 0, 4 0, 4 4, 0 4, 0 0), (1 1, 1 2, 2 2, 2 1, 1 1))",
		"POLYGON ((0 0, 0 4, 4 4, 4 0, 0 0))":                                     "POLYGON ((0 0, 4 0, 4 4, 0 4, 0 0))",
		"POLYGON ((0 0, 4 0, 4 4, 0 4))":                                          "POLYGON ((0 0, 4 0, 4 4, 0 4, 0 0))",
		"MULTIPOINT ((1 2), (3 4))":                                               "MULTIPOINT ((1 2), (3 4))",
		"MULTIPOINT (1 2, 3 4)":                                                   "MULTIPOINT ((1 2), (3 4))",
		"MULTILINESTRING ((0 0, 1 1), (2 2, 3 3))":                                "MULTILINESTRING ((0 0, 1 1), (2 2, 3 3))",
		"MULTIPOLYGON (((0 0, 1 0, 1 1, 0 1, 0 0)), ((5 5, 6 5, 6 6, 5 6, 5 5)))": "MULTIPOLYGON (((0 0, 1 0, 1 1, 0 1, 0 0)), ((5 5, 6 5, 6 6, 5 6, 5 5)))",
		"GEOMETRYCOLLECTION (POINT (1 2), LINESTRING (0 0, 1 1))":                 "GEOMETRYCOLLECTION (POINT (1 2), LINESTRING (0 0, 1 1))",
	} {
		g, err := parseWkt(in)
		if err != nil || g == nil || writeWkt(g) != want {
			t.Fatalf("%s: %v %v", in, err, g)
		}
	}
	for _, empty := range []string{"POINT EMPTY", "LINESTRING EMPTY", "POLYGON EMPTY", "MULTIPOLYGON EMPTY", "GEOMETRYCOLLECTION EMPTY", "MULTIPOINT (EMPTY)"} {
		if g, err := parseWkt(empty); err != nil || g != nil {
			t.Fatal(empty, err)
		}
	}
	for _, bad := range []string{"", "POINT", "POINT (1)", "POINT (a b)", "POINT (1 2", "POINT (1 2) trailing", "TRIANGLE ((0 0, 1 0, 0 1, 0 0))",
		"LINESTRING (0 0)", "SRID=4326 POINT (1 2)", "POINT (1e999 2)", "POINT (NaN 2)",
		strings.Repeat("GEOMETRYCOLLECTION (", 10_000) + "POINT (1 2)" + strings.Repeat(")", 10_000)} {
		if _, err := parseWkt(bad); err == nil {
			t.Fatal(bad[:min(40, len(bad))])
		}
	}
	r := rand.New(rand.NewSource(11))
	words := strings.Split("POINT LINESTRING POLYGON MULTI GEOMETRYCOLLECTION Z M EMPTY ( ) , 0 1 2 3.5 -4 1e3 SRID=4326; ", " ")
	for i := 0; i < 5000; i++ {
		parts := make([]string, r.Intn(14))
		for k := range parts {
			parts[k] = words[r.Intn(len(words))]
		}
		parseWkt(strings.Join(parts, " ")) // must not panic
	}
}
