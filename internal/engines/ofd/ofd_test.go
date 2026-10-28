package ofd

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/engines/docxwriter"
	"github.com/dseif0x/filee-go/internal/testutil"
)

func pngBytes(c color.Color) string {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.String()
}

// der wraps the seal picture like SES_ESPictrueInfo inside a signature sequence.
func der(picture string) string {
	tlv := func(tag byte, body string) string {
		n := len(body)
		switch {
		case n < 0x80:
			return string([]byte{tag, byte(n)}) + body
		case n < 0x100:
			return string([]byte{tag, 0x81, byte(n)}) + body
		default:
			return string([]byte{tag, 0x82, byte(n >> 8), byte(n)}) + body
		}
	}
	info := tlv(0x30, tlv(0x16, "png")+tlv(0x04, picture)+tlv(0x02, "\x28")+tlv(0x02, "\x28"))
	return tlv(0x30, tlv(0x30, tlv(0x02, "\x04")+info)+tlv(0x04, "sig"))
}

const ns = `xmlns:ofd="http://www.ofdspec.org/2016"`

func fixture(t *testing.T) string {
	content := `<ofd:Page ` + ns + `><ofd:Area><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:Area><ofd:Content><ofd:Layer DrawParam="2">` +
		`<ofd:TextObject ID="10" Boundary="20 20 170 10" Font="1" Size="6"><ofd:TextCode X="0" Y="6" DeltaX="g 3 6">电子发票</ofd:TextCode></ofd:TextObject>` +
		`<ofd:TextObject ID="11" Boundary="20 40 100 6" Font="1" Size="4"><ofd:TextCode X="0" Y="4">Hello world</ofd:TextCode></ofd:TextObject>` +
		// A 2×2 grid at x 20..120, y 60..80.
		`<ofd:PathObject ID="20" Boundary="0 0 210 297" LineWidth="0.25"><ofd:AbbreviatedData>M 20 60 L 120 60 M 20 70 L 120 70 M 20 80 L 120 80 M 20 60 L 20 80 M 70 60 L 70 80 M 120 60 L 120 80</ofd:AbbreviatedData></ofd:PathObject>` +
		`<ofd:TextObject ID="30" Boundary="22 61 40 6" Font="1" Size="4"><ofd:TextCode X="0" Y="4">名称</ofd:TextCode></ofd:TextObject>` +
		`<ofd:TextObject ID="31" Boundary="72 61 40 6" Font="1" Size="4"><ofd:TextCode X="0" Y="4">金额</ofd:TextCode></ofd:TextObject>` +
		`<ofd:TextObject ID="32" Boundary="22 71 40 6" Font="1" Size="4"><ofd:TextCode X="0" Y="4">笔</ofd:TextCode></ofd:TextObject>` +
		`<ofd:TextObject ID="33" Boundary="72 71 40 6" Font="1" Size="4"><ofd:TextCode X="0" Y="4">12.50</ofd:TextCode></ofd:TextObject>` +
		`<ofd:ImageObject ID="40" Boundary="80 120 30 30" CTM="30 0 0 30 0 0" ResourceID="5"/>` +
		`</ofd:Layer></ofd:Content></ofd:Page>`
	dir := t.TempDir()
	return testutil.Zip(t, filepath.Join(dir, "invoice.ofd"),
		"OFD.xml", `<ofd:OFD `+ns+` Version="1.1"><ofd:DocBody><ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot><ofd:Signatures>Doc_0/Signs/Signatures.xml</ofd:Signatures></ofd:DocBody></ofd:OFD>`,
		"Doc_0/Document.xml", `<ofd:Document `+ns+`><ofd:CommonData><ofd:PageArea><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:PageArea><ofd:PublicRes>PublicRes.xml</ofd:PublicRes></ofd:CommonData><ofd:Pages><ofd:Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></ofd:Pages></ofd:Document>`,
		"Doc_0/PublicRes.xml", `<ofd:Res `+ns+` BaseLoc="Res"><ofd:Fonts><ofd:Font ID="1" FontName="宋体"/></ofd:Fonts><ofd:DrawParams><ofd:DrawParam ID="2"><ofd:FillColor Value="128 0 0"/></ofd:DrawParam></ofd:DrawParams><ofd:MultiMedias><ofd:MultiMedia ID="5" Type="Image"><ofd:MediaFile>qr.png</ofd:MediaFile></ofd:MultiMedia></ofd:MultiMedias></ofd:Res>`,
		"Doc_0/Res/qr.png", pngBytes(color.Black),
		"Doc_0/Pages/Page_0/Content.xml", content,
		"Doc_0/Signs/Signatures.xml", `<ofd:Signatures `+ns+`><ofd:Signature ID="1" BaseLoc="Sign_0/Signature.xml"/></ofd:Signatures>`,
		"Doc_0/Signs/Sign_0/Signature.xml", `<ofd:Signature `+ns+`><ofd:SignedInfo><ofd:StampAnnot ID="1" PageRef="1" Boundary="150 15 40 40"/></ofd:SignedInfo><ofd:SignedValue>SignedValue.dat</ofd:SignedValue></ofd:Signature>`,
		"Doc_0/Signs/Sign_0/SignedValue.dat", der(pngBytes(color.RGBA{255, 0, 0, 255})),
	)
}

func TestRead(t *testing.T) {
	d, err := Read(fixture(t), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sections) != 1 || d.Sections[0].Page.Width != hwp(210) {
		t.Fatalf("sections %+v", d.Sections)
	}
	var texts []string
	var tables, images, floating int
	for _, b := range d.Sections[0].Blocks {
		switch b := b.(type) {
		case *doc.Paragraph:
			if s := doc.PlainText(b.Inlines); s != "" {
				texts = append(texts, s)
			}
			for _, in := range b.Inlines {
				if im, ok := in.(*doc.Image); ok {
					if im.Anchor != nil {
						floating++
					} else {
						images++
					}
				}
			}
		case *doc.Table:
			tables++
			if len(b.Rows) != 2 || len(b.Rows[1].Cells) != 2 || doc.PlainText(b.Rows[1].Cells[1].Blocks[0].(*doc.Paragraph).Inlines) != "12.50" {
				t.Errorf("table %+v", b)
			}
		}
	}
	all := strings.Join(texts, "|")
	if !strings.Contains(all, "电子发票") || !strings.Contains(all, "Hello world") || strings.Contains(all, "名称") {
		t.Errorf("texts %q", all)
	}
	if tables != 1 || images != 1 || floating != 1 {
		t.Errorf("tables %d images %d floating seals %d", tables, images, floating)
	}
	var first *doc.Text
	for _, in := range doc.Inlines(d.Sections[0].Blocks) {
		if tx, ok := in.(*doc.Text); ok && strings.Contains(tx.Text, "电子") {
			first = tx
		}
	}
	if first == nil || doc.Val(first.Format.Font, "") != "SimSun" || doc.Val(first.Format.Color, "") != "#800000" {
		t.Errorf("format %+v", first)
	}
	// And on to DOCX.
	out := filepath.Join(t.TempDir(), "x.docx")
	if err := docxwriter.Write(d, out); err != nil {
		t.Fatal(err)
	}
	if z := testutil.ReadZip(t, out); !strings.Contains(z["word/document.xml"], "电子发票") || z["word/media/image1.png"] == "" {
		t.Error("docx")
	}
}

func TestRefusesEncrypted(t *testing.T) {
	p := testutil.Zip(t, filepath.Join(t.TempDir(), "e.ofd"), "OFD.xml", `<ofd:OFD `+ns+`><ofd:DocBody><ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`, "Encryptions.xml", "<x/>")
	if _, err := Read(p, t.TempDir()); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("err %v", err)
	}
}

func TestResolve(t *testing.T) {
	if p, ok := resolve("Doc_0/Pages", "../Res/a.png"); !ok || p != "Doc_0/Res/a.png" {
		t.Fatal(p)
	}
	if _, ok := resolve("", "../../etc/passwd"); ok {
		t.Fatal("escaped")
	}
	if _, ok := resolve("", "C:/x"); ok {
		t.Fatal("drive")
	}
}

func TestSealOfd(t *testing.T) {
	seal := testutil.Zip(t, filepath.Join(t.TempDir(), "seal.ofd"),
		"OFD.xml", `<ofd:OFD `+ns+`><ofd:DocBody><ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`,
		"Doc_0/Document.xml", `<ofd:Document `+ns+`><ofd:CommonData><ofd:PageArea><ofd:PhysicalBox>0 0 40 30</ofd:PhysicalBox></ofd:PageArea></ofd:CommonData><ofd:Pages><ofd:Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></ofd:Pages></ofd:Document>`,
		"Doc_0/Pages/Page_0/Content.xml", `<ofd:Page `+ns+`><ofd:Content><ofd:Layer><ofd:PathObject ID="1" Boundary="0 0 40 30" LineWidth="1"><ofd:StrokeColor Value="255 0 0"/><ofd:AbbreviatedData>M 2 15 A 18 13 0 1 1 38 15 A 18 13 0 1 1 2 15 C</ofd:AbbreviatedData></ofd:PathObject></ofd:Layer></ofd:Content></ofd:Page>`)
	b, _ := readFile(seal)
	r := &reader{media: t.TempDir()}
	file := r.drawSeal(b)
	if file == "" {
		t.Fatal("no seal")
	}
	img, err := decodeFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// The ellipse's left edge is red, its centre transparent.
	if _, _, _, a := img.At(int(2*sealScale), int(15*sealScale)).RGBA(); a == 0 {
		t.Error("edge not drawn")
	}
	if _, _, _, a := img.At(int(20*sealScale), int(15*sealScale)).RGBA(); a != 0 {
		t.Error("centre drawn")
	}
}
