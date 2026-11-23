package golden

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeserializeValidFixtures(t *testing.T) {
	// These files are the valid fixtures the fuzz targets in pkg/fuzz seed
	// their corpora from, so they are exercised here as ordinary tests too.
	files := []struct {
		path      string
		wantCases int
	}{
		{"testdata/valid.yaml", 3},
		{"testdata/valid.json", 2},
	}

	for _, f := range files {
		t.Run(f.path, func(t *testing.T) {
			data, err := os.ReadFile(f.path)
			require.NoError(t, err)

			doc, err := Deserialize(data)
			require.NoError(t, err)
			require.Len(t, doc.Cases, f.wantCases)

			for _, c := range doc.Cases {
				out, err := Interpolate(c.Template, c.Values)
				require.NoError(t, err, "case %q", c.Name)
				assert.Equal(t, c.Expected, out, "case %q", c.Name)
			}
		})
	}
}

func TestDeserializeAcceptedShapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{
			name: "json document",
			in:   `{"cases":[{"name":"a","template":"x"}]}`,
		},
		{
			name: "json array",
			in:   `[{"name":"a","template":"x"}]`,
		},
		{
			name: "yaml document",
			in:   "cases:\n  - name: a\n    template: x\n",
		},
		{
			name: "yaml sequence",
			in:   "- name: a\n  template: x\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Deserialize([]byte(tt.in))
			require.NoError(t, err)
			require.Len(t, doc.Cases, 1)
			assert.Equal(t, "a", doc.Cases[0].Name)
		})
	}
}

func TestDeserializeRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"whitespace only", " \n\t"},
		{"truncated json", `{"cases":[`},
		{"truncated yaml", "cases:\n  - name: [unclosed\n"},
		{"yaml mapping without cases", "other: value\n"},
		{"json scalar", `"plain"`},
		{"yaml scalar", "plain\n"},
		{"case without name", "cases:\n  - template: x\n"},
		{"case without template", "cases:\n  - name: a\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Deserialize([]byte(tt.in))
			assert.Error(t, err)
		})
	}
}

func TestDeserializeEmptyCases(t *testing.T) {
	for _, in := range []string{`{"cases":[]}`, "cases: []\n"} {
		_, err := Deserialize([]byte(in))
		assert.ErrorIs(t, err, ErrNoCases)
	}
}

func TestDeserializeOversizedDocument(t *testing.T) {
	data := make([]byte, MaxDocumentBytes+1)
	_, err := Deserialize(data)
	assert.ErrorIs(t, err, ErrDocumentTooLarge)
}

func TestInterpolate(t *testing.T) {
	tests := []struct {
		name   string
		tmpl   string
		values map[string]any
		want   string
	}{
		{"field", "Hello, {{.name}}!", map[string]any{"name": "world"}, "Hello, world!"},
		{"number", "{{.n}}", map[string]any{"n": 7}, "7"},
		{"nil values", "constant", nil, "constant"},
		{"empty template", "", map[string]any{"x": 1}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Interpolate(tt.tmpl, tt.values)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInterpolateRejectsMalformedTemplates(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
	}{
		{"unclosed action", "{{.name"},
		{"unknown field", "{{.missing}}"},
		{"bad syntax", "{{if}}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Interpolate(tt.tmpl, map[string]any{"name": "x"})
			assert.Error(t, err)
			assert.Empty(t, out)
		})
	}
}

func TestInterpolateBoundsOutput(t *testing.T) {
	_, err := Interpolate("{{.value}}", map[string]any{
		"value": strings.Repeat("a", MaxOutputBytes+1),
	})
	assert.ErrorIs(t, err, ErrOutputTooLarge)
}

func TestInterpolateRecursiveTemplateTerminates(t *testing.T) {
	// text/template bounds template depth; this must return an error rather
	// than recurse forever.
	_, err := Interpolate(`{{define "x"}}{{template "x"}}{{end}}{{template "x"}}`, nil)
	assert.Error(t, err)
}
