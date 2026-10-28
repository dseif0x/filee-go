package hwpx

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/hwp/xmltree"
)

// Word equations (Office Math) in DOCX → HWPX (#143): every equation becomes a 한글 equation object with an equation
// script, sized so that rhwp draws it unstretched, and the text after it still arrives. Formats without equations
// get a linear form ("(a+b)/(2c)").

func mR(text string, sty ...string) string {
	pr := ""
	if len(sty) > 0 {
		pr = `<m:rPr><m:sty m:val="` + sty[0] + `"/></m:rPr>`
	}
	return "<m:r>" + pr + "<m:t>" + Escape(text) + "</m:t></m:r>"
}

func mFrac(n, d string) string { return "<m:f><m:num>" + n + "</m:num><m:den>" + d + "</m:den></m:f>" }
func mSup(b, s string) string  { return "<m:sSup><m:e>" + b + "</m:e><m:sup>" + s + "</m:sup></m:sSup>" }
func mSub(b, s string) string  { return "<m:sSub><m:e>" + b + "</m:e><m:sub>" + s + "</m:sub></m:sSub>" }
func mSubSup(b, sub, sup string) string {
	return "<m:sSubSup><m:e>" + b + "</m:e><m:sub>" + sub + "</m:sub><m:sup>" + sup + "</m:sup></m:sSubSup>"
}
func mRad(body, degree string) string {
	hide := ""
	if degree == "" {
		hide = `<m:degHide m:val="1"/>`
	}
	return "<m:rad><m:radPr>" + hide + "</m:radPr><m:deg>" + degree + "</m:deg><m:e>" + body + "</m:e></m:rad>"
}
func mNary(chr, sub, sup, body string) string {
	return `<m:nary><m:naryPr><m:chr m:val="` + chr + `"/><m:limLoc m:val="undOvr"/></m:naryPr><m:sub>` + sub + "</m:sub><m:sup>" + sup + "</m:sup><m:e>" + body + "</m:e></m:nary>"
}
func mDelimited(content, open, close string) string {
	pr := ""
	if open != "" {
		pr += `<m:begChr m:val="` + open + `"/>`
	}
	if close != "" || open != "" {
		pr += `<m:endChr m:val="` + close + `"/>`
	}
	return "<m:d><m:dPr>" + pr + "</m:dPr><m:e>" + content + "</m:e></m:d>"
}
func mMatrix(rows ...[]string) string {
	s := "<m:m>"
	for _, r := range rows {
		s += "<m:mr>"
		for _, c := range r {
			s += "<m:e>" + c + "</m:e>"
		}
		s += "</m:mr>"
	}
	return s + "</m:m>"
}

var equationSamples = []struct{ omml, script, linear string }{
	{mFrac(mR("a+b"), mR("2c")), "{{a + b} over {2 c}}", "(a+b)/(2c)"},
	{mSup(mR("x"), mR("2")) + mR("+") + mSubSup(mR("y"), mR("i"), mR("n")), "x ^{2} + y _{i} ^{n}", "x^2+y_i^n"},
	{mRad(mSup(mR("b"), mR("2"))+mR("−4ac"), "") + mR("+") + mRad(mR("x"), mR("3")), "sqrt {b ^{2} - 4 ac} + root {3} of {x}", "√(b^2-4ac)+[3]√x"},
	{mNary("∑", mR("i=1"), mR("n"), mSup(mR("i"), mR("2"))), "SUM _{i = 1} ^{n} i ^{2}", "∑_(i=1)^n i^2"},
	{mNary("∫", mR("0"), mR("∞"), mSup(mR("e"), mR("−")+mSup(mR("x"), mR("2")))+mR("dx")), "INT _{0} ^{INF} {e ^{- x ^{2}} dx}", "∫_0^∞ e^(-x^2)dx"},
	{mR("α+β=γ, Ω"), "alpha + beta = gamma , Omega", "α+β=γ,Ω"},
	{mR("A=") + mDelimited(mMatrix([]string{mR("1"), mR("2")}, []string{mR("3"), mR("4")}), "", ""), "A = pmatrix {1 & 2 # 3 & 4}", "A=(1, 2; 3, 4)"},
	{mSub(mR("ω"), mR("ref")) + mR("+") + mSub(mR("x"), mR("in")) + mR("+") + mDelimited(mR("t−1"), "", ""),
		"omega _{ref} + x _{i n} + LEFT ( t - 1 RIGHT )", "ω_(ref)+x_(in)+(t-1)"},
	{"<m:func><m:fName><m:limLow><m:e>" + mR("lim", "p") + "</m:e><m:lim>" + mR("x→0") + "</m:lim></m:limLow></m:fName><m:e>" +
		mFrac("<m:func><m:fName>"+mR("sin", "p")+"</m:fName><m:e>"+mR("x")+"</m:e></m:func>", mR("x")) + "</m:e></m:func>",
		"lim _{x rarrow 0} {{sin x} over {x}}", "lim_(x→0) (sin x)/x"},
	{"<m:acc><m:e>" + mR("a") + "</m:e></m:acc>" + mR("+") + `<m:bar><m:barPr><m:pos m:val="top"/></m:barPr><m:e>` + mR("AB") + "</m:e></m:bar>",
		"hat {a} + OVERLINE {AB}", "â+overline(AB)"},
	{mR("f(x)=") + mDelimited("<m:eqArr><m:e>"+mR("x,&x≥0")+"</m:e><m:e>"+mR("−x,&x<0")+"</m:e></m:eqArr>", "{", ""),
		"f ( x ) = cases {x , & x GEQ 0 # - x , & x < 0}", "f(x)={x, x≥0; -x, x<0"},
}

func parseOmml(t *testing.T, omml string) MathNode {
	t.Helper()
	d, err := xmltree.ParseString("<m:oMath " + docxNamespaces + ">" + omml + "</m:oMath>")
	if err != nil {
		t.Fatal(err)
	}
	return ReadOmml(d.Root)
}

func TestOmmlBecomesEquationScriptAndLinearText(t *testing.T) {
	for _, s := range equationSamples {
		m := parseOmml(t, s.omml)
		eq(t, "script", Script(m), s.script)
		eq(t, "linear", norm.NFC.String(Linear(m)), norm.NFC.String(s.linear))
	}
}

func TestEquationBoxesFollowTheLayoutOfTheScript(t *testing.T) {
	box := func(omml string) MathBox { return MeasureMath(parseOmml(t, omml), 1000) }
	letter := box(mR("x"))
	if math.Abs(letter.Width-577.5) > 1e-6 || letter.Height != 1000 || letter.Baseline != 800 {
		t.Errorf("letter %+v", letter)
	}
	fraction := box(mFrac(mR("a"), mR("b")))
	if fraction.Height <= 2*letter.Height || fraction.Width <= letter.Width {
		t.Errorf("fraction %+v", fraction)
	}
	if box(mNary("∑", mR("i=1"), mR("n"), mR("i"))).Height <= fraction.Height {
		t.Error("sum not taller than a fraction")
	}
	if box(mSup(mR("x"), mR("2"))).Width >= box(mR("x2")).Width {
		t.Error("exponent not smaller")
	}
}

func equationDocx() *docxBuilder {
	b := newDocx()
	b.add(wP(wR("Inline ") + "<m:oMath>" + mR("E=m") + mSup(mR("c"), mR("2")) + "</m:oMath>" + wR(" in a sentence.")))
	for _, s := range equationSamples {
		b.add(wP("<m:oMathPara><m:oMath>" + s.omml + "</m:oMath></m:oMathPara>"))
	}
	return b.add(wP(wR("Text after the equations.")))
}

func TestWordEquationsBecomeHwpxEquationObjects(t *testing.T) {
	hwpx := docxToHwpx(t, equationDocx(), "equations")
	section := section0(t, hwpx)
	equations := section.DescendantsNamed("equation")
	want := []string{"E = m c ^{2}"}
	for _, s := range equationSamples {
		want = append(want, s.script)
	}
	var got []string
	for _, e := range equations {
		got = append(got, e.Child("script").Value())
		eq(t, "treatAsChar", e.Child("pos").A("treatAsChar"), "1")
		eq(t, "baseUnit", e.A("baseUnit"), "1000")
		if atoi(t, e.Child("sz"), "width") <= 0 || atoi(t, e.Child("sz"), "height") < 1000 {
			t.Errorf("size %s", e.Child("sz"))
		}
		if e.Child("shapeComment").Value() == "" {
			t.Error("empty shape comment")
		}
	}
	eq(t, "scripts", strings.Join(got, "|"), strings.Join(want, "|"))
	eq(t, "comment", equations[1].Child("shapeComment").Value(), "(a+b)/(2c)")
	inline := paragraphWith(t, section, "Inline")
	if !strings.Contains(inline.Value(), "in a sentence.") || inline.FirstDescendant("equation") == nil {
		t.Error("inline equation not between its words")
	}
	var display *xmltree.Element
	for _, a := range equations[1].Ancestors() {
		if a.Local == "p" {
			display = a
			break
		}
	}
	eq(t, "centred", headItem(t, hwpx, "paraPr", display.A("paraPrIDRef")).Child("align").A("horizontal"), "CENTER")
}

func TestFormatsWithoutEquationsGetTheLinearForm(t *testing.T) {
	dir := t.TempDir()
	path := equationDocx().save(t, filepath.Join(dir, "linear.docx"))
	d, err := ReadDocx(path, filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, b := range d.Sections[0].Blocks {
		if p, ok := b.(*doc.Paragraph); ok {
			texts = append(texts, doc.PlainText(p.Inlines))
		}
	}
	text := strings.Join(texts, "\n")
	for _, want := range []string{"Inline E=mc^2 in a sentence.", "(a+b)/(2c)", "lim_(x→0) (sin x)/x"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in %s", want, text)
		}
	}
	_ = fmt.Sprint
}
