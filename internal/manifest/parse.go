// Package manifest splits a rendered Helm manifest into documents and reads
// the "# Source:" provenance comment Helm emits at the top of each one.
//
// Everything here works on the original text. Documents are returned as the
// exact byte range they occupied in the input, so filtering never reformats,
// reorders or re-quotes anything.
package manifest

import "strings"

// Doc is a single YAML document within a rendered manifest.
type Doc struct {
	// Source is the value of the "# Source:" comment in the document header,
	// or "" if the document has none.
	Source string
	// Text is the document exactly as it appeared in the input, excluding only
	// the leading "---" separator line. Header comments, including Helm's own
	// "# Source:" line, are kept so that rendering without --clean reproduces
	// the input byte for byte.
	Text string
	// Header holds the comment lines between the separator and the body, in
	// order, each without a trailing newline. Used to drop only Helm's own
	// "# Source:" line while preserving unrelated comments.
	Header []string
}

// Split parses a rendered manifest into documents in input order.
//
// A "---" line only starts a new document when it appears at the top level of
// the stream: not indented, and not inside a block or quoted scalar. This is
// what keeps a "---" inside a ConfigMap script from being mistaken for a
// document separator.
func Split(in string) []Doc {
	lines := splitLines(in)
	var docs []Doc

	start := 0
	for i := 0; i < len(lines); i++ {
		if !isSeparator(lines[i]) || inScalar(lines[:i]) {
			continue
		}
		if d, ok := newDoc(lines[start:i]); ok {
			docs = append(docs, d)
		}
		start = i + 1
	}
	if d, ok := newDoc(lines[start:]); ok {
		docs = append(docs, d)
	}
	return docs
}

// isSeparator reports whether a line is a top-level YAML document separator.
// YAML allows trailing content after "---" (a comment, or an inline node), but
// the marker itself must start at column zero.
func isSeparator(line string) bool {
	if !strings.HasPrefix(line, "---") {
		return false
	}
	rest := line[3:]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '#' || rest == "\r"
}

// inScalar reports whether a column-zero "---" following the given lines is
// scalar content rather than a document separator.
//
// Only a quoted scalar can absorb it. Block scalar ("|" / ">") content must be
// indented deeper than its key, so an unindented "---" necessarily ends the
// block and starts a new document; block state is still tracked here so that
// a "#" or a quote inside block content cannot confuse the quote scan.
func inScalar(prev []string) bool {
	blockIndent := -1 // indentation of the key owning an open block scalar
	inQuote := false
	var quoteChar byte

	for _, line := range prev {
		if inQuote {
			if closesQuote(line, quoteChar) {
				inQuote = false
			}
			continue
		}

		if blockIndent >= 0 {
			// Blank lines belong to the block scalar; deeper indentation
			// continues it, anything else ends it. Content must be indented
			// deeper than its key, so a line at column zero always closes the
			// scalar, which is what lets a following "---" be recognised.
			if strings.TrimSpace(line) == "" || indent(line) > blockIndent {
				continue
			}
			blockIndent = -1
		}

		if c, ok := opensQuote(line); ok {
			inQuote, quoteChar = true, c
			continue
		}
		if opensBlockScalar(line) {
			blockIndent = indent(line)
		}
	}
	return inQuote
}

// opensBlockScalar reports whether a line introduces a "|" or ">" block scalar,
// i.e. ends with the indicator plus optional chomping/indentation modifiers.
func opensBlockScalar(line string) bool {
	s := strings.TrimSpace(stripComment(line))
	i := strings.LastIndexAny(s, "|>")
	if i < 0 {
		return false
	}
	// Everything after the indicator may only be chomping/indent modifiers.
	for _, r := range s[i+1:] {
		if r != '-' && r != '+' && (r < '0' || r > '9') {
			return false
		}
	}
	// The indicator must follow a mapping value or sequence entry.
	before := strings.TrimSpace(s[:i])
	return strings.HasSuffix(before, ":") || before == "-" || strings.HasSuffix(before, " -")
}

// opensQuote reports whether a line leaves a quoted scalar unterminated.
func opensQuote(line string) (byte, bool) {
	s := stripComment(line)
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote == 0 {
			if c == '"' || c == '\'' {
				quote = c
			}
			continue
		}
		switch {
		case quote == '"' && c == '\\':
			i++ // escaped character
		case c == quote:
			if quote == '\'' && i+1 < len(s) && s[i+1] == '\'' {
				i++ // '' is an escaped quote
				continue
			}
			quote = 0
		}
	}
	return quote, quote != 0
}

// closesQuote reports whether a continuation line terminates an open quote.
func closesQuote(line string, quote byte) bool {
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote == '"' && c == '\\' {
			i++
			continue
		}
		if c == quote {
			if quote == '\'' && i+1 < len(line) && line[i+1] == '\'' {
				i++
				continue
			}
			return true
		}
	}
	return false
}

// stripComment removes a trailing YAML comment, ignoring '#' inside quotes.
func stripComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if quote == '"' && c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i]
		}
	}
	return line
}

func indent(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return len(line)
}

// newDoc builds a Doc from the lines between two separators, reporting false
// when the span holds no content at all.
func newDoc(lines []string) (Doc, bool) {
	// Drop leading blank lines; they carry no content and belong to the
	// separator, not the document.
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return Doc{}, false
	}

	// Header comments run until the first non-comment, non-blank line. Only
	// these are eligible to carry Helm provenance.
	var header []string
	body := 0
	for ; body < len(lines); body++ {
		t := strings.TrimSpace(lines[body])
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "#") {
			break
		}
		header = append(header, lines[body])
	}
	if body == len(lines) && len(header) == 0 {
		return Doc{}, false
	}

	return Doc{
		Source: sourceOf(header),
		Text:   strings.Join(lines, "\n"),
		Header: header,
	}, true
}

// sourceOf extracts the Helm provenance path from a document's header
// comments. Only a header comment can be provenance, so a "# Source:" inside a
// block scalar is never seen here.
func sourceOf(header []string) string {
	const marker = "# Source:"
	for _, line := range header {
		t := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(t, marker); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// splitLines splits on newlines without allocating a trailing empty element for
// the final newline that terminates the last line. A blank line before EOF is
// preserved, since it is part of the document's original bytes.
func splitLines(in string) []string {
	if in == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(in, "\n"), "\n")
}
