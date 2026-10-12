package scan

import (
	"strings"
)

// Segment is one simple command from a shell line.
type Segment struct {
	// Text is the source of the segment, for messages.
	Text string
	// Words are the arguments with quotes removed and redirects dropped.
	Words []string
	// Writes are the files the segment writes through > or >>.
	Writes []string
	// Substitution is set when the segment runs $(...), `...`, or <(...),
	// so its effect cannot be read from the words.
	Substitution bool
}

var devices = setOf("/dev/null", "/dev/stdout", "/dev/stderr", "/dev/tty")

// IsDevice reports a redirect target that is not a file.
func IsDevice(target string) bool {
	return devices[target] || strings.HasPrefix(target, "/dev/fd/")
}

// Parse splits a shell command into simple commands. It honours quotes, so a
// > or | inside a string is text, and it skips heredoc bodies and comments.
func Parse(command string) []Segment {
	p := parser{src: []rune(command)}
	p.run()
	return p.out
}

type parser struct {
	src      []rune
	i        int
	out      []Segment
	seg      Segment
	start    int
	buf      strings.Builder
	inWord   bool
	pending  byte // 'w' write target, 'r' read target, 'h' heredoc delimiter
	heredocs []string
}

func (p *parser) peek(offset int) rune {
	if p.i+offset < len(p.src) {
		return p.src[p.i+offset]
	}
	return 0
}

func (p *parser) word() {
	if !p.inWord {
		return
	}
	text := p.buf.String()
	p.buf.Reset()
	p.inWord = false
	switch p.pending {
	case 'w':
		if !IsDevice(text) {
			p.seg.Writes = append(p.seg.Writes, text)
		}
	case 'r':
	case 'h':
		p.heredocs = append(p.heredocs, text)
	default:
		p.seg.Words = append(p.seg.Words, text)
	}
	p.pending = 0
}

func (p *parser) end() {
	p.word()
	p.pending = 0
	end := p.i
	if end > len(p.src) {
		end = len(p.src)
	}
	if p.start > end {
		p.start = end
	}
	p.seg.Text = strings.TrimSpace(string(p.src[p.start:end]))
	if len(p.seg.Words) > 0 || len(p.seg.Writes) > 0 || p.seg.Substitution {
		p.out = append(p.out, p.seg)
	}
	p.seg = Segment{}
	p.start = p.i + 1
}

// fdPrefix drops a pending file descriptor number such as the 2 in 2>.
func (p *parser) fdPrefix() {
	if p.inWord && isDigits(p.buf.String()) {
		p.buf.Reset()
		p.inWord = false
		return
	}
	p.word()
}

func (p *parser) add(r rune) {
	p.buf.WriteRune(r)
	p.inWord = true
}

// skipBalanced copies $( ... ) or ` ... ` into the current word.
func (p *parser) skipBalanced(open, close rune) {
	p.seg.Substitution = true
	depth := 0
	for ; p.i < len(p.src); p.i++ {
		r := p.src[p.i]
		p.add(r)
		if r == open && open != close {
			depth++
		} else if r == close {
			if open == close && depth == 0 {
				depth = 1
				continue
			}
			depth--
			if depth == 0 {
				return
			}
		}
	}
}

func (p *parser) skipHeredocs() {
	for _, delimiter := range p.heredocs {
		for p.i+1 < len(p.src) {
			lineStart := p.i + 1
			lineEnd := lineStart
			for lineEnd < len(p.src) && p.src[lineEnd] != '\n' {
				lineEnd++
			}
			line := strings.TrimLeft(string(p.src[lineStart:lineEnd]), "\t")
			p.i = lineEnd
			if line == delimiter {
				break
			}
		}
	}
	p.heredocs = nil
	p.start = p.i + 1
}

func (p *parser) run() {
	var quote rune
	for ; p.i < len(p.src); p.i++ {
		r := p.src[p.i]
		if quote == '\'' {
			if r == '\'' {
				quote = 0
			} else {
				p.add(r)
			}
			continue
		}
		if quote == '"' {
			switch {
			case r == '\\' && strings.ContainsRune("$`\"\\\n", p.peek(1)) && p.peek(1) != 0:
				p.i++
				if p.src[p.i] != '\n' {
					p.add(p.src[p.i])
				}
			case r == '"':
				quote = 0
			case r == '`':
				p.skipBalanced('`', '`')
			case r == '$' && p.peek(1) == '(':
				p.skipBalanced('(', ')')
			default:
				p.add(r)
			}
			continue
		}
		switch {
		case r == '\\':
			if p.peek(1) == '\n' {
				p.i++
				continue
			}
			if p.i+1 < len(p.src) {
				p.i++
				p.add(p.src[p.i])
			}
		case r == '\'' || r == '"':
			quote = r
			p.inWord = true
		case r == '#' && !p.inWord:
			for p.i+1 < len(p.src) && p.src[p.i+1] != '\n' {
				p.i++
			}
		case r == ' ' || r == '\t' || r == '\r':
			p.word()
		case r == '\n':
			p.end()
			if len(p.heredocs) > 0 {
				p.skipHeredocs()
			}
		case r == ';' || r == '(' || r == ')':
			p.end()
		case r == '&':
			switch p.peek(1) {
			case '&':
				p.end()
				p.i++
				p.start = p.i + 1
			case '>':
				p.word()
				p.i++
				if p.peek(1) == '>' {
					p.i++
				}
				p.pending = 'w'
			default:
				p.end()
			}
		case r == '|':
			p.end()
			if next := p.peek(1); next == '|' || next == '&' {
				p.i++
				p.start = p.i + 1
			}
		case r == '>':
			p.fdPrefix()
			if next := p.peek(1); next == '>' || next == '|' {
				p.i++
			}
			if p.peek(1) == '&' {
				p.i++
				if next := p.peek(1); next == '-' || isDigit(next) {
					for next := p.peek(1); next == '-' || isDigit(next); next = p.peek(1) {
						p.i++
					}
					continue
				}
			}
			p.pending = 'w'
		case r == '<':
			p.fdPrefix()
			switch {
			case p.peek(1) == '<' && p.peek(2) == '<':
				p.i += 2
				p.pending = 'r'
			case p.peek(1) == '<':
				p.i++
				if p.peek(1) == '-' {
					p.i++
				}
				p.pending = 'h'
			case p.peek(1) == '&':
				p.i++
				for next := p.peek(1); next == '-' || isDigit(next); next = p.peek(1) {
					p.i++
				}
			case p.peek(1) == '(':
				p.seg.Substitution = true
				p.i++
				p.skipBalanced('(', ')')
			default:
				p.pending = 'r'
			}
		case r == '`':
			p.skipBalanced('`', '`')
		case r == '$' && p.peek(1) == '(':
			p.skipBalanced('(', ')')
		default:
			p.add(r)
		}
	}
	p.end()
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !isDigit(r) {
			return false
		}
	}
	return true
}
