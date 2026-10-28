package pdf

import (
	"bytes"
	"strconv"
)

// contentOp is one operator of a content stream with its operands (numbers
// as float64, names without the slash as name, everything else skipped as nil).
type contentOp struct {
	Op       string
	Operands []any
}

type name string

// scanContent calls f for every operator of a content stream. Inline images
// (BI … ID data EI) are one "BI" operator.
func scanContent(data []byte, f func(op contentOp) bool) {
	var operands []any
	i := 0
	n := len(data)
	isDelim := func(c byte) bool {
		return c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
	}
	isSpace := func(c byte) bool { return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0 }
	depth := 0 // inside arrays and dictionaries, operands are not counted
	for i < n {
		c := data[i]
		switch {
		case isSpace(c):
			i++
		case c == '%':
			for i < n && data[i] != '\n' && data[i] != '\r' {
				i++
			}
		case c == '(':
			level := 0
			for i < n {
				if data[i] == '\\' {
					i += 2
					continue
				}
				if data[i] == '(' {
					level++
				} else if data[i] == ')' {
					level--
					if level == 0 {
						i++
						break
					}
				}
				i++
			}
			if depth == 0 {
				operands = append(operands, nil)
			}
		case c == '<' && i+1 < n && data[i+1] == '<':
			depth++
			i += 2
		case c == '>' && i+1 < n && data[i+1] == '>':
			depth--
			i += 2
			if depth == 0 {
				operands = append(operands, nil)
			}
		case c == '<':
			for i < n && data[i] != '>' {
				i++
			}
			i++
			if depth == 0 {
				operands = append(operands, nil)
			}
		case c == '[':
			depth++
			i++
		case c == ']':
			depth--
			i++
			if depth == 0 {
				operands = append(operands, nil)
			}
		case c == '/':
			j := i + 1
			for j < n && !isSpace(data[j]) && !isDelim(data[j]) {
				j++
			}
			if depth == 0 {
				operands = append(operands, name(data[i+1:j]))
			}
			i = j
		case c == '{' || c == '}' || c == ')' || c == '>':
			i++
		default:
			j := i
			for j < n && !isSpace(data[j]) && !isDelim(data[j]) {
				j++
			}
			tok := string(data[i:j])
			i = j
			if v, err := strconv.ParseFloat(tok, 64); err == nil || (len(tok) > 0 && (tok[0] == '-' || tok[0] == '+' || tok[0] == '.' || (tok[0] >= '0' && tok[0] <= '9'))) {
				if depth == 0 {
					operands = append(operands, v)
				}
				continue
			}
			if depth > 0 {
				continue // true, false, null inside an array or dictionary
			}
			if tok == "true" || tok == "false" || tok == "null" {
				operands = append(operands, nil)
				continue
			}
			if tok == "BI" {
				// Skip the inline image up to "EI" after whitespace.
				id := bytes.Index(data[i:], []byte("ID"))
				if id < 0 {
					return
				}
				k := i + id + 3
				for k+2 <= n {
					e := bytes.Index(data[k:], []byte("EI"))
					if e < 0 {
						k = n
						break
					}
					k += e
					if (k == 0 || isSpace(data[k-1])) && (k+2 == n || isSpace(data[k+2]) || isDelim(data[k+2])) {
						k += 2
						break
					}
					k += 2
				}
				i = k
			}
			if !f(contentOp{Op: tok, Operands: operands}) {
				return
			}
			operands = operands[:0]
		}
	}
}

func operandNumber(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}
