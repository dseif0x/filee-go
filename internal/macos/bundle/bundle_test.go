package bundle

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestPlists(t *testing.T) {
	info := InfoPlist("1.2.3-dev", "Filee")
	for _, want := range []string{"<key>LSUIElement</key>", "<string>1.2.3</string>", "<string>convertFiles</string>",
		"<key>NSAppleEventsUsageDescription</key>", "<string>com.filee.app</string>", "<dict/>"} {
		if !strings.Contains(info, want) {
			t.Errorf("Info.plist lacks %s", want)
		}
	}
	agent := LaunchAgent("/Applications/Filee.app/Contents/MacOS/Filee")
	if !strings.Contains(agent, "<string>--background</string>") || !strings.Contains(agent, "<key>RunAtLoad</key>\n\t<true/>") {
		t.Errorf("launch agent:\n%s", agent)
	}
	if ServiceStatusKey() != `"com.filee.app - Convert with Filee - convertFiles"` {
		t.Error(ServiceStatusKey())
	}
	if !strings.Contains(ServiceStatus(false), `"enabled_context_menu" = 0;`) {
		t.Error(ServiceStatus(false))
	}
	if s := ServicesStrings("Filee로 변환"); !strings.Contains(s, `"Convert with Filee" = "Filee로 변환";`) {
		t.Error(s)
	}
}

func TestICNS(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(10, 10, color.Black)
	b, err := ICNS(img)
	if err != nil {
		t.Fatal(err)
	}
	if string(b[:4]) != "icns" || int(binary.BigEndian.Uint32(b[4:8])) != len(b) {
		t.Fatal("bad header")
	}
	var kinds []string
	for off := 8; off < len(b); {
		n := int(binary.BigEndian.Uint32(b[off+4:]))
		kinds = append(kinds, string(b[off:off+4]))
		if !bytes.HasPrefix(b[off+8:], []byte("\x89PNG")) {
			t.Fatalf("%s isn't PNG", b[off:off+4])
		}
		off += n
	}
	if strings.Join(kinds, ",") != "icp4,icp5,ic11,ic12,ic07,ic13,ic08,ic14,ic09" {
		t.Errorf("kinds %v", kinds)
	}
}

// thin is a minimal 64-bit Mach-O header with no load commands.
func thin(cpu macho.Cpu) []byte {
	var b bytes.Buffer
	for _, v := range []uint32{macho.Magic64, uint32(cpu), 0, uint32(macho.TypeExec), 0, 0, 0, 0} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("payload")
	return b.Bytes()
}

func TestUniversal(t *testing.T) {
	arm, amd := thin(macho.CpuArm64), thin(macho.CpuAmd64)
	fat, err := Universal(arm, amd)
	if err != nil {
		t.Fatal(err)
	}
	f, err := macho.NewFatFile(bytes.NewReader(fat))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Arches) != 2 || f.Arches[0].Cpu != macho.CpuArm64 || f.Arches[1].Cpu != macho.CpuAmd64 {
		t.Fatalf("arches %+v", f.Arches)
	}
	for i, want := range [][]byte{arm, amd} {
		a := f.Arches[i]
		if a.Offset%(1<<14) != 0 || !bytes.Equal(fat[a.Offset:a.Offset+a.Size], want) {
			t.Errorf("slice %d misplaced", i)
		}
	}
}
