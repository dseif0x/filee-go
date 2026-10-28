package model3d

// Stanford PLY: ASCII, binary little endian and binary big endian. Unitless
// (read as millimetres), Z up like Blender treats it. Read: vertex x/y/z,
// normals, texture coordinates (s/t, u/v, texture_u/texture_v), colours
// (red/green/blue/alpha in any type), faces as index lists (polygons are
// triangulated) with optional per-corner texture coordinates and face
// colours, and a "comment TextureFile" picture next to the file; other
// elements are skipped. Written: binary little endian, one mesh; PLY has no
// materials, so material colours become vertex colours.

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type plyType int

const (
	plyInt8 plyType = iota
	plyUint8
	plyInt16
	plyUint16
	plyInt32
	plyUint32
	plyFloat32
	plyFloat64
)

type plyProperty struct {
	name      string
	typ       plyType
	countType plyType
	list      bool
}

type plyElement struct {
	name       string
	count      int64
	properties []plyProperty
}

type plyValues interface {
	read(t plyType) (float64, error)
}

var errPlyShort = errors.New("The PLY file is cut short: it ends before all vertices and faces were read.")

type splitKey struct {
	vertex int
	uv     vec2
	color  vec4
}

func readPly(ctx context.Context, path string, progress func(float64)) (*Model, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<16)
	encoding, elements, textureFile, err := readPlyHeader(r)
	if err != nil {
		return nil, err
	}
	var values plyValues
	switch encoding {
	case "ascii":
		values = &plyASCII{r: r}
	case "binary_big_endian":
		values = &plyBinary{r: r, order: binary.BigEndian}
	default:
		values = &plyBinary{r: r, order: binary.LittleEndian}
	}

	part := newPart(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	model := &Model{Parts: []*Part{part}}
	vertexCount := 0
	split := map[splitKey]int{}
	claimed := map[int]bool{}
	var total int64 = 0
	for _, e := range elements {
		total += e.count
	}
	total = max(1, total)
	var done int64
	corners := make([]int, 0, 256)
	var cornerValues, cornerUvs []float64

	for _, element := range elements {
		switch element.name {
		case "vertex":
			vertexCount = int(min(element.count, math.MaxInt32/4))
			if err := readPlyVertices(ctx, element, values, part, progress, done, total); err != nil {
				return nil, err
			}
		case "face":
			indexProperty, uvProperty := -1, -1
			colorRoles := make([]int, len(element.properties))
			hasFaceColor := false
			for i, p := range element.properties {
				if indexProperty < 0 && p.list && (p.name == "vertex_indices" || p.name == "vertex_index") {
					indexProperty = i
				}
				if uvProperty < 0 && p.list && p.name == "texcoord" {
					uvProperty = i
				}
				colorRoles[i] = plyColorChannel(p.name)
				if colorRoles[i] >= 0 {
					hasFaceColor = true
				}
			}
			if indexProperty < 0 {
				return nil, errors.New("The PLY file's faces have no vertex_indices list.")
			}
			for fi := int64(0); fi < element.count; fi++ {
				if fi&0xFFFF == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					if progress != nil {
						progress(float64(done+fi) / float64(total))
					}
				}
				cornerValues, cornerUvs = cornerValues[:0], cornerUvs[:0]
				color := [4]float64{1, 1, 1, 1}
				for pi, property := range element.properties {
					if property.list {
						c, err := values.read(property.countType)
						if err != nil {
							return nil, err
						}
						count := int64(c)
						if count < 0 || count > 1<<20 {
							return nil, errors.New("The PLY file has a face with an invalid number of corners.")
						}
						for i := int64(0); i < count; i++ {
							v, err := values.read(property.typ)
							if err != nil {
								return nil, err
							}
							if pi == indexProperty {
								cornerValues = append(cornerValues, v)
							} else if pi == uvProperty {
								cornerUvs = append(cornerUvs, v)
							}
						}
						continue
					}
					v, err := values.read(property.typ)
					if err != nil {
						return nil, err
					}
					if colorRoles[pi] >= 0 {
						color[colorRoles[pi]] = plyColorValue(v, property.typ)
					}
				}
				n := min(len(cornerValues), 256)
				wedgeUvs := n > 0 && len(cornerUvs) >= n*2
				corners = corners[:0]
				for i := 0; i < n; i++ {
					index := int64(cornerValues[i])
					if index < 0 || index >= int64(vertexCount) {
						return nil, fmt.Errorf("A face of the PLY file refers to vertex %d, but there are only %d vertices.", index, vertexCount)
					}
					c := int(index)
					if wedgeUvs || hasFaceColor {
						uv := part.TexCoord(c)
						if wedgeUvs {
							uv = vec2{float32(cornerUvs[i*2]), float32(cornerUvs[i*2+1])}
						}
						rgba := part.Color(c)
						if hasFaceColor {
							rgba = vec4{float32(color[0]), float32(color[1]), float32(color[2]), float32(color[3])}
						}
						c = plySplit(part, split, claimed, c, uv, rgba, wedgeUvs, hasFaceColor)
					}
					corners = append(corners, c)
				}
				addPolygon(part, corners)
			}
		default:
			// Edges, materials, ...: read past them.
			for i := int64(0); i < element.count; i++ {
				for _, property := range element.properties {
					if property.list {
						c, err := values.read(property.countType)
						if err != nil {
							return nil, err
						}
						for k := int64(0); k < int64(c); k++ {
							if _, err := values.read(property.typ); err != nil {
								return nil, err
							}
						}
					} else if _, err := values.read(property.typ); err != nil {
						return nil, err
					}
				}
			}
		}
		done += element.count
	}

	if textureFile != "" {
		abs, _ := filepath.Abs(path)
		if texture := resolveReference(filepath.Dir(abs), textureFile); texture != "" {
			if data, err := os.ReadFile(texture); err == nil {
				ext := filepath.Ext(texture)
				name := strings.TrimSuffix(filepath.Base(texture), ext)
				mat := newMaterial(name)
				mat.BaseColorTexture = &Texture{Data: data, Extension: sniff(data, ext), Name: name}
				model.Materials = append(model.Materials, mat)
				part.Material = 0
			}
		}
	}
	part.Transform(zUpToYUp)
	return model, nil
}

// plySplit returns a copy of vertex with the corner's own texture coordinate and/or colour.
func plySplit(p *Part, split map[splitKey]int, claimed map[int]bool, vertex int, uv vec2, color vec4, setUv, setColor bool) int {
	if setUv && p.TexCoords == nil {
		p.TexCoords = make([]float32, p.VertexCount()*2)
	}
	if setColor && p.Colors == nil {
		p.Colors = make([]float32, p.VertexCount()*4)
		for i := range p.Colors {
			p.Colors[i] = 1
		}
	}
	if !claimed[vertex] {
		claimed[vertex] = true
		// The first face to use a vertex gives it its texture coordinate and colour.
		if p.TexCoords != nil {
			p.TexCoords[vertex*2], p.TexCoords[vertex*2+1] = uv.X, uv.Y
		}
		if p.Colors != nil {
			p.Colors[vertex*4], p.Colors[vertex*4+1], p.Colors[vertex*4+2], p.Colors[vertex*4+3] = color.X, color.Y, color.Z, color.W
		}
		return vertex
	}
	if p.TexCoord(vertex) == uv && p.Color(vertex) == color {
		return vertex
	}
	key := splitKey{vertex, uv, color}
	if c, ok := split[key]; ok {
		return c
	}
	var normal *vec3
	if p.Normals != nil {
		n := p.Normal(vertex)
		normal = &n
	}
	c := p.AddVertex(p.Position(vertex), normal, &uv, &color)
	split[key] = c
	return c
}

func readPlyVertices(ctx context.Context, element plyElement, values plyValues, p *Part, progress func(float64), done, total int64) error {
	index := func(names ...string) int {
		for _, n := range names {
			for i, prop := range element.properties {
				if prop.name == n {
					return i
				}
			}
		}
		return -1
	}
	position := [3]int{index("x"), index("y"), index("z")}
	if position[0] < 0 || position[1] < 0 || position[2] < 0 {
		return errors.New("The PLY file's vertices have no x, y and z.")
	}
	normal := [3]int{index("nx"), index("ny"), index("nz")}
	uv := [2]int{index("s", "u", "texture_u", "texture_s"), index("t", "v", "texture_v", "texture_t")}
	colorRoles := make([]int, len(element.properties))
	hasColors := false
	for i, prop := range element.properties {
		colorRoles[i] = plyColorChannel(prop.name)
		if colorRoles[i] >= 0 {
			hasColors = true
		}
	}
	hasNormals := normal[0] >= 0 && normal[1] >= 0 && normal[2] >= 0
	hasUvs := uv[0] >= 0 && uv[1] >= 0
	if hasNormals {
		p.Normals = []float32{}
	}
	if hasUvs {
		p.TexCoords = []float32{}
	}
	if hasColors {
		p.Colors = []float32{}
	}
	count := int(min(element.count, math.MaxInt32/4))
	if count < 1<<24 {
		p.Positions = make([]float32, 0, count*3)
	}
	row := make([]float64, len(element.properties))
	for v := 0; v < count; v++ {
		if v&0xFFFF == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			if progress != nil {
				progress(float64(done+int64(v)) / float64(total))
			}
		}
		color := [4]float32{1, 1, 1, 1}
		for pi, prop := range element.properties {
			if prop.list {
				n, err := values.read(prop.countType)
				if err != nil {
					return err
				}
				for k := int64(0); k < int64(n); k++ {
					if _, err := values.read(prop.typ); err != nil {
						return err
					}
				}
				continue
			}
			value, err := values.read(prop.typ)
			if err != nil {
				return err
			}
			row[pi] = value
			if colorRoles[pi] >= 0 {
				color[colorRoles[pi]] = float32(plyColorValue(value, prop.typ))
			}
		}
		var np *vec3
		if hasNormals {
			np = &vec3{float32(row[normal[0]]), float32(row[normal[1]]), float32(row[normal[2]])}
		}
		var up *vec2
		if hasUvs {
			up = &vec2{float32(row[uv[0]]), float32(row[uv[1]])}
		}
		c := vec4{color[0], color[1], color[2], color[3]}
		p.AddVertex(vec3{float32(row[position[0]]), float32(row[position[1]]), float32(row[position[2]])}, np, up, &c)
	}
	return nil
}

// plyColorChannel is 0–3 for red, green, blue, alpha (also "diffuse_red" …), else -1.
func plyColorChannel(name string) int {
	switch name {
	case "red", "diffuse_red", "r":
		return 0
	case "green", "diffuse_green", "g":
		return 1
	case "blue", "diffuse_blue", "b":
		return 2
	case "alpha", "diffuse_alpha", "a":
		return 3
	}
	return -1
}

// plyColorValue is a colour channel as 0–1: integers are scaled by their range, floats are taken as they are.
func plyColorValue(v float64, t plyType) float64 {
	switch t {
	case plyUint8, plyInt8:
		return v / 255
	case plyUint16, plyInt16:
		return v / 65535
	case plyUint32, plyInt32:
		if v > 255 {
			return v / math.MaxUint32
		}
		return v / 255
	}
	if v > 1 {
		v /= 255
	}
	return max(0, min(1, v))
}

func readPlyHeader(r *bufio.Reader) (string, []plyElement, string, error) {
	magic, ok, err := plyHeaderLine(r)
	if err != nil {
		return "", nil, "", err
	}
	if !ok || magic != "ply" {
		return "", nil, "", errors.New(`The file is not a PLY file (it does not start with "ply").`)
	}
	encoding := ""
	texture := ""
	var elements []plyElement
	for {
		line, ok, err := plyHeaderLine(r)
		if err != nil {
			return "", nil, "", err
		}
		if !ok {
			return "", nil, "", errors.New("The PLY header has no end_header line.")
		}
		words := strings.Fields(line)
		if len(words) == 0 {
			continue
		}
		switch {
		case words[0] == "end_header":
			if encoding == "" {
				return "", nil, "", errors.New("The PLY header has no format line.")
			}
			return encoding, elements, texture, nil
		case words[0] == "format" && len(words) >= 2:
			switch words[1] {
			case "ascii", "binary_little_endian", "binary_big_endian":
				encoding = words[1]
			default:
				return "", nil, "", fmt.Errorf("Unknown PLY format %q.", words[1])
			}
		case words[0] == "comment" && len(words) >= 3 && strings.EqualFold(words[1], "TextureFile"):
			i := strings.Index(line, words[1])
			texture = strings.TrimSpace(line[i+len(words[1]):])
		case words[0] == "element" && len(words) >= 3:
			count, err := strconv.ParseInt(words[2], 10, 64)
			if err != nil || count < 0 {
				return "", nil, "", fmt.Errorf("The PLY element %q has an invalid count.", words[1])
			}
			elements = append(elements, plyElement{name: words[1], count: count})
		case words[0] == "property" && len(elements) > 0:
			e := &elements[len(elements)-1]
			if len(words) >= 5 && words[1] == "list" {
				ct, err := plyTypeOf(words[2])
				if err != nil {
					return "", nil, "", err
				}
				t, err := plyTypeOf(words[3])
				if err != nil {
					return "", nil, "", err
				}
				e.properties = append(e.properties, plyProperty{name: words[4], typ: t, countType: ct, list: true})
			} else if len(words) >= 3 {
				t, err := plyTypeOf(words[1])
				if err != nil {
					return "", nil, "", err
				}
				e.properties = append(e.properties, plyProperty{name: words[2], typ: t})
			} else {
				return "", nil, "", fmt.Errorf("Invalid PLY property line: %q.", line)
			}
		}
	}
}

func plyTypeOf(name string) (plyType, error) {
	switch name {
	case "char", "int8":
		return plyInt8, nil
	case "uchar", "uint8":
		return plyUint8, nil
	case "short", "int16":
		return plyInt16, nil
	case "ushort", "uint16":
		return plyUint16, nil
	case "int", "int32":
		return plyInt32, nil
	case "uint", "uint32":
		return plyUint32, nil
	case "float", "float32":
		return plyFloat32, nil
	case "double", "float64":
		return plyFloat64, nil
	}
	return 0, fmt.Errorf("Unknown PLY property type %q.", name)
}

// plyHeaderLine reads one header line byte by byte, so the reader stops exactly where the body starts.
func plyHeaderLine(r *bufio.Reader) (string, bool, error) {
	var b []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			if err == io.EOF {
				return string(b), len(b) > 0, nil
			}
			return "", false, err
		}
		if c == '\n' {
			return strings.TrimRight(string(b), "\r"), true, nil
		}
		if len(b) > 4096 {
			return "", false, errors.New("The file is not a PLY file (its header has an endless line).")
		}
		b = append(b, c)
	}
}

type plyBinary struct {
	r     *bufio.Reader
	order binary.ByteOrder
	buf   [8]byte
}

func (b *plyBinary) read(t plyType) (float64, error) {
	size := 4
	switch t {
	case plyInt8, plyUint8:
		size = 1
	case plyInt16, plyUint16:
		size = 2
	case plyFloat64:
		size = 8
	}
	if _, err := io.ReadFull(b.r, b.buf[:size]); err != nil {
		return 0, errPlyShort
	}
	d := b.buf[:size]
	switch t {
	case plyInt8:
		return float64(int8(d[0])), nil
	case plyUint8:
		return float64(d[0]), nil
	case plyInt16:
		return float64(int16(b.order.Uint16(d))), nil
	case plyUint16:
		return float64(b.order.Uint16(d)), nil
	case plyInt32:
		return float64(int32(b.order.Uint32(d))), nil
	case plyUint32:
		return float64(b.order.Uint32(d)), nil
	case plyFloat32:
		return float64(math.Float32frombits(b.order.Uint32(d))), nil
	}
	return math.Float64frombits(b.order.Uint64(d)), nil
}

type plyASCII struct {
	r      *bufio.Reader
	tokens []string
}

func (a *plyASCII) read(plyType) (float64, error) {
	for len(a.tokens) == 0 {
		line, err := a.r.ReadString('\n')
		if line == "" && err != nil {
			return 0, errPlyShort
		}
		a.tokens = strings.Fields(line)
	}
	token := a.tokens[0]
	a.tokens = a.tokens[1:]
	v, err := strconv.ParseFloat(token, 64)
	if err != nil {
		var ne *strconv.NumError
		if !(errors.As(err, &ne) && ne.Err == strconv.ErrRange) {
			return 0, fmt.Errorf("The PLY file has an invalid number: %q.", token)
		}
	}
	return v, nil
}

// writePly writes one binary little-endian mesh (Z up, millimetres).
func writePly(ctx context.Context, m *Model, w io.Writer) error {
	hasNormals, hasUvs, hasColors := false, false, false
	for _, p := range m.Parts {
		hasNormals = hasNormals || p.Normals != nil
		hasUvs = hasUvs || p.TexCoords != nil
		if mat := m.MaterialOf(p); p.Colors != nil || mat != nil && mat.BaseColor != (vec4{1, 1, 1, 1}) {
			hasColors = true
		}
	}
	for _, p := range m.Parts {
		if hasNormals && p.Normals == nil {
			p.ComputeNormals()
		}
	}
	var h strings.Builder
	h.WriteString("ply\nformat binary_little_endian 1.0\ncomment Written by Filee\n")
	fmt.Fprintf(&h, "element vertex %d\n", m.VertexCount())
	h.WriteString("property float x\nproperty float y\nproperty float z\n")
	if hasNormals {
		h.WriteString("property float nx\nproperty float ny\nproperty float nz\n")
	}
	if hasUvs {
		h.WriteString("property float s\nproperty float t\n")
	}
	if hasColors {
		h.WriteString("property uchar red\nproperty uchar green\nproperty uchar blue\nproperty uchar alpha\n")
	}
	fmt.Fprintf(&h, "element face %d\n", m.TriangleCount())
	h.WriteString("property list uchar int vertex_indices\nend_header\n")

	bw := bufio.NewWriterSize(w, 1<<20)
	bw.WriteString(h.String())
	rec := make([]byte, 0, 36)
	putF := func(b []byte, f float32) []byte { return binary.LittleEndian.AppendUint32(b, math.Float32bits(f)) }
	for _, p := range m.Parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		fallback := vec4{1, 1, 1, 1}
		if mat := m.MaterialOf(p); mat != nil {
			fallback = mat.BaseColor
		}
		fallback = linearToSrgb4(fallback)
		for v := 0; v < p.VertexCount(); v++ {
			rec = rec[:0]
			pos := toZUp(p.Position(v))
			rec = putF(putF(putF(rec, pos.X), pos.Y), pos.Z)
			if hasNormals {
				n := toZUp(p.Normal(v))
				rec = putF(putF(putF(rec, n.X), n.Y), n.Z)
			}
			if hasUvs {
				uv := p.TexCoord(v)
				rec = putF(putF(rec, uv.X), uv.Y)
			}
			if hasColors {
				c := fallback
				if p.Colors != nil {
					c = p.Color(v)
				}
				rec = append(rec, toByte(c.X), toByte(c.Y), toByte(c.Z), toByte(c.W))
			}
			bw.Write(rec)
		}
	}
	offset := 0
	face := make([]byte, 13)
	face[0] = 3
	for _, p := range m.Parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i := 0; i+2 < len(p.Indices); i += 3 {
			binary.LittleEndian.PutUint32(face[1:], uint32(p.Indices[i]+offset))
			binary.LittleEndian.PutUint32(face[5:], uint32(p.Indices[i+1]+offset))
			binary.LittleEndian.PutUint32(face[9:], uint32(p.Indices[i+2]+offset))
			bw.Write(face)
		}
		offset += p.VertexCount()
	}
	return bw.Flush()
}
