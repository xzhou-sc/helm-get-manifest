package manifest

import (
	"strings"
	"testing"
)

// doc is shorthand for building expected results in table tests.
type doc struct {
	source string
	text   string
}

func got(docs []Doc) []doc {
	out := make([]doc, len(docs))
	for i, d := range docs {
		out[i] = doc{d.Source, d.Text}
	}
	return out
}

func equal(a []doc, b []doc) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []doc
	}{
		{
			name: "single document",
			in:   "---\n# Source: c/templates/cm.yaml\nkind: ConfigMap\n",
			want: []doc{{"c/templates/cm.yaml", "# Source: c/templates/cm.yaml\nkind: ConfigMap"}},
		},
		{
			name: "multiple documents",
			in:   "---\n# Source: c/templates/a.yaml\nkind: A\n---\n# Source: c/templates/b.yaml\nkind: B\n",
			want: []doc{
				{"c/templates/a.yaml", "# Source: c/templates/a.yaml\nkind: A"},
				{"c/templates/b.yaml", "# Source: c/templates/b.yaml\nkind: B"},
			},
		},
		{
			name: "one source rendering multiple documents",
			in:   "---\n# Source: c/templates/multi.yaml\nkind: A\n---\n# Source: c/templates/multi.yaml\nkind: B\n",
			want: []doc{
				{"c/templates/multi.yaml", "# Source: c/templates/multi.yaml\nkind: A"},
				{"c/templates/multi.yaml", "# Source: c/templates/multi.yaml\nkind: B"},
			},
		},
		{
			// The headline correctness case: a "---" inside a block scalar is
			// content, not a document boundary.
			name: "block scalar containing separator",
			in:   "---\n# Source: c/templates/cm.yaml\ndata:\n  script: |\n    echo start\n    ---\n    echo end\n",
			want: []doc{{
				"c/templates/cm.yaml",
				"# Source: c/templates/cm.yaml\ndata:\n  script: |\n    echo start\n    ---\n    echo end",
			}},
		},
		{
			// Helm's own splitter gets this wrong; a quoted scalar can put a
			// "---" at column zero.
			name: "quoted scalar containing separator at column zero",
			in:   "---\n# Source: c/templates/cm.yaml\ndata:\n  s: \"line1\n---\nline2\"\n",
			want: []doc{{
				"c/templates/cm.yaml",
				"# Source: c/templates/cm.yaml\ndata:\n  s: \"line1\n---\nline2\"",
			}},
		},
		{
			// A "# Source:" in a block scalar is data, not provenance.
			name: "block scalar containing source marker",
			in:   "---\n# Source: c/templates/real.yaml\ndata:\n  example: |\n    # Source: fake/templates/foo.yaml\n",
			want: []doc{{
				"c/templates/real.yaml",
				"# Source: c/templates/real.yaml\ndata:\n  example: |\n    # Source: fake/templates/foo.yaml",
			}},
		},
		{
			name: "document without provenance",
			in:   "---\nkind: Naked\n",
			want: []doc{{"", "kind: Naked"}},
		},
		{
			name: "unrelated header comments are preserved",
			in:   "---\n# Source: c/templates/a.yaml\n# a note\nkind: A\n",
			want: []doc{{"c/templates/a.yaml", "# Source: c/templates/a.yaml\n# a note\nkind: A"}},
		},
		{
			name: "separator with trailing comment",
			in:   "---\n# Source: c/templates/a.yaml\nkind: A\n--- # next\n# Source: c/templates/b.yaml\nkind: B\n",
			want: []doc{
				{"c/templates/a.yaml", "# Source: c/templates/a.yaml\nkind: A"},
				{"c/templates/b.yaml", "# Source: c/templates/b.yaml\nkind: B"},
			},
		},
		{
			name: "empty trailing document is dropped",
			in:   "---\n# Source: c/templates/a.yaml\nkind: A\n---\n",
			want: []doc{{"c/templates/a.yaml", "# Source: c/templates/a.yaml\nkind: A"}},
		},
		{
			name: "no leading separator",
			in:   "# Source: c/templates/a.yaml\nkind: A\n",
			want: []doc{{"c/templates/a.yaml", "# Source: c/templates/a.yaml\nkind: A"}},
		},
		{
			name: "empty input",
			in:   "",
			want: nil,
		},
		{
			name: "folded scalar containing separator",
			in:   "---\n# Source: c/templates/a.yaml\ndata:\n  s: >-\n    ---\n",
			want: []doc{{"c/templates/a.yaml", "# Source: c/templates/a.yaml\ndata:\n  s: >-\n    ---"}},
		},
		{
			name: "block scalar followed by more keys",
			in:   "---\n# Source: c/templates/a.yaml\ndata:\n  s: |\n    ---\nkind: A\n---\n# Source: c/templates/b.yaml\nkind: B\n",
			want: []doc{
				{"c/templates/a.yaml", "# Source: c/templates/a.yaml\ndata:\n  s: |\n    ---\nkind: A"},
				{"c/templates/b.yaml", "# Source: c/templates/b.yaml\nkind: B"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if g := got(Split(tt.in)); !equal(g, tt.want) {
				t.Errorf("Split() =\n%#v\nwant\n%#v", g, tt.want)
			}
		})
	}
}

// TestSplitPreservesBytes guards the core promise: filtering is not formatting.
func TestSplitPreservesBytes(t *testing.T) {
	// Deliberately awkward: odd indentation, mixed quoting, trailing spaces,
	// an unusual key order and a comment in the body.
	body := "apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"    name: 'single-quoted'   \n" +
		"    labels:\n" +
		"      b: \"2\"\n" +
		"      a: 1\n" +
		"data:\n" +
		"  # an inline comment\n" +
		"  key:    unquoted-value\n" +
		"  num: 010"
	in := "---\n# Source: c/templates/cm.yaml\n" + body + "\n"

	docs := Split(in)
	if len(docs) != 1 {
		t.Fatalf("got %d docs, want 1", len(docs))
	}
	if docs[0].Text != "# Source: c/templates/cm.yaml\n"+body {
		t.Errorf("document text was altered:\ngot  %q\nwant %q", docs[0].Text, body)
	}
	// Round-tripping without --clean must reproduce the input exactly.
	if out := Render(docs, false); out != in {
		t.Errorf("round trip altered manifest:\ngot  %q\nwant %q", out, in)
	}
}

func TestSources(t *testing.T) {
	in := "---\n# Source: c/templates/a.yaml\nkind: A\n" +
		"---\n# Source: c/templates/b.yaml\nkind: B\n" +
		"---\n# Source: c/templates/a.yaml\nkind: A2\n" +
		"---\nkind: NoSource\n"

	want := []string{"c/templates/a.yaml", "c/templates/b.yaml"}
	got := Sources(Split(in))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Sources() = %v, want %v (deduplicated, first-seen order)", got, want)
	}
}

func TestSelect(t *testing.T) {
	in := "---\n# Source: c/templates/a.yaml\nkind: A\n" +
		"---\n# Source: c/templates/b.yaml\nkind: B\n" +
		"---\n# Source: c/templates/a.yaml\nkind: A2\n"
	docs := Split(in)

	if n := len(Select(docs, "c/templates/a.yaml")); n != 2 {
		t.Errorf("Select() returned %d docs, want 2 (all documents from one source)", n)
	}
	if n := len(Select(docs, "c/templates/missing.yaml")); n != 0 {
		t.Errorf("Select() on unknown source returned %d docs, want 0", n)
	}
	// Exact matching: a suffix must not match in the MVP.
	if n := len(Select(docs, "a.yaml")); n != 0 {
		t.Errorf("Select() matched a bare basename, want exact matching only")
	}
}

// TestSelectSimilarNames covers the parent/subchart collision the spec calls
// out: two templates with the same basename must stay distinguishable.
func TestSelectSimilarNames(t *testing.T) {
	in := "---\n# Source: parent/templates/secret.yaml\nkind: Parent\n" +
		"---\n# Source: parent/charts/foo/templates/secret.yaml\nkind: Child\n"
	docs := Split(in)

	for _, tc := range []struct{ source, want string }{
		{"parent/templates/secret.yaml", "# Source: parent/templates/secret.yaml\nkind: Parent"},
		{"parent/charts/foo/templates/secret.yaml", "# Source: parent/charts/foo/templates/secret.yaml\nkind: Child"},
	} {
		sel := Select(docs, tc.source)
		if len(sel) != 1 || sel[0].Text != tc.want {
			t.Errorf("Select(%q) = %v, want exactly [%q]", tc.source, got(sel), tc.want)
		}
	}
}

func TestRender(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		clean bool
		want  string
	}{
		{
			name:  "default output keeps helm shape",
			in:    "---\n# Source: c/templates/a.yaml\nkind: A\n",
			clean: false,
			want:  "---\n# Source: c/templates/a.yaml\nkind: A\n",
		},
		{
			name:  "clean strips source and leading separator",
			in:    "---\n# Source: c/templates/a.yaml\napiVersion: v1\nkind: A\n",
			clean: true,
			want:  "apiVersion: v1\nkind: A\n",
		},
		{
			// The multi-document rule: separators between, none leading.
			name: "clean keeps separators between documents",
			in: "---\n# Source: c/templates/multi.yaml\nkind: A\n" +
				"---\n# Source: c/templates/multi.yaml\nkind: B\n",
			clean: true,
			want:  "kind: A\n---\nkind: B\n",
		},
		{
			name:  "clean preserves unrelated comments",
			in:    "---\n# Source: c/templates/a.yaml\n# keep me\nkind: A\n",
			clean: true,
			want:  "# keep me\nkind: A\n",
		},
		{
			name:  "clean leaves a document without provenance alone",
			in:    "---\nkind: A\n",
			clean: true,
			want:  "kind: A\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out := Render(Split(tt.in), tt.clean); out != tt.want {
				t.Errorf("Render() = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestCandidates(t *testing.T) {
	in := "---\n# Source: demo/templates/external-secret-objstore.yaml\nkind: A\n" +
		"---\n# Source: demo/templates/external-secret-db.yaml\nkind: B\n" +
		"---\n# Source: demo/templates/deployment.yaml\nkind: C\n"
	docs := Split(in)

	if c := Candidates(docs, "deployment.yaml"); len(c) != 1 || c[0] != "demo/templates/deployment.yaml" {
		t.Errorf("Candidates() = %v, want the matching full path", c)
	}
	if c := Candidates(docs, "nothing-alike.yaml"); len(c) != 0 {
		t.Errorf("Candidates() = %v, want none", c)
	}
}
