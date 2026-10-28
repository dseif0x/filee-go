package ofd

import (
	"bytes"
	"strings"

	"github.com/dseif0x/filee-go/internal/xdom"
)

// Seals: an e-invoice's or a contract's seal is stored in its signature
// (Signs/Sign_n/SignedValue.dat, GM/T 0031 SES_Signature, DER), not drawn on
// the page. The seal picture inside it — PNG, JPEG, GIF, BMP or a small OFD
// package of its own — is shown where the signature's StampAnnot puts it,
// floating over the text like the stamp on paper. The signature itself is
// not checked.

const (
	maxSignatures       = 64
	maxStamps           = 16
	maxSignedValueBytes = 4 << 20
	maxSealPackageBytes = 16 << 20
	sealScale           = 12.0 // pixels per mm (about 300 dpi)
	maxSealSize         = 200.0
	maxSealObjects      = 20_000
)

// addSeals adds the seal of each signature at its StampAnnot's boundary on the page it names.
func (r *reader) addSeals(ofd *xdom.Node, pages []*pageContent) {
	listPath, ok := resolve("", child(child(ofd, "DocBody"), "Signatures").Value())
	if !ok {
		return
	}
	list := r.optional(listPath)
	if list == nil {
		return
	}
	byID := map[string]*pageContent{}
	for _, p := range pages {
		if p.id != "" {
			if _, dup := byID[p.id]; !dup {
				byID[p.id] = p
			}
		}
	}
	for i, sig := range children(list, "Signature") {
		if i >= maxSignatures {
			break
		}
		path, ok := resolve(dirName(listPath), attr(sig, "BaseLoc"))
		if !ok {
			continue
		}
		root := r.optional(path)
		if root == nil {
			continue
		}
		type stamp struct {
			page *pageContent
			box  [4]float64
		}
		var stamps []stamp
		for k, s := range children(child(root, "SignedInfo"), "StampAnnot") {
			if k >= maxStamps {
				break
			}
			p := byID[attr(s, "PageRef")]
			b, ok := box(attr(s, "Boundary"))
			if p != nil && ok && b[2] >= 1 && b[2] <= maxSealSize && b[3] >= 1 && b[3] <= maxSealSize {
				stamps = append(stamps, stamp{p, b})
			}
		}
		if len(stamps) == 0 {
			continue
		}
		valuePath, ok := resolve(dirName(path), child(root, "SignedValue").Value())
		if !ok {
			continue
		}
		signed, err := r.pkg.readPart(valuePath, maxSignedValueBytes)
		if err != nil || signed == nil {
			continue
		}
		file := r.sealFile(signed)
		if file == "" {
			continue
		}
		for _, s := range stamps {
			x, y, w, h := s.box[0], s.box[1], s.box[2], s.box[3]
			if x < s.page.width && y < s.page.height && x+w > 0 && y+h > 0 {
				s.page.pictures = append(s.page.pictures, picture{file: file, left: x, top: y, width: w, height: h})
			}
		}
	}
}

// optional is an XML part that may be missing or damaged (nil then).
func (r *reader) optional(name string) *xdom.Node {
	n, err := r.pkg.loadXML(name)
	if err != nil {
		return nil
	}
	return n
}

// sealFile is the seal picture of a signature as a file in the media folder, "" when there is none to show.
func (r *reader) sealFile(signed []byte) (file string) {
	data := sealPicture(signed)
	if data == nil {
		return ""
	}
	defer func() {
		// A damaged seal is left out; the page still converts.
		if rec := recover(); rec != nil {
			if _, ok := rec.(failure); !ok {
				panic(rec)
			}
			file = ""
		}
	}()
	if bytes.HasPrefix(data, []byte("PK")) {
		return r.drawSeal(data)
	}
	return r.save(data)
}

// sealPicture finds the picture data of a seal (SES_ESPictrueInfo: SEQUENCE
// { type IA5String, data OCTET STRING, width, height }) in a DER signature.
func sealPicture(der []byte) []byte {
	budget := 20_000
	header := func(pos, end int) (tag, body, length int, ok bool) {
		if pos+2 > end {
			return
		}
		tag = int(der[pos])
		if tag&0x1F == 0x1F {
			return // multi-byte tags do not occur in a signature
		}
		first := int(der[pos+1])
		body = pos + 2
		if first < 0x80 {
			length = first
		} else {
			count := first & 0x7F
			if count == 0 || count > 4 || body+count > end {
				return
			}
			v := 0
			for i := 0; i < count; i++ {
				v = v<<8 | int(der[body+i])
			}
			body += count
			if v < 0 || v > 1<<31-1 {
				return
			}
			length = v
		}
		ok = length <= end-body
		return
	}
	pictureInfo := func(start, end int) []byte {
		tt, tb, tl, ok := header(start, end)
		if !ok || (tt != 0x16 && tt != 0x0C && tt != 0x13) || tl < 2 || tl > 8 {
			return nil
		}
		switch strings.ToLower(strings.TrimSpace(string(der[tb : tb+tl]))) {
		case "ofd", "png", "jpg", "jpeg", "gif", "bmp", "tif", "tiff":
		default:
			return nil
		}
		dt, db, dl, ok := header(tb+tl, end)
		if !ok || dt != 0x04 || dl == 0 {
			return nil
		}
		return der[db : db+dl]
	}
	var find func(start, end, depth int) []byte
	find = func(start, end, depth int) []byte {
		for pos := start; pos < end && budget > 0; budget-- {
			tag, body, length, ok := header(pos, end)
			if !ok {
				return nil
			}
			if tag == 0x30 || tag >= 0xA0 && tag <= 0xA3 {
				if tag == 0x30 {
					if f := pictureInfo(body, body+length); f != nil {
						return f
					}
				}
				if depth < 12 {
					if f := find(body, body+length, depth+1); f != nil {
						return f
					}
				}
			}
			pos = body + length
		}
		return nil
	}
	return find(0, len(der), 0)
}
