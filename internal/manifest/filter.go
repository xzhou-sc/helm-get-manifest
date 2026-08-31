package manifest

import "strings"

// Sources lists the distinct "# Source:" values in the manifest, in first-seen
// order. Documents without provenance are skipped.
func Sources(docs []Doc) []string {
	seen := make(map[string]bool, len(docs))
	var out []string
	for _, d := range docs {
		if d.Source == "" || seen[d.Source] {
			continue
		}
		seen[d.Source] = true
		out = append(out, d.Source)
	}
	return out
}

// Select returns the documents whose source matches exactly.
func Select(docs []Doc, source string) []Doc {
	var out []Doc
	for _, d := range docs {
		if d.Source == source {
			out = append(out, d)
		}
	}
	return out
}

// Render writes documents back out as a YAML stream.
//
// Without clean, output matches Helm's own shape: each document preceded by a
// "---" separator line, text untouched.
//
// With clean, Helm's "# Source:" line is dropped and the leading separator is
// omitted, but separators between documents are kept so a source that renders
// several resources still yields a valid multi-document stream.
func Render(docs []Doc, clean bool) string {
	var b strings.Builder
	for i, d := range docs {
		text := d.Text
		if clean {
			text = dropSourceComment(d)
			if i > 0 {
				b.WriteString("---\n")
			}
		} else {
			b.WriteString("---\n")
		}
		// Text holds the document's lines without the newline that terminates
		// the last one, so exactly one is added back here. Any blank line the
		// document ended with is inside Text and survives.
		b.WriteString(text)
		b.WriteString("\n")
	}
	return b.String()
}

// dropSourceComment removes the Helm provenance line from a document's header,
// leaving every other line, including unrelated comments, untouched.
func dropSourceComment(d Doc) string {
	if d.Source == "" {
		return d.Text
	}
	lines := splitLines(d.Text)
	for i, line := range lines {
		if i >= len(d.Header) {
			break
		}
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "# Source:") {
			lines = append(lines[:i:i], lines[i+1:]...)
			break
		}
	}
	// A provenance-only header can leave blank lines at the top.
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// Candidates returns sources whose last path element matches that of want, to
// suggest alternatives when an exact match fails.
func Candidates(docs []Doc, want string) []string {
	base := want[strings.LastIndex(want, "/")+1:]
	var out []string
	for _, s := range Sources(docs) {
		if s[strings.LastIndex(s, "/")+1:] == base || strings.HasSuffix(s, want) {
			out = append(out, s)
		}
	}
	return out
}
