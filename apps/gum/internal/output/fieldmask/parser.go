// Package fieldmask implements a Google-style field-mask parser and projector.
//
// Grammar (EBNF):
//
//	mask     = field { "," field }
//	field    = ident [ "(" mask ")" ]
//	         | "*"
//	ident    = ( letter | "_" ) { letter | digit | "_" }
//	letter   = "a"-"z" | "A"-"Z"
//	digit    = "0"-"9"
package fieldmask

import (
	"fmt"
)

// parser holds the input string and the current read position.
type parser struct {
	s   string
	pos int
}

// peek returns the byte at the current position, or 0 at EOF.
func (p *parser) peek() byte {
	if p.pos >= len(p.s) {
		return 0
	}
	return p.s[p.pos]
}

// consume advances past the current byte and returns it.
func (p *parser) consume() byte {
	b := p.s[p.pos]
	p.pos++
	return b
}

// expect consumes b if it is the current byte; otherwise returns an error.
func (p *parser) expect(b byte) error {
	got := p.peek()
	if got == b {
		p.consume()
		return nil
	}
	if got == 0 {
		return fmt.Errorf("fieldmask: expected %q at position %d, got EOF", b, p.pos)
	}
	return fmt.Errorf("fieldmask: expected %q at position %d, got %q", b, p.pos, got)
}

// isIdentStart reports whether b can start an identifier.
func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

// isIdentCont reports whether b can continue an identifier.
func isIdentCont(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// parseIdent reads an identifier starting at the current position.
func (p *parser) parseIdent() (string, error) {
	start := p.pos
	if !isIdentStart(p.peek()) {
		got := p.peek()
		if got == 0 {
			return "", fmt.Errorf("fieldmask: expected identifier at position %d, got EOF", p.pos)
		}
		return "", fmt.Errorf("fieldmask: expected identifier at position %d, got %q", p.pos, got)
	}
	for p.pos < len(p.s) && isIdentCont(p.s[p.pos]) {
		p.pos++
	}
	return p.s[start:p.pos], nil
}

// parseMask parses "field { ',' field }". When nested is true the caller
// owns the closing ')'; otherwise the input must be fully consumed.
func (p *parser) parseMask(nested bool) ([]*node, error) {
	// Parse the first required field.
	first, err := p.parseField()
	if err != nil {
		return nil, err
	}
	nodes := []*node{first}

	// Parse any additional comma-separated fields.
	for p.peek() == ',' {
		p.consume() // eat ','

		// A trailing or doubled comma is illegal — the next token must be a field.
		if next := p.peek(); next == 0 || next == ')' || next == ',' {
			return nil, fmt.Errorf("fieldmask: unexpected %q after comma at position %d", next, p.pos)
		}
		f, err := p.parseField()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, f)
	}

	// At top level the whole input must be consumed; in nested context the
	// caller will consume the closing ')'.
	if !nested && p.pos != len(p.s) {
		return nil, fmt.Errorf("fieldmask: unexpected character %q at position %d", p.s[p.pos], p.pos)
	}
	return nodes, nil
}

// parseField parses a single field: "*" | ident [ "(" mask ")" ].
func (p *parser) parseField() (*node, error) {
	if p.peek() == '*' {
		p.consume()
		return &node{name: "*", wildcard: true}, nil
	}

	name, err := p.parseIdent()
	if err != nil {
		return nil, err
	}
	n := &node{name: name}

	if p.peek() != '(' {
		return n, nil
	}
	p.consume() // eat '('

	// Empty sub-selection is illegal.
	if p.peek() == ')' {
		return nil, fmt.Errorf("fieldmask: empty parentheses for field %q at position %d", name, p.pos)
	}
	if p.peek() == 0 {
		return nil, fmt.Errorf("fieldmask: unmatched '(' for field %q", name)
	}

	children, err := p.parseMask(true)
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, fmt.Errorf("fieldmask: unmatched '(' for field %q: %w", name, err)
	}
	n.children = children
	return n, nil
}

// Parse parses a Google-style field-mask expression and returns a Mask.
// Returns (nil, error) on any syntactic violation.
func Parse(s string) (*Mask, error) {
	if s == "" {
		return nil, fmt.Errorf("fieldmask: empty mask string")
	}
	if s[0] == ',' {
		return nil, fmt.Errorf("fieldmask: mask starts with ','")
	}
	p := &parser{s: s}
	nodes, err := p.parseMask(false)
	if err != nil {
		return nil, err
	}
	return &Mask{roots: nodes}, nil
}
