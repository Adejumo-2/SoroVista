// Package fuzz holds the seed corpus and shared checks for the golden fixture
// fuzz targets. The targets process arbitrary YAML, JSON and template text,
// and assert that malformed input is rejected with an error rather than a
// panic, an infinite loop or unbounded memory growth.
//
// The Fuzz* entry points themselves are declared in fuzz_test.go: Go only
// discovers fuzz targets in _test.go files, so a target in this file would
// compile but never run under `go test -fuzz`. Run one with, for example:
//
//	go test -fuzz=FuzzGoldenDeserializer -fuzztime=10s ./pkg/fuzz
package fuzz

import (
	"testing"

	"github.com/sorotrail/sorovista/pkg/golden"
)

type goldenSeed struct {
	name  string
	data  []byte
	valid bool
}

// goldenSeeds covers both supported encodings, both accepted document shapes
// and the corruptions the deserializer must reject. The valid cases mirror
// pkg/golden/testdata.
var goldenSeeds = []goldenSeed{
	{
		name: "valid json document",
		data: []byte(`{"cases":[
			{"name":"greeting","template":"Hello, {{.name}}!","values":{"name":"SoroVista"},"expected":"Hello, SoroVista!"}
		]}`),
		valid: true,
	},
	{
		name:  "valid json array",
		data:  []byte(`[{"name":"ledger","template":"{{.ledger}}","values":{"ledger":42},"expected":"42"}]`),
		valid: true,
	},
	{
		name:  "valid yaml document",
		data:  []byte("cases:\n  - name: greeting\n    template: \"Hello, {{.name}}!\"\n    values:\n      name: SoroVista\n    expected: \"Hello, SoroVista!\"\n"),
		valid: true,
	},
	{
		name:  "valid yaml sequence",
		data:  []byte("- name: constant\n  template: constant\n  expected: constant\n"),
		valid: true,
	},
	{name: "empty", data: nil},
	{name: "truncated json", data: []byte(`{"cases":[`)},
	{name: "truncated yaml", data: []byte("cases:\n  - name: [unclosed\n")},
	{name: "corrupted yaml", data: []byte("cases:\n\t- name: tab indent\n")},
	{name: "yaml scalar", data: []byte("just a string\n")},
	{name: "json scalar", data: []byte(`"just a string"`)},
	{name: "case without name", data: []byte("cases:\n  - template: x\n")},
	{name: "case without template", data: []byte("cases:\n  - name: x\n")},
}

type templateSeed struct {
	name  string
	tmpl  string
	value string
}

var templateSeeds = []templateSeed{
	{name: "field", tmpl: "Hello, {{.name}}!", value: "SoroVista"},
	{name: "literal", tmpl: "constant", value: "ignored"},
	{name: "empty", tmpl: "", value: ""},
	{name: "unclosed action", tmpl: "{{.name", value: "x"},
	{name: "missing key", tmpl: "{{.missing}}", value: "x"},
	{name: "bad syntax", tmpl: "{{if}}", value: "x"},
	{name: "recursive template", tmpl: `{{define "x"}}{{template "x"}}{{end}}{{template "x"}}`, value: ""},
}

// checkGoldenDeserializer is the body shared by the fuzz target and its seed
// test. It enforces the invariant that a document only parses when it has at
// least one named, templated case.
func checkGoldenDeserializer(t *testing.T, data []byte) (golden.Document, error) {
	t.Helper()

	doc, err := golden.Deserialize(data)
	if err != nil {
		return golden.Document{}, err
	}
	if len(doc.Cases) == 0 {
		t.Fatalf("Deserialize accepted a document with no cases: %q", data)
	}
	for _, c := range doc.Cases {
		if c.Name == "" || c.Template == "" {
			t.Fatalf("Deserialize accepted an incomplete case: %q", data)
		}
		// A malformed template inside a parseable document is an expected
		// error; only a panic would be a bug here.
		_, _ = golden.Interpolate(c.Template, c.Values)
	}
	return doc, nil
}

// checkInterpolate is the body shared by the fuzz target and its seed test.
func checkInterpolate(t *testing.T, tmpl, value string) (string, error) {
	t.Helper()

	out, err := golden.Interpolate(tmpl, map[string]any{
		"name":  value,
		"value": value,
	})
	if err != nil {
		return "", err
	}
	if len(out) > golden.MaxOutputBytes {
		t.Fatalf("Interpolate produced %d bytes, over the %d byte cap", len(out), golden.MaxOutputBytes)
	}
	return out, nil
}
