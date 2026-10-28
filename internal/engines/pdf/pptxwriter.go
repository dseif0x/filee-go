// A PowerPoint presentation (PPTX, PresentationML) whose slides each show one
// picture, fitted and centred, with optional speaker notes: the package
// PDF → PPTX needs. Written by hand, as small as PowerPoint, Impress and
// Keynote accept: presentation, one master with one blank layout, a theme,
// presentation / view / table style properties, the slides with their
// pictures, a notes master and notes slides when there is text, and the
// document properties. Slides are written into the ZIP as they come, so a
// long PDF never has all its pages in memory.

package pdf

import (
	"archive/zip"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
)

const (
	emuPerPoint  = 12700.0
	minSlideSide = 914400   // 1 inch
	maxSlideSide = 51206400 // 56 inches
	pptxTypes    = "application/vnd.openxmlformats-officedocument.presentationml."
	pNamespaces  = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`
	treeStart    = `<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
	colorMap = `<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" ` +
		`accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>`
	notesWidth  = 6858000
	notesHeight = 9144000
)

type pictureDeck struct {
	path, temp  string
	f           *os.File
	z           *zip.Writer
	hasNotes    []bool
	SlideWidth  int64
	SlideHeight int64
	saved       bool
	err         error
}

func clampSide(v int64) int64 { return max(minSlideSide, min(v, maxSlideSide)) }

func newPictureDeck(path string, slideWidth, slideHeight int64) (*pictureDeck, error) {
	temp := path + ".tmp"
	f, err := os.Create(temp)
	if err != nil {
		return nil, err
	}
	return &pictureDeck{path: path, temp: temp, f: f, z: zip.NewWriter(f), SlideWidth: clampSide(slideWidth), SlideHeight: clampSide(slideHeight)}, nil
}

// slideSize for a page of width × height points: the page's own size, scaled
// into PowerPoint's 1–56 inch limits keeping the shape as far as they allow.
func slideSize(width, height float64) (int64, int64) {
	cx, cy := max(1, width)*emuPerPoint, max(1, height)*emuPerPoint
	shrink := min(1, maxSlideSide/max(cx, cy))
	cx, cy = cx*shrink, cy*shrink
	grow := max(1, minSlideSide/min(cx, cy))
	cx, cy = cx*grow, cy*grow
	return clampSide(int64(math.Round(cx))), clampSide(int64(math.Round(cy)))
}

// fit places a picture of width × height on the slide: as large as fits, centred (EMU).
func (d *pictureDeck) fit(width, height float64) (x, y, cx, cy int64) {
	scale := min(float64(d.SlideWidth)/max(width, 1e-6), float64(d.SlideHeight)/max(height, 1e-6))
	cx = max(1, min(int64(math.Round(width*scale)), d.SlideWidth))
	cy = max(1, min(int64(math.Round(height*scale)), d.SlideHeight))
	return (d.SlideWidth - cx) / 2, (d.SlideHeight - cy) / 2, cx, cy
}

func (d *pictureDeck) write(name, xml string, method uint16) {
	if d.err != nil {
		return
	}
	w, err := d.z.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Now()})
	if err != nil {
		d.err = err
		return
	}
	if _, err := w.Write([]byte(xml)); err != nil {
		d.err = err
	}
}

func (d *pictureDeck) part(name, xml string) { d.write(name, xmlDeclaration+xml, zip.Deflate) }

func pptRels(rels ...[3]string) string {
	var b strings.Builder
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, r := range rels {
		t := r[1]
		if !strings.Contains(t, "://") {
			t = relBase + t
		}
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="%s" Target="%s"/>`, r[0], t, r[2])
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

// addSlide adds a slide showing the JPEG, fitted for a picture of width ×
// height, with notes (lines separated by CR LF or LF) when there are any.
func (d *pictureDeck) addSlide(jpeg []byte, width, height float64, name, notes string) {
	n := len(d.hasNotes) + 1
	has := strings.TrimSpace(notes) != ""
	d.hasNotes = append(d.hasNotes, has)
	d.write(fmt.Sprintf("ppt/media/image%d.jpeg", n), string(jpeg), zip.Store)
	x, y, cx, cy := d.fit(width, height)
	nm := xmlEscape(name)
	d.part(fmt.Sprintf("ppt/slides/slide%d.xml", n), `<p:sld `+pNamespaces+`><p:cSld>`+treeStart+
		`<p:pic><p:nvPicPr><p:cNvPr id="2" name="`+nm+`" descr="`+nm+`"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr>`+
		`<p:blipFill><a:blip r:embed="rId2"/><a:stretch><a:fillRect/></a:stretch></p:blipFill>`+
		fmt.Sprintf(`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`, x, y, cx, cy)+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`+
		`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`)
	rels := [][3]string{{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"}, {"rId2", "image", fmt.Sprintf("../media/image%d.jpeg", n)}}
	if has {
		rels = append(rels, [3]string{"rId3", "notesSlide", fmt.Sprintf("../notesSlides/notesSlide%d.xml", n)})
	}
	d.part(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n), pptRels(rels...))
	if !has {
		return
	}
	var body strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n") {
		if line == "" {
			body.WriteString("<a:p/>")
		} else {
			fmt.Fprintf(&body, "<a:p><a:r><a:t>%s</a:t></a:r></a:p>", xmlEscape(line))
		}
	}
	d.part(fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", n), `<p:notes `+pNamespaces+`><p:cSld>`+treeStart+
		`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image Placeholder 1"/><p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/></p:cNvSpPr>`+
		`<p:nvPr><p:ph type="sldImg"/></p:nvPr></p:nvSpPr><p:spPr/></p:sp>`+
		`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes Placeholder 2"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>`+
		`<p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/>`+
		`<p:txBody><a:bodyPr/><a:lstStyle/>`+body.String()+`</p:txBody></p:sp>`+
		`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:notes>`)
	d.part(fmt.Sprintf("ppt/notesSlides/_rels/notesSlide%d.xml.rels", n), pptRels(
		[3]string{"rId1", "notesMaster", "../notesMasters/notesMaster1.xml"},
		[3]string{"rId2", "slide", fmt.Sprintf("../slides/slide%d.xml", n)}))
}

// Save writes the parts that list the slides and moves the file into place.
func (d *pictureDeck) Save() error {
	slides := len(d.hasNotes)
	if slides == 0 {
		return fmt.Errorf("a presentation needs at least one slide")
	}
	notes := false
	noteCount := 0
	for _, h := range d.hasNotes {
		if h {
			notes = true
			noteCount++
		}
	}
	var p strings.Builder
	p.WriteString(`<p:presentation ` + pNamespaces + ` saveSubsetFonts="1"><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>`)
	if notes {
		p.WriteString(`<p:notesMasterIdLst><p:notesMasterId r:id="rId6"/></p:notesMasterIdLst>`)
	}
	p.WriteString("<p:sldIdLst>")
	for i := 0; i < slides; i++ {
		fmt.Fprintf(&p, `<p:sldId id="%d" r:id="rId%d"/>`, 256+i, 7+i)
	}
	fmt.Fprintf(&p, `</p:sldIdLst><p:sldSz cx="%d" cy="%d"/><p:notesSz cx="%d" cy="%d"/></p:presentation>`, d.SlideWidth, d.SlideHeight, notesWidth, notesHeight)
	d.part("ppt/presentation.xml", p.String())
	rels := [][3]string{{"rId1", "slideMaster", "slideMasters/slideMaster1.xml"}, {"rId2", "theme", "theme/theme1.xml"},
		{"rId3", "presProps", "presProps.xml"}, {"rId4", "viewProps", "viewProps.xml"}, {"rId5", "tableStyles", "tableStyles.xml"}}
	if notes {
		rels = append(rels, [3]string{"rId6", "notesMaster", "notesMasters/notesMaster1.xml"})
	}
	for i := 0; i < slides; i++ {
		rels = append(rels, [3]string{fmt.Sprintf("rId%d", 7+i), "slide", fmt.Sprintf("slides/slide%d.xml", i+1)})
	}
	d.part("ppt/_rels/presentation.xml.rels", pptRels(rels...))
	d.part("ppt/slideMasters/slideMaster1.xml", slideMaster())
	d.part("ppt/slideMasters/_rels/slideMaster1.xml.rels", pptRels([3]string{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"}, [3]string{"rId2", "theme", "../theme/theme1.xml"}))
	d.part("ppt/slideLayouts/slideLayout1.xml", `<p:sldLayout `+pNamespaces+` type="blank" preserve="1"><p:cSld name="Blank">`+treeStart+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`)
	d.part("ppt/slideLayouts/_rels/slideLayout1.xml.rels", pptRels([3]string{"rId1", "slideMaster", "../slideMasters/slideMaster1.xml"}))
	d.part("ppt/theme/theme1.xml", pptTheme())
	if notes {
		d.part("ppt/notesMasters/notesMaster1.xml", d.notesMaster())
		// A master of its own needs a theme of its own.
		d.part("ppt/notesMasters/_rels/notesMaster1.xml.rels", pptRels([3]string{"rId1", "theme", "../theme/theme2.xml"}))
		d.part("ppt/theme/theme2.xml", pptTheme())
	}
	d.part("ppt/presProps.xml", `<p:presentationPr `+pNamespaces+`/>`)
	d.part("ppt/viewProps.xml", `<p:viewPr `+pNamespaces+`><p:normalViewPr><p:restoredLeft sz="15620"/><p:restoredTop sz="94660"/></p:normalViewPr><p:gridSpacing cx="76200" cy="76200"/></p:viewPr>`)
	d.part("ppt/tableStyles.xml", `<a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" def="{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}"/>`)
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	d.part("docProps/core.xml", `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`+
		`<dcterms:created xsi:type="dcterms:W3CDTF">`+now+`</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">`+now+`</dcterms:modified></cp:coreProperties>`)
	d.part("docProps/app.xml", fmt.Sprintf(`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Filee</Application><Slides>%d</Slides><Notes>%d</Notes></Properties>`, slides, noteCount))
	d.part("_rels/.rels", pptRels([3]string{"rId1", "officeDocument", "ppt/presentation.xml"},
		[3]string{"rId2", "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties", "docProps/core.xml"},
		[3]string{"rId3", "extended-properties", "docProps/app.xml"}))
	d.part("[Content_Types].xml", d.contentTypes(slides, notes))
	err := d.err
	if cerr := d.z.Close(); err == nil {
		err = cerr
	}
	if cerr := d.f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(d.temp)
		return err
	}
	d.saved = true
	return os.Rename(d.temp, d.path)
}

// Close removes an unfinished file.
func (d *pictureDeck) Close() {
	if d.saved {
		return
	}
	d.z.Close()
	d.f.Close()
	os.Remove(d.temp)
}

func (d *pictureDeck) contentTypes(slides int, notes bool) string {
	var b strings.Builder
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/><Default Extension="jpeg" ContentType="image/jpeg"/>`)
	o := func(part, typ string) { fmt.Fprintf(&b, `<Override PartName="/%s" ContentType="%s"/>`, part, typ) }
	o("ppt/presentation.xml", pptxTypes+"presentation.main+xml")
	o("ppt/slideMasters/slideMaster1.xml", pptxTypes+"slideMaster+xml")
	o("ppt/slideLayouts/slideLayout1.xml", pptxTypes+"slideLayout+xml")
	for i := 1; i <= slides; i++ {
		o(fmt.Sprintf("ppt/slides/slide%d.xml", i), pptxTypes+"slide+xml")
	}
	o("ppt/theme/theme1.xml", "application/vnd.openxmlformats-officedocument.theme+xml")
	if notes {
		o("ppt/notesMasters/notesMaster1.xml", pptxTypes+"notesMaster+xml")
		o("ppt/theme/theme2.xml", "application/vnd.openxmlformats-officedocument.theme+xml")
		for i := 1; i <= slides; i++ {
			if d.hasNotes[i-1] {
				o(fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", i), pptxTypes+"notesSlide+xml")
			}
		}
	}
	o("ppt/presProps.xml", pptxTypes+"presProps+xml")
	o("ppt/viewProps.xml", pptxTypes+"viewProps+xml")
	o("ppt/tableStyles.xml", pptxTypes+"tableStyles+xml")
	o("docProps/core.xml", "application/vnd.openxmlformats-package.core-properties+xml")
	o("docProps/app.xml", "application/vnd.openxmlformats-officedocument.extended-properties+xml")
	b.WriteString("</Types>")
	return b.String()
}

func slideMaster() string {
	level := func(size, font, align string) string {
		return `<a:lvl1pPr` + align + `><a:defRPr sz="` + size + `"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill>` +
			`<a:latin typeface="+` + font + `-lt"/><a:ea typeface="+` + font + `-ea"/><a:cs typeface="+` + font + `-cs"/></a:defRPr></a:lvl1pPr>`
	}
	return `<p:sldMaster ` + pNamespaces + `><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>` +
		treeStart + `</p:spTree></p:cSld>` + colorMap +
		`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>` +
		`<p:txStyles><p:titleStyle>` + level("4400", "mj", ` algn="ctr"`) + `</p:titleStyle>` +
		`<p:bodyStyle>` + level("2800", "mn", "") + `</p:bodyStyle><p:otherStyle>` + level("1800", "mn", "") + `</p:otherStyle></p:txStyles></p:sldMaster>`
}

// notesMaster: the slide's picture in the upper part, shaped like the slide, the notes below.
func (d *pictureDeck) notesMaster() string {
	const boxLeft, boxTop, boxWidth, boxHeight = 685800, 457200, 5486400, 3657600
	scale := min(float64(boxWidth)/float64(d.SlideWidth), float64(boxHeight)/float64(d.SlideHeight))
	cx := int64(math.Round(float64(d.SlideWidth) * scale))
	cy := int64(math.Round(float64(d.SlideHeight) * scale))
	x := boxLeft + (boxWidth-cx)/2
	y := boxTop + (boxHeight-cy)/2
	return `<p:notesMaster ` + pNamespaces + `><p:cSld>` + treeStart +
		`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image Placeholder 1"/><p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/></p:cNvSpPr>` +
		`<p:nvPr><p:ph type="sldImg" idx="2"/></p:nvPr></p:nvSpPr>` +
		fmt.Sprintf(`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom>`, x, y, cx, cy) +
		`<a:noFill/><a:ln w="12700"><a:solidFill><a:prstClr val="black"/></a:solidFill></a:ln></p:spPr></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes Placeholder 2"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>` +
		`<p:nvPr><p:ph type="body" sz="quarter" idx="1"/></p:nvPr></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="685800" y="4343400"/><a:ext cx="5486400" cy="4114800"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody><a:bodyPr vert="horz" lIns="91440" tIns="45720" rIns="91440" bIns="45720" rtlCol="0"/><a:lstStyle/><a:p/></p:txBody></p:sp>` +
		`</p:spTree></p:cSld>` + colorMap +
		`<p:notesStyle><a:lvl1pPr marL="0" algn="l" defTabSz="914400" rtl="0" eaLnBrk="1" latinLnBrk="0" hangingPunct="1">` +
		`<a:defRPr sz="1200" kern="1200"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill>` +
		`<a:latin typeface="+mn-lt"/><a:ea typeface="+mn-ea"/><a:cs typeface="+mn-cs"/></a:defRPr></a:lvl1pPr></p:notesStyle></p:notesMaster>`
}

// pptTheme: Office's default colours and fonts; three plain entries per style list.
func pptTheme() string {
	c := func(name, value string) string {
		return `<a:` + name + `><a:srgbClr val="` + value + `"/></a:` + name + `>`
	}
	const fill = `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	lines := ""
	for _, w := range []string{"6350", "12700", "19050"} {
		lines += `<a:ln w="` + w + `">` + fill + `</a:ln>`
	}
	return `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Office Theme"><a:themeElements><a:clrScheme name="Office">` +
		`<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
		c("dk2", "44546A") + c("lt2", "E7E6E6") + c("accent1", "4472C4") + c("accent2", "ED7D31") + c("accent3", "A5A5A5") +
		c("accent4", "FFC000") + c("accent5", "5B9BD5") + c("accent6", "70AD47") + c("hlink", "0563C1") + c("folHlink", "954F72") + `</a:clrScheme>` +
		`<a:fontScheme name="Office"><a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>` +
		`<a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>` +
		`<a:fmtScheme name="Office"><a:fillStyleLst>` + fill + fill + fill + `</a:fillStyleLst><a:lnStyleLst>` + lines + `</a:lnStyleLst>` +
		`<a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>` +
		`<a:bgFillStyleLst>` + fill + fill + fill + `</a:bgFillStyleLst></a:fmtScheme></a:themeElements><a:objectDefaults/><a:extraClrSchemeLst/></a:theme>`
}
