package model3d

// Wavefront OBJ with its MTL material library. Unitless (read as
// millimetres), Y up. Read: v (with the common "v x y z r g b" vertex colour
// extension), vt, vn, f with negative (relative) indices and polygons,
// o / g / usemtl as parts, mtllib with Kd, d / Tr and map_Kd textures next to
// the file. Written: one "o" per part, an .mtl next to the .obj and the
// textures next to that.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"
)

type partKey struct {
	name     string
	material string
	hasMat   bool
}

type cornerKey struct{ v, t, n int }

// objPartBuilder collects the vertices of one part, one per distinct (position, uv, normal) corner.
type objPartBuilder struct {
	part    *Part
	corners map[cornerKey]int
}

func readObj(ctx context.Context, path string, progress func(float64)) (*Model, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, _ := f.Stat()
	length := max(int64(1), info.Size())
	abs, _ := filepath.Abs(path)
	directory := filepath.Dir(abs)

	model := &Model{}
	var positions, uvs, normals []float32
	var colors []float32 // nil until a vertex has a colour
	// Materials of the libraries by name; only those a face uses become materials of the model.
	library := map[string]*Material{}
	materials := map[string]int{}
	parts := map[partKey]*objPartBuilder{}
	var order []*objPartBuilder

	objectName, groupName := "", ""
	material, hasMaterial := "", false
	var current *objPartBuilder

	partFor := func(name string) *objPartBuilder {
		key := partKey{name, material, hasMaterial}
		if b, ok := parts[key]; ok {
			return b
		}
		index := -1
		if hasMaterial {
			i, ok := materials[material]
			if !ok {
				// usemtl without a definition (missing .mtl) keeps the name, white.
				i = len(model.Materials)
				m := library[material]
				if m == nil {
					m = newMaterial(material)
				}
				model.Materials = append(model.Materials, m)
				materials[material] = i
			}
			index = i
		}
		partName := name
		if partName == "" {
			partName = "OBJ"
			if hasMaterial {
				partName = material
			}
		}
		p := newPart(partName)
		p.Material = index
		b := &objPartBuilder{part: p, corners: map[cornerKey]int{}}
		parts[key] = b
		order = append(order, b)
		return b
	}

	r := bufio.NewReaderSize(f, 1<<16)
	lineNumber := 0
	var read int64
	first := true
	cornerBuf := make([]int, 0, 256)
	for {
		raw, err := r.ReadString('\n')
		if raw == "" && err != nil {
			if err != io.EOF {
				return nil, err
			}
			break
		}
		read += int64(len(raw))
		lineNumber++
		if first {
			raw = strings.TrimPrefix(raw, "\uFEFF")
			first = false
		}
		if lineNumber&0xFFFF == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if progress != nil {
				progress(float64(read) / float64(length))
			}
		}
		line := strings.TrimRight(raw, "\r\n")
		// A trailing backslash continues the statement on the next line.
		for strings.HasSuffix(line, "\\") {
			next, err := r.ReadString('\n')
			if next == "" && err != nil {
				break
			}
			read += int64(len(next))
			line = line[:len(line)-1] + " " + strings.TrimRight(next, "\r\n")
			lineNumber++
		}
		text := strings.TrimSpace(line)
		if text == "" || text[0] == '#' {
			continue
		}
		keyword, rest := text, ""
		if i := strings.IndexAny(text, " \t"); i >= 0 {
			keyword, rest = text[:i], strings.TrimSpace(text[i+1:])
		}
		switch keyword {
		case "v":
			values, err := parseObjFloats(rest, 7, lineNumber)
			if err != nil {
				return nil, err
			}
			if len(values) < 3 {
				return nil, fmt.Errorf("Line %d of the OBJ file has a vertex with fewer than three coordinates.", lineNumber)
			}
			positions = append(positions, values[0], values[1], values[2])
			if len(values) >= 6 {
				if colors == nil {
					colors = make([]float32, 0, len(positions))
					for i := 0; i < (len(positions)/3-1)*3; i++ {
						colors = append(colors, 1)
					}
				}
				scale := float32(1)
				if values[3] > 1 || values[4] > 1 || values[5] > 1 {
					scale = 1.0 / 255
				}
				colors = append(colors, clamp01(values[3]*scale), clamp01(values[4]*scale), clamp01(values[5]*scale))
			} else if colors != nil {
				colors = append(colors, 1, 1, 1)
			}
		case "vt":
			values, err := parseObjFloats(rest, 3, lineNumber)
			if err != nil {
				return nil, err
			}
			var u, v float32
			if len(values) > 0 {
				u = values[0]
			}
			if len(values) > 1 {
				v = values[1]
			}
			uvs = append(uvs, u, v)
		case "vn":
			values, err := parseObjFloats(rest, 3, lineNumber)
			if err != nil {
				return nil, err
			}
			if len(values) < 3 {
				return nil, fmt.Errorf("Line %d of the OBJ file has a normal with fewer than three values.", lineNumber)
			}
			normals = append(normals, values[0], values[1], values[2])
		case "f", "fo":
			if current == nil {
				name := objectName
				if groupName != "" {
					name = groupName
				}
				current = partFor(name)
			}
			cornerBuf = cornerBuf[:0]
			for _, token := range strings.FieldsFunc(rest, func(r rune) bool { return r == ' ' || r == '\t' }) {
				if len(cornerBuf) == 256 {
					return nil, fmt.Errorf("Line %d of the OBJ file has a face with more than 256 corners.", lineNumber)
				}
				c, err := current.corner(token, positions, colors, uvs, normals, lineNumber)
				if err != nil {
					return nil, err
				}
				cornerBuf = append(cornerBuf, c)
			}
			addPolygon(current.part, cornerBuf)
		case "o":
			objectName, groupName = rest, ""
			current = nil
		case "g":
			groupName = rest
			current = nil
		case "usemtl":
			material, hasMaterial = rest, true
			current = nil
		case "mtllib":
			for _, file := range libraryNames(rest, directory) {
				readMtl(file, library)
			}
		}
	}

	for _, b := range order {
		p := b.part
		if p.Normals != nil && hasZeroNormal(p.Normals) {
			p.ComputeNormals()
		}
		model.Parts = append(model.Parts, p)
	}
	return model, nil
}

func hasZeroNormal(n []float32) bool {
	for i := 0; i+2 < len(n); i += 3 {
		if n[i] == 0 && n[i+1] == 0 && n[i+2] == 0 {
			return true
		}
	}
	return false
}

// libraryNames are the .mtl files of an mtllib statement: the whole text as
// one name first (names may have spaces), else each word.
func libraryNames(text, directory string) []string {
	if whole := resolveReference(directory, text); whole != "" {
		return []string{whole}
	}
	var out []string
	for _, n := range strings.Fields(text) {
		if f := resolveReference(directory, n); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// resolveReference finds a file referenced from a 3D model (an .mtl, a
// texture, a glTF buffer) inside directory or a folder below it, else by name
// only in directory. A path that climbs out or points elsewhere is never
// opened: a downloaded model must not pull a picture from anywhere on the disk
// into its output. "" when there is none.
func resolveReference(directory, reference string) string {
	reference = strings.ReplaceAll(strings.Trim(strings.TrimSpace(reference), `"`), `\`, "/")
	if reference == "" || strings.ContainsRune(reference, 0) {
		return ""
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return ""
	}
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root += string(filepath.Separator)
	}
	inside := func(candidate string) bool {
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			return strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(root))
		}
		return strings.HasPrefix(candidate, root)
	}
	exists := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
	// An absolute reference is taken as it is (filepath.Join would put it below root).
	candidate := filepath.FromSlash(reference)
	if filepath.IsAbs(candidate) {
		candidate = filepath.Clean(candidate)
	} else {
		candidate = filepath.Join(root, candidate)
	}
	if inside(candidate) && exists(candidate) {
		return candidate
	}
	name := reference
	if i := strings.LastIndexAny(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" || name == "." || name == ".." {
		return ""
	}
	candidate = filepath.Join(root, name)
	if exists(candidate) {
		return candidate
	}
	return ""
}

func parseObjFloats(text string, limit, line int) ([]float32, error) {
	var out []float32
	for _, token := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '\t' }) {
		if len(out) == limit {
			break
		}
		v, ok := parseFloat32(token)
		if !ok {
			return nil, fmt.Errorf("Line %d of the OBJ file has an invalid number: %q.", line, token)
		}
		out = append(out, v)
	}
	return out, nil
}

func (b *objPartBuilder) corner(token string, positions, colors, uvs, normals []float32, line int) (int, error) {
	v, t, n := -1, -1, -1
	for slot, field := range strings.Split(token, "/") {
		if slot > 2 {
			break
		}
		if field == "" {
			continue
		}
		value, err := strconv.Atoi(field)
		if err != nil || value == 0 {
			return 0, fmt.Errorf("Line %d of the OBJ file has an invalid face index: %q.", line, token)
		}
		var count int
		var what string
		switch slot {
		case 0:
			count, what = len(positions)/3, "vertex"
		case 1:
			count, what = len(uvs)/2, "texture coordinate"
		default:
			count, what = len(normals)/3, "normal"
		}
		index := value - 1
		if value < 0 {
			index = count + value
		}
		if index < 0 || index >= count {
			return 0, fmt.Errorf("Line %d of the OBJ file refers to %s %d, which does not exist (there are %d).", line, what, value, count)
		}
		switch slot {
		case 0:
			v = index
		case 1:
			t = index
		default:
			n = index
		}
	}
	if v < 0 {
		return 0, fmt.Errorf("Line %d of the OBJ file has a face corner without a vertex: %q.", line, token)
	}
	key := cornerKey{v, t, n}
	if existing, ok := b.corners[key]; ok {
		return existing, nil
	}
	p := b.part
	if t >= 0 && p.TexCoords == nil {
		p.TexCoords = make([]float32, p.VertexCount()*2)
	}
	if n >= 0 && p.Normals == nil {
		p.Normals = make([]float32, p.VertexCount()*3)
	}
	if colors != nil && p.Colors == nil {
		p.Colors = make([]float32, p.VertexCount()*4)
		for i := range p.Colors {
			p.Colors[i] = 1
		}
	}
	var normal *vec3
	if n >= 0 {
		normal = &vec3{normals[n*3], normals[n*3+1], normals[n*3+2]}
	}
	var uv *vec2
	if t >= 0 {
		uv = &vec2{uvs[t*2], uvs[t*2+1]}
	}
	var color *vec4
	if colors != nil {
		color = &vec4{colors[v*3], colors[v*3+1], colors[v*3+2], 1}
	}
	vertex := p.AddVertex(vec3{positions[v*3], positions[v*3+1], positions[v*3+2]}, normal, uv, color)
	b.corners[key] = vertex
	return vertex, nil
}

const defaultMaterial = "Default"

// writeObj writes m as OBJ. mtlPath ("" = no material library) gets the
// materials; texturePaths maps each texture to the file it was written to.
func writeObj(ctx context.Context, m *Model, objPath, mtlPath string, texturePaths map[*Texture]string) error {
	anyColors := false
	for _, p := range m.Parts {
		if p.Colors != nil {
			anyColors = true
		}
	}
	matNames := make([]string, len(m.Materials))
	for i, mat := range m.Materials {
		matNames[i] = mat.Name
	}
	materialNames := uniqueObjNames(matNames, "Material")

	f, err := os.Create(objPath)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<16)
	w.WriteString("# Written by Filee\n")
	if mtlPath != "" {
		fmt.Fprintf(w, "mtllib %s\n", relativeReference(objPath, mtlPath))
	}
	names := make([]string, len(m.Parts))
	for i, p := range m.Parts {
		names[i] = p.Name
	}
	partNames := uniqueObjNames(names, "Part")
	vBase, tBase, nBase := 1, 1, 1
	buf := make([]byte, 0, 256)
	for pi, p := range m.Parts {
		if err := ctx.Err(); err != nil {
			f.Close()
			return err
		}
		fmt.Fprintf(w, "o %s\n", partNames[pi])
		// Without vertex colours, a material's colour becomes the vertex colour, so parts look the same.
		fallback := vec4{1, 1, 1, 1}
		if mat := m.MaterialOf(p); mat != nil {
			fallback = mat.BaseColor
		}
		fallback = linearToSrgb4(fallback)
		for v := 0; v < p.VertexCount(); v++ {
			pos := p.Position(v)
			buf = append(buf[:0], 'v')
			buf = appendObjFloat(buf, pos.X)
			buf = appendObjFloat(buf, pos.Y)
			buf = appendObjFloat(buf, pos.Z)
			if anyColors {
				c := fallback
				if p.Colors != nil {
					c = p.Color(v)
				}
				buf = appendObjFloat(buf, c.X)
				buf = appendObjFloat(buf, c.Y)
				buf = appendObjFloat(buf, c.Z)
			}
			buf = append(buf, '\n')
			w.Write(buf)
		}
		if p.TexCoords != nil {
			for v := 0; v < p.VertexCount(); v++ {
				uv := p.TexCoord(v)
				buf = append(buf[:0], 'v', 't')
				buf = appendObjFloat(buf, uv.X)
				buf = appendObjFloat(buf, uv.Y)
				buf = append(buf, '\n')
				w.Write(buf)
			}
		}
		if p.Normals != nil {
			for v := 0; v < p.VertexCount(); v++ {
				n := p.Normal(v)
				buf = append(buf[:0], 'v', 'n')
				buf = appendObjFloat(buf, n.X)
				buf = appendObjFloat(buf, n.Y)
				buf = appendObjFloat(buf, n.Z)
				buf = append(buf, '\n')
				w.Write(buf)
			}
		}
		if mtlPath != "" {
			name := defaultMaterial
			if p.Material >= 0 && p.Material < len(m.Materials) {
				name = materialNames[p.Material]
			}
			fmt.Fprintf(w, "usemtl %s\n", name)
		}
		for i := 0; i+2 < len(p.Indices); i += 3 {
			buf = append(buf[:0], 'f')
			for k := 0; k < 3; k++ {
				idx := p.Indices[i+k]
				buf = append(buf, ' ')
				buf = strconv.AppendInt(buf, int64(idx+vBase), 10)
				hasT, hasN := p.TexCoords != nil, p.Normals != nil
				if !hasT && !hasN {
					continue
				}
				buf = append(buf, '/')
				if hasT {
					buf = strconv.AppendInt(buf, int64(idx+tBase), 10)
				}
				if hasN {
					buf = append(buf, '/')
					buf = strconv.AppendInt(buf, int64(idx+nBase), 10)
				}
			}
			buf = append(buf, '\n')
			w.Write(buf)
		}
		vBase += p.VertexCount()
		if p.TexCoords != nil {
			tBase += p.VertexCount()
		}
		if p.Normals != nil {
			nBase += p.VertexCount()
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if mtlPath != "" {
		return writeMtl(m, materialNames, mtlPath, texturePaths)
	}
	return nil
}

// appendObjFloat appends " value" in the shortest form that reads back the same (0 for NaN / infinity).
func appendObjFloat(buf []byte, v float32) []byte {
	if !finite(v) {
		v = 0
	}
	buf = append(buf, ' ')
	return appendShortFloat(buf, v)
}

// appendShortFloat formats like .NET's float.ToString(): shortest round trip,
// exponent form "1E-05" for very small or large values.
func appendShortFloat(buf []byte, v float32) []byte {
	if v == 0 {
		return append(buf, '0')
	}
	a := abs32(v)
	if a < 1e-4 || a >= 1e15 {
		s := strconv.FormatFloat(float64(v), 'E', -1, 32)
		// Go writes "1E-05" already; .NET does the same ("E-05", "E+15").
		return append(buf, s...)
	}
	return strconv.AppendFloat(buf, float64(v), 'f', -1, 32)
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// uniqueObjNames are OBJ / MTL names without spaces, unique, never empty.
func uniqueObjNames(names []string, fallback string) []string {
	used := map[string]bool{defaultMaterial: true}
	out := make([]string, 0, len(names))
	for _, name := range names {
		clean := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || unicode.IsControl(r) {
				return '_'
			}
			return r
		}, name)
		if clean == "" {
			clean = fallback
		}
		unique := clean
		for i := 2; used[unique]; i++ {
			unique = fmt.Sprintf("%s_%d", clean, i)
		}
		used[unique] = true
		out = append(out, unique)
	}
	return out
}

// relativeReference is the path of target as written in a file next to from (forward slashes).
func relativeReference(from, target string) string {
	fa, _ := filepath.Abs(from)
	ta, _ := filepath.Abs(target)
	rel, err := filepath.Rel(filepath.Dir(fa), ta)
	if err != nil {
		rel = ta
	}
	return filepath.ToSlash(rel)
}

// readMtl adds the materials of an MTL file to library (a later definition of a name wins).
func readMtl(path string, library map[string]*Material) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	directory := filepath.Dir(path)
	var current *Material
	for _, raw := range strings.Split(strings.TrimPrefix(string(data), "\uFEFF"), "\n") {
		text := strings.TrimSpace(raw)
		if text == "" || text[0] == '#' {
			continue
		}
		keyword, rest := text, ""
		if i := strings.IndexAny(text, " \t"); i >= 0 {
			keyword, rest = text[:i], strings.TrimSpace(text[i+1:])
		}
		if keyword == "newmtl" {
			current = newMaterial(rest)
			library[rest] = current
			continue
		}
		if current == nil {
			continue
		}
		values := strings.Fields(rest)
		switch strings.ToLower(keyword) {
		case "kd":
			if kd, ok := mtlFloats(values, 3); ok {
				current.BaseColor = vec4{kd[0], kd[1], kd[2], current.BaseColor.W}
			}
		case "d":
			if len(values) >= 1 {
				if d, ok := mtlFloats(values[len(values)-1:], 1); ok {
					current.BaseColor.W = clamp01(d[0])
				}
			}
		case "tr":
			if len(values) >= 1 {
				if tr, ok := mtlFloats(values[len(values)-1:], 1); ok {
					current.BaseColor.W = clamp01(1 - tr[0])
				}
			}
		case "map_kd":
			if file := mtlTextureFile(values); file != "" {
				if texture := resolveReference(directory, file); texture != "" {
					if data, err := os.ReadFile(texture); err == nil {
						ext := filepath.Ext(texture)
						current.BaseColorTexture = &Texture{Data: data, Extension: sniff(data, ext), Name: strings.TrimSuffix(filepath.Base(texture), ext)}
					}
				}
			}
		}
	}
}

func mtlFloats(values []string, count int) ([]float32, bool) {
	if len(values) < count {
		return nil, false
	}
	out := make([]float32, count)
	for i := 0; i < count; i++ {
		v, ok := parseFloat32(values[i])
		if !ok {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// mtlTextureFile is the file name of a map_Kd statement after its options (-s 1 1 1, -bm 0.5, -clamp on, ...).
func mtlTextureFile(values []string) string {
	i := 0
	for i < len(values) && strings.HasPrefix(values[i], "-") {
		option := strings.ToLower(values[i])
		i++
		arguments := 0
		switch option {
		case "-o", "-s", "-t":
			arguments = 3
		case "-mm":
			arguments = 2
		case "-blendu", "-blendv", "-boost", "-cc", "-clamp", "-texres", "-bm", "-imfchan", "-type":
			arguments = 1
		}
		// -o, -s and -t take one to three numbers.
		for a := 0; a < arguments && i < len(values); a++ {
			if arguments == 3 && a > 0 {
				if _, ok := parseFloat32(values[i]); !ok {
					break
				}
			}
			i++
		}
	}
	if i < len(values) {
		return strings.Join(values[i:], " ")
	}
	return ""
}

func writeMtl(m *Model, names []string, path string, texturePaths map[*Texture]string) error {
	var b strings.Builder
	b.WriteString("# Written by Filee\n")
	write := func(name string, mat *Material) {
		c := mat.BaseColor
		b.WriteString("\n")
		fmt.Fprintf(&b, "newmtl %s\n", name)
		fmt.Fprintf(&b, "Kd %s %s %s\n", shortFloat(c.X), shortFloat(c.Y), shortFloat(c.Z))
		b.WriteString("Ka 0 0 0\nKs 0 0 0\n")
		if c.W < 1 {
			fmt.Fprintf(&b, "d %s\n", shortFloat(c.W))
		}
		b.WriteString("illum 1\n")
		if mat.BaseColorTexture != nil {
			if file, ok := texturePaths[mat.BaseColorTexture]; ok {
				fmt.Fprintf(&b, "map_Kd %s\n", relativeReference(path, file))
			}
		}
	}
	write(defaultMaterial, newMaterial(""))
	for i, mat := range m.Materials {
		write(names[i], mat)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func shortFloat(v float32) string { return string(appendShortFloat(nil, v)) }
