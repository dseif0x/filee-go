package hwpx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/doc"
)

// pptxBuilder builds small presentations: a master with title / body placeholders and a decorative bar, one layout,
// an Office theme, slides and pictures.
type pptxBuilder struct {
	slides []string
	images [][]byte
	nextID int
}

const pptxNamespaces = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

const pptxTreeStart = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`

const pptxMaster = `<p:cSld><p:spTree>` + pptxTreeStart +
	`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>` +
	`<p:spPr><a:xfrm><a:off x="838200" y="365125"/><a:ext cx="10515600" cy="1325563"/></a:xfrm></p:spPr><p:txBody><a:bodyPr anchor="ctr"/><a:lstStyle/><a:p/></p:txBody></p:sp>` +
	`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Body"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr>` +
	`<p:spPr><a:xfrm><a:off x="838200" y="1825625"/><a:ext cx="10515600" cy="4351338"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p/></p:txBody></p:sp>` +
	`<p:sp><p:nvSpPr><p:cNvPr id="4" name="Bar"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>` +
	`<p:spPr><a:xfrm><a:off x="0" y="6705600"/><a:ext cx="12192000" cy="152400"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom>` +
	`<a:solidFill><a:schemeClr val="accent2"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr></p:sp>` +
	`</p:spTree></p:cSld>` +
	`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` +
	`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>` +
	`<p:txStyles>` +
	`<p:titleStyle><a:lvl1pPr algn="ctr"><a:buNone/><a:defRPr sz="4400"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mj-lt"/><a:ea typeface="+mj-ea"/></a:defRPr></a:lvl1pPr></p:titleStyle>` +
	`<p:bodyStyle><a:lvl1pPr marL="228600" indent="-228600"><a:buChar char="•"/><a:defRPr sz="2800"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill></a:defRPr></a:lvl1pPr>` +
	`<a:lvl2pPr marL="685800" indent="-228600"><a:buChar char="–"/><a:defRPr sz="2400"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill></a:defRPr></a:lvl2pPr></p:bodyStyle>` +
	`<p:otherStyle><a:lvl1pPr><a:defRPr sz="1800"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill></a:defRPr></a:lvl1pPr></p:otherStyle>` +
	`</p:txStyles>`

const pptxLayout = `<p:cSld name="Title and Content"><p:spTree>` + pptxTreeStart +
	`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p/></p:txBody></p:sp>` +
	`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Content"/><p:cNvSpPr/><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p/></p:txBody></p:sp>` +
	`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>`

const pptxTheme = `<a:themeElements><a:clrScheme name="Office">` +
	`<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
	`<a:dk2><a:srgbClr val="44546A"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2>` +
	`<a:accent1><a:srgbClr val="4472C4"/></a:accent1><a:accent2><a:srgbClr val="ED7D31"/></a:accent2><a:accent3><a:srgbClr val="A5A5A5"/></a:accent3>` +
	`<a:accent4><a:srgbClr val="FFC000"/></a:accent4><a:accent5><a:srgbClr val="5B9BD5"/></a:accent5><a:accent6><a:srgbClr val="70AD47"/></a:accent6>` +
	`<a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme>` +
	`<a:fontScheme name="Office"><a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface=""/><a:cs typeface=""/><a:font script="Hang" typeface="맑은 고딕"/></a:majorFont>` +
	`<a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/><a:font script="Hang" typeface="맑은 고딕"/></a:minorFont></a:fontScheme>` +
	`<a:fmtScheme name="Office"><a:fillStyleLst/><a:lnStyleLst/><a:effectStyleLst/><a:bgFillStyleLst/></a:fmtScheme></a:themeElements>`

func newPptx() *pptxBuilder { return &pptxBuilder{nextID: 10} }

func (b *pptxBuilder) image(png []byte) string {
	b.images = append(b.images, png)
	return fmt.Sprintf("rIdImg%d", len(b.images))
}

func (b *pptxBuilder) slide(shapes, background, backgroundImage string) {
	bg := ""
	if backgroundImage != "" {
		bg = `<p:bg><p:bgPr><a:blipFill dpi="0" rotWithShape="1"><a:blip r:embed="` + backgroundImage + `"/><a:stretch><a:fillRect/></a:stretch></a:blipFill><a:effectLst/></p:bgPr></p:bg>`
	} else if background != "" {
		bg = `<p:bg><p:bgPr><a:solidFill><a:srgbClr val="` + strings.TrimPrefix(background, "#") + `"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>`
	}
	b.slides = append(b.slides, "<p:sld "+pptxNamespaces+"><p:cSld>"+bg+"<p:spTree>"+pptxTreeStart+shapes+"</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>")
}

func (b *pptxBuilder) id() int {
	b.nextID++
	return b.nextID - 1
}

func pptxParagraph(text, pPr, rPr string) string {
	if rPr == "" {
		rPr = `<a:rPr lang="ko-KR"/>`
	}
	return "<a:p>" + pPr + "<a:r>" + rPr + "<a:t>" + Escape(text) + "</a:t></a:r></a:p>"
}

func pptxXfrm(x, y, cx, cy int64) string {
	return fmt.Sprintf(`<a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`, x, y, cx, cy)
}

func (b *pptxBuilder) title(text string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/>`, b.id()) +
		"<p:txBody><a:bodyPr/><a:lstStyle/>" + pptxParagraph(text, "", "") + "</p:txBody></p:sp>"
}

func (b *pptxBuilder) body(items ...[2]any) string {
	s := fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Content"/><p:cNvSpPr/><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr><p:spPr/>`, b.id()) + "<p:txBody><a:bodyPr/><a:lstStyle/>"
	for _, it := range items {
		pPr := ""
		if it[1].(int) > 0 {
			pPr = fmt.Sprintf(`<a:pPr lvl="%d"/>`, it[1].(int))
		}
		s += pptxParagraph(it[0].(string), pPr, "")
	}
	return s + "</p:txBody></p:sp>"
}

func (b *pptxBuilder) shape(geometry string, x, y, cx, cy int64, fill, text string) string {
	s := fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Shape"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>`, b.id()) +
		"<p:spPr>" + pptxXfrm(x, y, cx, cy) + `<a:prstGeom prst="` + geometry + `"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="` + strings.TrimPrefix(fill, "#") + `"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr>`
	if text != "" {
		s += `<p:txBody><a:bodyPr anchor="ctr"/><a:lstStyle/>` + pptxParagraph(text, `<a:pPr algn="ctr"/>`, `<a:rPr lang="ko-KR" sz="2000" b="1"><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill></a:rPr>`) + "</p:txBody>"
	}
	return s + "</p:sp>"
}

func (b *pptxBuilder) picture(id string, x, y, cx, cy int64) string {
	return fmt.Sprintf(`<p:pic><p:nvPicPr><p:cNvPr id="%d" name="Picture"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr>`, b.id()) +
		`<p:blipFill><a:blip r:embed="` + id + `"/><a:stretch><a:fillRect/></a:stretch></p:blipFill>` +
		"<p:spPr>" + pptxXfrm(x, y, cx, cy) + `<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`
}

func (b *pptxBuilder) table(x, y, columnWidth, rowHeight int64, rows ...[]string) string {
	s := fmt.Sprintf(`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="Table"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr>`, b.id()) +
		fmt.Sprintf(`<p:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm>`, x, y, columnWidth*int64(len(rows[0])), rowHeight*int64(len(rows))) +
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl>` +
		`<a:tblPr firstRow="1" bandRow="1"><a:tableStyleId>{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}</a:tableStyleId></a:tblPr><a:tblGrid>`
	for range rows[0] {
		s += fmt.Sprintf(`<a:gridCol w="%d"/>`, columnWidth)
	}
	s += "</a:tblGrid>"
	for _, row := range rows {
		s += fmt.Sprintf(`<a:tr h="%d">`, rowHeight)
		for _, c := range row {
			s += "<a:tc><a:txBody><a:bodyPr/><a:lstStyle/>" + pptxParagraph(c, "", "") + "</a:txBody><a:tcPr/></a:tc>"
		}
		s += "</a:tr>"
	}
	return s + "</a:tbl></a:graphicData></a:graphic></p:graphicFrame>"
}

func (b *pptxBuilder) group(x, y, cx, cy int64, scale float64, children string) string {
	return fmt.Sprintf(`<p:grpSp><p:nvGrpSpPr><p:cNvPr id="%d" name="Group"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>`, b.id()) +
		fmt.Sprintf(`<p:grpSpPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/><a:chOff x="0" y="0"/><a:chExt cx="%d" cy="%d"/></a:xfrm></p:grpSpPr>`, x, y, cx, cy, int64(float64(cx)/scale), int64(float64(cy)/scale)) +
		children + "</p:grpSp>"
}

func relationships(rels ...[3]string) string {
	s := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	for _, r := range rels {
		s += fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/%s" Target="%s"/>`, r[0], r[1], r[2])
	}
	return s + "</Relationships>"
}

func (b *pptxBuilder) save(t *testing.T, path string) string {
	t.Helper()
	presentationRels := [][3]string{{"rId1", "slideMaster", "slideMasters/slideMaster1.xml"}, {"rId2", "theme", "theme/theme1.xml"}}
	ids := ""
	for i := range b.slides {
		presentationRels = append(presentationRels, [3]string{fmt.Sprintf("rId%d", i+10), "slide", fmt.Sprintf("slides/slide%d.xml", i+1)})
		ids += fmt.Sprintf(`<p:sldId id="%d" r:id="rId%d"/>`, 256+i, i+10)
	}
	slideRels := [][3]string{{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"}}
	for i := range b.images {
		slideRels = append(slideRels, [3]string{fmt.Sprintf("rIdImg%d", i+1), "image", fmt.Sprintf("../media/image%d.png", i+1)})
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	write := func(name string, data []byte) {
		w, _ := z.Create(name)
		w.Write(data)
	}
	write("[Content_Types].xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/></Types>`))
	write("_rels/.rels", []byte(relationships([3]string{"rId1", "officeDocument", "ppt/presentation.xml"})))
	write("ppt/presentation.xml", []byte("<p:presentation "+pptxNamespaces+`><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>`+ids+`</p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`))
	write("ppt/_rels/presentation.xml.rels", []byte(relationships(presentationRels...)))
	write("ppt/slideMasters/slideMaster1.xml", []byte("<p:sldMaster "+pptxNamespaces+">"+pptxMaster+"</p:sldMaster>"))
	write("ppt/slideMasters/_rels/slideMaster1.xml.rels", []byte(relationships([3]string{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"}, [3]string{"rId2", "theme", "../theme/theme1.xml"})))
	write("ppt/slideLayouts/slideLayout1.xml", []byte("<p:sldLayout "+pptxNamespaces+">"+pptxLayout+"</p:sldLayout>"))
	write("ppt/slideLayouts/_rels/slideLayout1.xml.rels", []byte(relationships([3]string{"rId1", "slideMaster", "../slideMasters/slideMaster1.xml"})))
	write("ppt/theme/theme1.xml", []byte("<a:theme "+pptxNamespaces+` name="Office">`+pptxTheme+"</a:theme>"))
	for i, s := range b.slides {
		write(fmt.Sprintf("ppt/slides/slide%d.xml", i+1), []byte(s))
		write(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1), []byte(relationships(slideRels...)))
	}
	for i, png := range b.images {
		write(fmt.Sprintf("ppt/media/image%d.png", i+1), png)
	}
	z.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPresentationsSurviveTheWayThroughHwpx(t *testing.T) {
	b := newPptx()
	chart := b.image(pngBytes(160, 90))
	b.slide(b.title("2026년 사업 보고"), "", "")
	b.slide(b.title("다음 분기 계획")+
		b.body([2]any{"매출 증가", 0}, [2]any{"지점 확대", 1})+
		b.shape("roundRect", 838200, 4400000, 3000000, 1500000, "#7F77DD", "강조")+
		b.picture(chart, 8000000, 1900000, 3000000, 1687500)+
		b.table(4000000, 4400000, 1200000, 370840, []string{"지점", "매출"}, []string{"강남", "16.2억"})+
		b.group(6000000, 4000000, 2000000, 1000000, 0.5, b.shape("ellipse", 0, 0, 4000000, 2000000, "#1D9E75", "단계 1")),
		"#1F1E2D", "")
	dir := t.TempDir()
	source, err := ReadPptx(b.save(t, filepath.Join(dir, "계획.pptx")), filepath.Join(dir, "pptx-media"))
	if err != nil {
		t.Fatal(err)
	}
	// One page per slide, sized like the slide.
	eq(t, "page", *source.Sections[0].Page, doc.Page{Width: 96000, Height: 54000})
	eq(t, "pages", len(source.Sections[0].Blocks), 2)
	read, _ := roundTrip(t, source, "pptx")
	ellipse, pictures, bodyBullet := false, 0, false
	var all []doc.Block
	for _, s := range read.Sections {
		all = append(all, s.Blocks...)
	}
	for _, in := range doc.Inlines(all) {
		switch v := in.(type) {
		case *doc.TextBox:
			if v.Anchor == nil {
				t.Error("text box without anchor")
			}
			if v.Shape == doc.ShapeEllipse && allText(v.Blocks) == "단계 1" {
				ellipse = true
			}
			if strings.Contains(allText(v.Blocks), "• 매출 증가") {
				bodyBullet = true
			}
		case *doc.Image:
			if v.Anchor != nil && v.Anchor.HorizontalRelativeTo == "PAPER" {
				pictures++
			}
		}
	}
	if !ellipse || pictures != 1 || !bodyBullet {
		t.Errorf("ellipse %v pictures %d bullet %v", ellipse, pictures, bodyBullet)
	}
}

func TestAPictureBackgroundCoversTheSlide(t *testing.T) {
	b := newPptx()
	chalkboard := b.image(pngBytes(160, 90))
	b.slide(b.title("채널A, 〈금쪽같은 내새끼〉"), "", chalkboard)
	dir := t.TempDir()
	read, err := ReadPptx(b.save(t, filepath.Join(dir, "칠판.pptx")), filepath.Join(dir, "pptx-media"))
	if err != nil {
		t.Fatal(err)
	}
	var images []*doc.Image
	for _, in := range doc.Inlines(read.Sections[0].Blocks) {
		if img, ok := in.(*doc.Image); ok {
			images = append(images, img)
		}
	}
	if len(images) != 1 {
		t.Fatalf("%d pictures", len(images))
	}
	eq(t, "size", fmt.Sprint(*images[0].Width, *images[0].Height), "96000 54000")
	eq(t, "offset", fmt.Sprint(images[0].Anchor.HorizontalOffset, images[0].Anchor.VerticalOffset), "0 0")
}

func TestAutoNumberLabels(t *testing.T) {
	for scheme, want := range map[string]string{"arabicPeriod": "3.", "alphaLcParenR": "c)", "romanUcParenBoth": "(III)", "circleNumDbPlain": "③", "arabicPlain": "3"} {
		eq(t, scheme, AutoNumberLabel(scheme, 3), want)
	}
}
