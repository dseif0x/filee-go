package model3d

// The node tree of an Autodesk FBX file, binary or ASCII. Binary: the
// "Kaydara FBX Binary" header, node records with 32-bit offsets (64-bit from
// version 7.5), scalar, string, raw and array properties, arrays plain or
// zlib-compressed. ASCII: "Name: value, value { children }" with ";" comments
// and "*count { a: ... }" arrays. Animation curves, poses, deformers and the
// other parts the reader never looks at are skipped without decoding.

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const fbxDamaged = "The FBX file is cut short or damaged."

var errFbxDamaged = errors.New(fbxDamaged)

const fbxMaxDepth = 64

// fbxNode is one FBX node: a name, its properties (int64, float64, string,
// bool, []byte or a slice of numbers/bools) and its children.
type fbxNode struct {
	name       string
	properties []any
	children   []*fbxNode
}

func (n *fbxNode) child(name string) *fbxNode {
	if n == nil {
		return nil
	}
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

func (n *fbxNode) childrenNamed(name string) []*fbxNode {
	if n == nil {
		return nil
	}
	var out []*fbxNode
	for _, c := range n.children {
		if c.name == name {
			out = append(out, c)
		}
	}
	return out
}

// text returns property i as a string, or "" and false.
func (n *fbxNode) text(i int) (string, bool) {
	if n == nil || i >= len(n.properties) {
		return "", false
	}
	s, ok := n.properties[i].(string)
	return s, ok
}

var fbxBinaryMagic = []byte("Kaydara FBX Binary  \x00")

// Nodes no reader needs (by name, at the top level and in Objects): animation can be most of a file.
var fbxSkipped = map[string]bool{
	"Takes": true, "References": true, "Documents": true, "Version5": true,
	"AnimationStack": true, "AnimationLayer": true, "AnimationCurveNode": true, "AnimationCurve": true, "Pose": true, "Deformer": true,
	"NodeAttribute": true, "CollectionExclusive": true, "Constraint": true, "Implementation": true, "BindingTable": true,
}

// fbxSkip stands for a skipped node while reading.
var fbxSkip = &fbxNode{}

// parseFbx reads a whole file; the returned node (no name) has the top-level nodes as children.
func parseFbx(ctx context.Context, data []byte) (*fbxNode, error) {
	if bytes.HasPrefix(data, fbxBinaryMagic) {
		return parseFbxBinary(ctx, data)
	}
	if fbxLooksASCII(data) {
		r := &fbxASCII{text: []rune(strings.ToValidUTF8(string(data), "�")), ctx: ctx}
		return r.document()
	}
	return nil, errors.New("The file is not an FBX model.")
}

func fbxLooksASCII(data []byte) bool {
	head := data[:min(len(data), 4096)]
	// Cut a split character at the end.
	for len(head) > 0 && !utf8.Valid(head) && len(data) > 4096 {
		head = head[:len(head)-1]
	}
	s := strings.TrimLeft(string(head), "\uFEFF \t\r\n")
	return !strings.ContainsRune(s, 0) && (strings.HasPrefix(s, "; FBX") || strings.Contains(s, "FBXHeaderExtension:"))
}

type fbxBinary struct {
	data []byte
	wide bool
	ctx  context.Context
}

func parseFbxBinary(ctx context.Context, data []byte) (*fbxNode, error) {
	if len(data) < 27 {
		return nil, errFbxDamaged
	}
	version := binary.LittleEndian.Uint32(data[23:])
	r := &fbxBinary{data: data, wide: version >= 7500, ctx: ctx}
	root := &fbxNode{}
	position := int64(27)
	for {
		node, err := r.node(&position, int64(len(data)), 0)
		if err != nil {
			return nil, err
		}
		if node == nil {
			break
		}
		if node != fbxSkip {
			root.children = append(root.children, node)
		}
	}
	if len(root.children) == 0 {
		return nil, errFbxDamaged
	}
	return root, nil
}

func (r *fbxBinary) headerSize() int64 {
	if r.wide {
		return 25
	}
	return 13
}

// node reads the node at position: the node, fbxSkip, or nil at the end of a list.
func (r *fbxBinary) node(position *int64, limit int64, depth int) (*fbxNode, error) {
	if depth <= 2 {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
	}
	hs := r.headerSize()
	if *position+hs > limit {
		// The top-level list may end without its null record; a nested one may not.
		if depth == 0 {
			return nil, nil
		}
		return nil, errFbxDamaged
	}
	header := r.data[*position : *position+hs]
	var end, count, listLength int64
	if r.wide {
		end = int64(binary.LittleEndian.Uint64(header))
		count = int64(binary.LittleEndian.Uint64(header[8:]))
		listLength = int64(binary.LittleEndian.Uint64(header[16:]))
	} else {
		end = int64(binary.LittleEndian.Uint32(header))
		count = int64(binary.LittleEndian.Uint32(header[4:]))
		listLength = int64(binary.LittleEndian.Uint32(header[8:]))
	}
	nameLength := int64(header[hs-1])
	*position += hs
	if end == 0 {
		return nil, nil // the null record that ends a list
	}
	propertiesStart := *position + nameLength
	if depth > fbxMaxDepth || end > limit || end < 0 || propertiesStart > end || listLength < 0 || listLength > end-propertiesStart ||
		count < 0 || count > listLength {
		return nil, errFbxDamaged
	}
	name := string(r.data[*position : *position+nameLength])
	if depth <= 1 && fbxSkipped[name] {
		*position = end
		return fbxSkip, nil
	}
	node := &fbxNode{name: name}
	*position = propertiesStart
	propertiesEnd := propertiesStart + listLength
	for i := int64(0); i < count; i++ {
		p, err := r.property(position, propertiesEnd)
		if err != nil {
			return nil, err
		}
		node.properties = append(node.properties, p)
	}
	*position = propertiesEnd
	for *position < end {
		child, err := r.node(position, end, depth+1)
		if err != nil {
			return nil, err
		}
		if child == nil {
			break
		}
		if child != fbxSkip {
			node.children = append(node.children, child)
		}
	}
	*position = end
	return node, nil
}

func (r *fbxBinary) need(position, length, end int64) error {
	if length < 0 || position+length > end {
		return errFbxDamaged
	}
	return nil
}

func (r *fbxBinary) property(position *int64, end int64) (any, error) {
	if err := r.need(*position, 1, end); err != nil {
		return nil, err
	}
	t := r.data[*position]
	*position++
	d := r.data
	read := func(n int64) ([]byte, error) {
		if err := r.need(*position, n, end); err != nil {
			return nil, err
		}
		b := d[*position : *position+n]
		*position += n
		return b, nil
	}
	switch t {
	case 'C':
		b, err := read(1)
		if err != nil {
			return nil, err
		}
		return b[0] != 0, nil
	case 'Y':
		b, err := read(2)
		if err != nil {
			return nil, err
		}
		return int64(int16(binary.LittleEndian.Uint16(b))), nil
	case 'I':
		b, err := read(4)
		if err != nil {
			return nil, err
		}
		return int64(int32(binary.LittleEndian.Uint32(b))), nil
	case 'L':
		b, err := read(8)
		if err != nil {
			return nil, err
		}
		return int64(binary.LittleEndian.Uint64(b)), nil
	case 'F':
		b, err := read(4)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), nil
	case 'D':
		b, err := read(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
	case 'S', 'R':
		lb, err := read(4)
		if err != nil {
			return nil, err
		}
		b, err := read(int64(binary.LittleEndian.Uint32(lb)))
		if err != nil {
			return nil, err
		}
		if t == 'S' {
			return strings.ToValidUTF8(string(b), "�"), nil
		}
		return bytes.Clone(b), nil
	case 'f', 'd', 'l', 'i', 'b':
		return r.array(t, position, end)
	}
	return nil, errFbxDamaged
}

func (r *fbxBinary) array(t byte, position *int64, end int64) (any, error) {
	if err := r.need(*position, 12, end); err != nil {
		return nil, err
	}
	h := r.data[*position : *position+12]
	length := int64(binary.LittleEndian.Uint32(h))
	encoding := binary.LittleEndian.Uint32(h[4:])
	stored := int64(binary.LittleEndian.Uint32(h[8:]))
	*position += 12
	if err := r.need(*position, stored, end); err != nil {
		return nil, err
	}
	size := int64(1)
	switch t {
	case 'f', 'i':
		size = 4
	case 'd', 'l':
		size = 8
	}
	n := length * size
	// Deflate packs at most about 1032:1, so a larger length is a lie that would only allocate memory.
	if n > math.MaxInt32-64 || (encoding == 0 && n != stored) || (encoding != 0 && n > stored*1032+64) {
		return nil, errFbxDamaged
	}
	raw := r.data[*position : *position+stored]
	switch encoding {
	case 0:
	case 1:
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, errFbxDamaged
		}
		inflated := make([]byte, n)
		if _, err := io.ReadFull(zr, inflated); err != nil {
			return nil, errFbxDamaged
		}
		raw = inflated
	default:
		return nil, errFbxDamaged
	}
	*position += stored
	le := binary.LittleEndian
	switch t {
	case 'f':
		out := make([]float32, length)
		for i := range out {
			out[i] = math.Float32frombits(le.Uint32(raw[i*4:]))
		}
		return out, nil
	case 'd':
		out := make([]float64, length)
		for i := range out {
			out[i] = math.Float64frombits(le.Uint64(raw[i*8:]))
		}
		return out, nil
	case 'i':
		out := make([]int32, length)
		for i := range out {
			out[i] = int32(le.Uint32(raw[i*4:]))
		}
		return out, nil
	case 'l':
		out := make([]int64, length)
		for i := range out {
			out[i] = int64(le.Uint64(raw[i*8:]))
		}
		return out, nil
	}
	out := make([]bool, length)
	for i := range out {
		out[i] = raw[i] != 0
	}
	return out, nil
}

// ───────────────────────── ASCII ─────────────────────────

type fbxASCII struct {
	text     []rune
	position int
	nodes    int
	ctx      context.Context
}

func (r *fbxASCII) document() (*fbxNode, error) {
	root := &fbxNode{}
	for {
		r.skipSpace(true)
		if r.position >= len(r.text) {
			break
		}
		node, err := r.node(0)
		if err != nil {
			return nil, err
		}
		if !fbxSkipped[node.name] {
			root.children = append(root.children, node)
		}
	}
	if len(root.children) == 0 {
		return nil, errors.New("The file is not an FBX model.")
	}
	return root, nil
}

func isNameStop(c rune) bool {
	return c == ':' || c == '{' || c == '}' || c == ',' || c == '"' || unicode.IsSpace(c)
}

func (r *fbxASCII) node(depth int) (*fbxNode, error) {
	r.nodes++
	if r.nodes&0x3FF == 0 {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
	}
	if depth > fbxMaxDepth {
		return nil, r.invalid()
	}
	start := r.position
	for r.position < len(r.text) && !isNameStop(r.text[r.position]) {
		r.position++
	}
	if r.position == start || r.position >= len(r.text) || r.text[r.position] != ':' {
		return nil, r.invalid()
	}
	node := &fbxNode{name: string(r.text[start:r.position])}
	r.position++
	// The values of an array ("a: 1,2,3,...") go straight into one list instead of a property each.
	var numbers []float64
	isArray := node.name == "a"

	r.skipSpace(false)
	for r.position < len(r.text) {
		c := r.text[r.position]
		if c == '{' {
			r.position++
			if err := r.childrenOf(node, depth); err != nil {
				return nil, err
			}
			break
		}
		if c == '\r' || c == '\n' || c == ';' || c == '}' {
			break
		}
		if c == ',' {
			r.position++
			r.skipSpace(true)
			continue
		}
		if err := r.value(node, isArray, &numbers); err != nil {
			return nil, err
		}
		r.skipSpace(false)
	}
	if isArray {
		if numbers == nil {
			numbers = []float64{}
		}
		node.properties = append(node.properties, numbers)
	}
	return node, nil
}

func (r *fbxASCII) childrenOf(node *fbxNode, depth int) error {
	for {
		r.skipSpace(true)
		if r.position >= len(r.text) {
			return errFbxDamaged
		}
		if r.text[r.position] == '}' {
			r.position++
			break
		}
		child, err := r.node(depth + 1)
		if err != nil {
			return err
		}
		if depth == 0 && fbxSkipped[child.name] {
			continue
		}
		node.children = append(node.children, child)
	}
	// "Vertices: *24 { a: ... }": the array becomes the node's only property.
	if len(node.children) == 1 && node.children[0].name == "a" {
		node.properties = append([]any(nil), node.children[0].properties...)
		node.children = nil
	}
	return nil
}

func (r *fbxASCII) value(node *fbxNode, isArray bool, numbers *[]float64) error {
	c := r.text[r.position]
	if c == '"' {
		end := -1
		for i := r.position + 1; i < len(r.text); i++ {
			if r.text[i] == '"' {
				end = i
				break
			}
		}
		if end < 0 {
			return errFbxDamaged
		}
		node.properties = append(node.properties, strings.ReplaceAll(string(r.text[r.position+1:end]), "&quot;", `"`))
		r.position = end + 1
		return nil
	}
	start := r.position
	for r.position < len(r.text) {
		ch := r.text[r.position]
		if ch == ',' || ch == '{' || ch == '}' || ch == ';' || unicode.IsSpace(ch) {
			break
		}
		r.position++
	}
	if r.position == start {
		r.position++ // a stray character (another kind of space)
		return nil
	}
	token := string(r.text[start:r.position])
	if token[0] == '*' {
		return nil // the element count of an array that follows in braces
	}
	if isArray {
		v, err := strconv.ParseFloat(token, 64)
		if err != nil && !isRangeErr(err) {
			return r.invalid()
		}
		*numbers = append(*numbers, v)
		return nil
	}
	if !strings.ContainsAny(token, ".eE") {
		if v, err := strconv.ParseInt(token, 10, 64); err == nil {
			node.properties = append(node.properties, v)
			return nil
		}
	}
	if v, err := strconv.ParseFloat(token, 64); err == nil && !strings.ContainsAny(token, "xXpP_") {
		node.properties = append(node.properties, v)
		return nil
	}
	node.properties = append(node.properties, token) // T, Y, W, ...
	return nil
}

func isRangeErr(err error) bool {
	var ne *strconv.NumError
	return errors.As(err, &ne) && ne.Err == strconv.ErrRange
}

func (r *fbxASCII) skipSpace(newLines bool) {
	for r.position < len(r.text) {
		c := r.text[r.position]
		switch {
		case c == ';' && newLines:
			end := -1
			for i := r.position; i < len(r.text); i++ {
				if r.text[i] == '\n' {
					end = i
					break
				}
			}
			if end < 0 {
				r.position = len(r.text)
			} else {
				r.position = end + 1
			}
		case c == ' ' || c == '\t' || c == '\uFEFF' || (newLines && (c == '\r' || c == '\n')):
			r.position++
		default:
			return
		}
	}
}

func (r *fbxASCII) invalid() error {
	line := 1
	for i := 0; i < r.position && i < len(r.text); i++ {
		if r.text[i] == '\n' {
			line++
		}
	}
	return fmt.Errorf("Line %d of the FBX file is not valid.", line)
}
