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

// ErrAmbiguous reports a shorthand source that resolved to more than one
// template. It carries the candidates so the caller can show them.
type ErrAmbiguous struct {
	Want    string
	Matches []string
}

func (e *ErrAmbiguous) Error() string {
	return "source is ambiguous: " + e.Want
}

// Resolve maps a source argument to exactly one source present in the
// manifest.
//
// An exact match always wins, so a full path is never ambiguous even when it
// is also a suffix of a longer one. Otherwise the argument is matched as a
// trailing path suffix: "external-config.yaml" finds
// "demo/charts/sub/templates/external-config.yaml".
//
// The extension may be left off, since charts are inconsistent about .yaml and
// .yml: "external-config" matches either spelling. Matching is otherwise on
// whole path elements, so "config" does not match "external-config.yaml".
//
// Resolving to several sources is an error rather than an arbitrary choice.
func Resolve(docs []Doc, want string) (string, error) {
	sources := Sources(docs)

	for _, s := range sources {
		if s == want {
			return s, nil
		}
	}

	// Match the argument as given. If it carries no YAML extension, both
	// spellings are tried together, so a name matching one template's .yaml
	// and another's .yml is reported as ambiguous rather than picked.
	suffixes := []string{want}
	if ext := pathExt(want); ext != ".yaml" && ext != ".yml" {
		suffixes = []string{want + ".yaml", want + ".yml"}
	}

	var matches []string
	for _, s := range sources {
		for _, suffix := range suffixes {
			if strings.HasSuffix(s, "/"+suffix) {
				matches = append(matches, s)
				break
			}
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", nil // caller reports "not found" with its own suggestions
	default:
		return "", &ErrAmbiguous{Want: want, Matches: matches}
	}
}

// pathExt returns the extension of the last path element, or "" if it has none.
func pathExt(p string) string {
	base := p[strings.LastIndex(p, "/")+1:]
	if i := strings.LastIndex(base, "."); i >= 0 {
		return base[i:]
	}
	return ""
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
// suggest alternatives when Resolve finds nothing. This catches a source typed
// with the wrong directory, which suffix matching cannot resolve on its own.
func Candidates(docs []Doc, want string) []string {
	base := want[strings.LastIndex(want, "/")+1:]
	stem := strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")

	var out []string
	for _, s := range Sources(docs) {
		sBase := s[strings.LastIndex(s, "/")+1:]
		sStem := strings.TrimSuffix(strings.TrimSuffix(sBase, ".yaml"), ".yml")
		if sStem == stem || strings.HasSuffix(s, want) {
			out = append(out, s)
		}
	}
	return out
}
