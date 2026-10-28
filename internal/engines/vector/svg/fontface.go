package svg

import (
	stdctx "context"
	"regexp"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/fonts"
	"github.com/dseif0x/filee-go/internal/engines/vector/gfx"
)

// @font-face rules with embedded (data:) or neighbouring font files make the
// drawing's own fonts available to its text, as browsers do.

var fontFaceRule = regexp.MustCompile(`(?is)@font-face\s*\{([^}]*)\}`)

type fontFace struct {
	family string
	srcs   []string
}

func fontFaces(css string) []fontFace {
	var out []fontFace
	for _, m := range fontFaceRule.FindAllStringSubmatch(cssComment.ReplaceAllString(css, ""), -1) {
		var ff fontFace
		for _, d := range parseDeclarations(m[1]) {
			switch d.prop {
			case "font-family":
				if f := parseFamilies(d.value); len(f) > 0 {
					ff.family = f[0]
				}
			case "src":
				for _, part := range splitOutsideParens(d.value, ',') {
					part = strings.TrimSpace(part)
					if strings.HasPrefix(strings.ToLower(part), "url(") {
						end := strings.IndexByte(part, ')')
						if end > 4 {
							ff.srcs = append(ff.srcs, strings.Trim(strings.TrimSpace(part[4:end]), `"'`))
						}
					}
				}
			}
		}
		if ff.family != "" && len(ff.srcs) > 0 {
			out = append(out, ff)
		}
	}
	return out
}

// loadFontFaces gives the document a font set with its @font-face fonts.
func (d *Document) loadFontFaces() {
	var css strings.Builder
	var walk func(*Node)
	walk = func(n *Node) {
		if n.Name == "style" {
			css.WriteString(textContent(n))
			css.WriteByte('\n')
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(d.root)
	faces := fontFaces(css.String())
	if len(faces) == 0 {
		return
	}
	var set *gfx.Fonts
	for _, ff := range faces {
		for _, src := range ff.srcs {
			data, _, ok := d.loadHref(src)
			if !ok || len(data) < 12 {
				continue
			}
			// WOFF, WOFF2 and EOT become plain OpenType first.
			if f, err := fonts.Load(data); err == nil {
				if otf, err := f.Save(stdctx.Background(), "otf"); err == nil {
					data = otf
				}
			}
			if set == nil {
				set = gfx.NewFonts(gfx.SystemFonts)
			}
			if set.AddFontData(data, ff.family) == nil {
				break
			}
		}
	}
	if set != nil {
		d.Fonts = set
	}
}
