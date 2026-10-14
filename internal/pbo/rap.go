// Copyright (c) 2026 Aristarh Ucolov.
package pbo

// Configs: binarized (config.bin, "rap") and plain text (config.cpp), read
// into the same small tree — classes with their parent class and the values
// the manager looks at (numbers, strings and arrays of them).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Class is one config class.
type Class struct {
	Name    string
	Parent  string // inherited class name ("" for none)
	Classes []*Class
	Values  map[string]interface{} // string | float64 | []interface{} (lower-case keys)
}

// Child returns the sub-class with this name (case-insensitive).
func (c *Class) Child(name string) *Class {
	for _, k := range c.Classes {
		if strings.EqualFold(k.Name, name) {
			return k
		}
	}
	return nil
}

// Number reads a numeric value.
func (c *Class) Number(key string) (float64, bool) {
	v, ok := c.Values[strings.ToLower(key)]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

// String reads a string value.
func (c *Class) String(key string) (string, bool) {
	v, ok := c.Values[strings.ToLower(key)].(string)
	return v, ok
}

// Strings reads an array of strings (other elements skipped).
func (c *Class) Strings(key string) []string {
	arr, _ := c.Values[strings.ToLower(key)].([]interface{})
	var out []string
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func newClass(name, parent string) *Class {
	return &Class{Name: name, Parent: parent, Values: map[string]interface{}{}}
}

// ParseConfig reads a binarized (raP) or text config.
func ParseConfig(data []byte) (*Class, error) {
	if len(data) >= 4 && string(data[:4]) == "\x00raP" {
		return parseRap(data)
	}
	return parseText(string(data))
}

// ---------------------------------------------------------------- binarized

type rapReader struct {
	b     []byte
	depth int
}

func (r *rapReader) u8(at *int) (byte, error) {
	if *at >= len(r.b) {
		return 0, errors.New("rap: unexpected end")
	}
	v := r.b[*at]
	*at++
	return v, nil
}

func (r *rapReader) u32(at *int) (uint32, error) {
	if *at+4 > len(r.b) {
		return 0, errors.New("rap: unexpected end")
	}
	v := binary.LittleEndian.Uint32(r.b[*at:])
	*at += 4
	return v, nil
}

func (r *rapReader) asciiz(at *int) (string, error) {
	for i := *at; i < len(r.b); i++ {
		if r.b[i] == 0 {
			s := string(r.b[*at:i])
			*at = i + 1
			return s, nil
		}
		if i-*at > 4096 {
			return "", errors.New("rap: string too long")
		}
	}
	return "", errors.New("rap: unterminated string")
}

// cint reads Bohemia's compressed integer (7 bits per byte, low first).
func (r *rapReader) cint(at *int) (int, error) {
	v, shift := 0, 0
	for i := 0; i < 5; i++ {
		b, err := r.u8(at)
		if err != nil {
			return 0, err
		}
		v |= int(b&0x7F) << shift
		if b&0x80 == 0 {
			return v, nil
		}
		shift += 7
	}
	return 0, errors.New("rap: bad compressed integer")
}

func parseRap(b []byte) (*Class, error) {
	if len(b) < 16 {
		return nil, errors.New("rap: too short")
	}
	r := &rapReader{b: b}
	root := newClass("", "")
	if err := r.body(16, root); err != nil {
		return nil, err
	}
	return root, nil
}

func (r *rapReader) body(offset int, c *Class) error {
	r.depth++
	defer func() { r.depth-- }()
	if r.depth > 64 || offset < 16 || offset >= len(r.b) {
		return fmt.Errorf("rap: bad class offset %d", offset)
	}
	at := offset
	parent, err := r.asciiz(&at)
	if err != nil {
		return err
	}
	c.Parent = parent
	n, err := r.cint(&at)
	if err != nil {
		return err
	}
	if n > 100000 {
		return errors.New("rap: too many entries")
	}
	for i := 0; i < n; i++ {
		typ, err := r.u8(&at)
		if err != nil {
			return err
		}
		switch typ {
		case 0: // class
			name, err := r.asciiz(&at)
			if err != nil {
				return err
			}
			off, err := r.u32(&at)
			if err != nil {
				return err
			}
			sub := newClass(name, "")
			if err := r.body(int(off), sub); err != nil {
				return err
			}
			c.Classes = append(c.Classes, sub)
		case 1: // value
			sub, err := r.u8(&at)
			if err != nil {
				return err
			}
			name, err := r.asciiz(&at)
			if err != nil {
				return err
			}
			v, err := r.scalar(&at, sub)
			if err != nil {
				return err
			}
			c.Values[strings.ToLower(name)] = v
		case 2, 5: // array (5: += with a flag word first)
			if typ == 5 {
				if _, err := r.u32(&at); err != nil {
					return err
				}
			}
			name, err := r.asciiz(&at)
			if err != nil {
				return err
			}
			arr, err := r.array(&at)
			if err != nil {
				return err
			}
			c.Values[strings.ToLower(name)] = arr
		case 3, 4: // extern / delete class
			if _, err := r.asciiz(&at); err != nil {
				return err
			}
		default:
			return fmt.Errorf("rap: unknown entry type %d", typ)
		}
	}
	return nil
}

func (r *rapReader) scalar(at *int, sub byte) (interface{}, error) {
	switch sub {
	case 0, 4: // string, variable name
		return r.asciiz(at)
	case 1: // float
		v, err := r.u32(at)
		return float64(math.Float32frombits(v)), err
	case 2: // int32
		v, err := r.u32(at)
		return float64(int32(v)), err
	case 6: // int64
		if *at+8 > len(r.b) {
			return nil, errors.New("rap: unexpected end")
		}
		v := int64(binary.LittleEndian.Uint64(r.b[*at:]))
		*at += 8
		return float64(v), nil
	}
	return nil, fmt.Errorf("rap: unknown value type %d", sub)
}

func (r *rapReader) array(at *int) ([]interface{}, error) {
	r.depth++
	defer func() { r.depth-- }()
	if r.depth > 64 {
		return nil, errors.New("rap: arrays nested too deep")
	}
	n, err := r.cint(at)
	if err != nil {
		return nil, err
	}
	if n > 100000 {
		return nil, errors.New("rap: array too long")
	}
	out := make([]interface{}, 0, n)
	for i := 0; i < n; i++ {
		typ, err := r.u8(at)
		if err != nil {
			return nil, err
		}
		if typ == 3 {
			sub, err := r.array(at)
			if err != nil {
				return nil, err
			}
			out = append(out, sub)
			continue
		}
		v, err := r.scalar(at, typ)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ---------------------------------------------------------------- text

// parseText reads a plain config.cpp well enough for classes, inheritance and
// simple values. Preprocessor lines are skipped; macros are not expanded.
func parseText(src string) (*Class, error) {
	p := &textParser{toks: tokenize(src)}
	root := newClass("", "")
	if err := p.body(root, true); err != nil {
		return nil, err
	}
	return root, nil
}

type textParser struct {
	toks []string
	i    int
	deep int
}

func (p *textParser) next() string {
	if p.i >= len(p.toks) {
		return ""
	}
	t := p.toks[p.i]
	p.i++
	return t
}

func (p *textParser) peek() string {
	if p.i >= len(p.toks) {
		return ""
	}
	return p.toks[p.i]
}

func (p *textParser) body(c *Class, top bool) error {
	p.deep++
	defer func() { p.deep-- }()
	if p.deep > 64 {
		return errors.New("config: classes nested too deep")
	}
	for p.i < len(p.toks) {
		t := p.next()
		switch {
		case t == "}":
			if top {
				continue // unbalanced brace in the text: keep going
			}
			if p.peek() == ";" {
				p.next()
			}
			return nil
		case t == ";":
		case strings.EqualFold(t, "class"):
			name := p.next()
			parent := ""
			if p.peek() == ":" {
				p.next()
				parent = p.next()
			}
			switch p.peek() {
			case "{":
				p.next()
				sub := newClass(name, parent)
				if err := p.body(sub, false); err != nil {
					return err
				}
				c.Classes = append(c.Classes, sub)
			default: // forward declaration: class X;
				if p.peek() == ";" {
					p.next()
				}
			}
		case strings.EqualFold(t, "delete"):
			p.next()
		default:
			name := t
			isArray := false
			if p.peek() == "[" {
				p.next()
				if p.peek() == "]" {
					p.next()
				}
				isArray = true
			}
			op := p.next()
			if op != "=" && op != "+=" {
				continue // something we don't understand: skip the token
			}
			if isArray {
				c.Values[strings.ToLower(name)] = p.array()
			} else {
				c.Values[strings.ToLower(name)] = p.scalar()
			}
		}
	}
	if !top {
		return errors.New("config: class not closed")
	}
	return nil
}

func (p *textParser) scalar() interface{} {
	var parts []string
	for p.i < len(p.toks) && p.peek() != ";" && p.peek() != "}" {
		parts = append(parts, p.next())
	}
	if p.peek() == ";" {
		p.next()
	}
	s := strings.Join(parts, " ")
	if unq, ok := unquote(s); ok {
		return unq
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

func (p *textParser) array() []interface{} {
	if p.peek() != "{" {
		p.scalar()
		return nil
	}
	p.next()
	var out []interface{}
	for p.i < len(p.toks) {
		t := p.peek()
		switch t {
		case "}":
			p.next()
			if p.peek() == ";" {
				p.next()
			}
			return out
		case ",":
			p.next()
		case "{":
			out = append(out, p.array())
		default:
			p.next()
			if unq, ok := unquote(t); ok {
				out = append(out, unq)
			} else if f, err := strconv.ParseFloat(t, 64); err == nil {
				out = append(out, f)
			} else {
				out = append(out, t)
			}
		}
	}
	return out
}

func unquote(s string) (string, bool) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`), true
	}
	return "", false
}

// tokenize splits config text into identifiers/numbers, quoted strings and
// punctuation, dropping comments and preprocessor lines.
func tokenize(src string) []string {
	var out []string
	rs := []rune(src)
	for i := 0; i < len(rs); {
		c := rs[i]
		switch {
		case c == '/' && i+1 < len(rs) && rs[i+1] == '/':
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(rs) && rs[i+1] == '*':
			i += 2
			for i+1 < len(rs) && !(rs[i] == '*' && rs[i+1] == '/') {
				i++
			}
			i += 2
		case c == '#':
			// preprocessor: to the end of the line (with continuations)
			for i < len(rs) && rs[i] != '\n' {
				if rs[i] == '\\' && i+1 < len(rs) && rs[i+1] == '\n' {
					i++
				}
				i++
			}
		case unicode.IsSpace(c):
			i++
		case c == '"':
			j := i + 1
			for j < len(rs) {
				if rs[j] == '"' {
					if j+1 < len(rs) && rs[j+1] == '"' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j >= len(rs) {
				j = len(rs) - 1
			}
			out = append(out, string(rs[i:j+1]))
			i = j + 1
		case c == '+' && i+1 < len(rs) && rs[i+1] == '=':
			out = append(out, "+=")
			i += 2
		case strings.ContainsRune("{}[];:=,", c):
			out = append(out, string(c))
			i++
		default:
			j := i
			for j < len(rs) && !unicode.IsSpace(rs[j]) && !strings.ContainsRune("{}[];:=,\"", rs[j]) &&
				!(rs[j] == '/' && j+1 < len(rs) && (rs[j+1] == '/' || rs[j+1] == '*')) {
				j++
			}
			if j == i {
				j++
			}
			out = append(out, string(rs[i:j]))
			i = j
		}
	}
	return out
}
