// Package golden loads golden test fixtures from JSON or YAML and interpolates
// their templates.
//
// A fixture document is either a sequence of cases or a mapping with a single
// "cases" key:
//
//	cases:
//	  - name: greeting
//	    template: "Hello, {{.name}}!"
//	    values:
//	      name: SoroVista
//	    expected: "Hello, SoroVista!"
//
// Templates are Go text/template expressions with missing keys treated as
// errors, so a fixture that references an undefined value fails loudly instead
// of rendering "<no value>".
//
// Both parsers are written to be fuzz-safe: malformed input returns an error,
// never panics. Deserialize rejects oversized documents and Interpolate bounds
// its output, so a pathological fixture cannot exhaust memory.
package golden

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"go.yaml.in/yaml/v3"
)

const (
	// MaxDocumentBytes bounds a fixture document. Golden fixtures are small
	// by nature, so anything larger is a mistake or hostile input.
	MaxDocumentBytes = 1 << 20 // 1 MiB

	// MaxOutputBytes bounds one interpolated result. A template that expands
	// past this limit returns ErrOutputTooLarge instead of growing without
	// bound.
	MaxOutputBytes = 1 << 20 // 1 MiB
)

var (
	// ErrDocumentTooLarge is returned when a fixture document exceeds
	// MaxDocumentBytes.
	ErrDocumentTooLarge = errors.New("golden: document exceeds size limit")

	// ErrNoCases is returned when a fixture document contains no cases.
	ErrNoCases = errors.New("golden: document contains no cases")

	// ErrOutputTooLarge is returned when an interpolated result exceeds
	// MaxOutputBytes.
	ErrOutputTooLarge = errors.New("golden: interpolated output exceeds size limit")
)

// Case is one golden fixture: a template, the values it is rendered with, and
// the output it is expected to produce.
type Case struct {
	// Name identifies the case in test output.
	Name string `json:"name" yaml:"name"`
	// Template is a Go text/template expression.
	Template string `json:"template" yaml:"template"`
	// Values are the data the template is executed against.
	Values map[string]any `json:"values,omitempty" yaml:"values,omitempty"`
	// Expected is the output the rendered template must equal.
	Expected string `json:"expected,omitempty" yaml:"expected,omitempty"`
}

// Document is a parsed fixture file.
type Document struct {
	Cases []Case `json:"cases" yaml:"cases"`
}

// Deserialize parses a fixture document written in JSON or YAML. The format is
// detected from the first non-space byte: "{" or "[" means JSON, anything else
// is parsed as YAML. The returned error wraps the parser's own error, so a
// caller can report where in the document parsing failed.
func Deserialize(data []byte) (Document, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Document{}, errors.New("golden: empty document")
	}
	if len(data) > MaxDocumentBytes {
		return Document{}, ErrDocumentTooLarge
	}

	var doc Document
	var err error
	if trimmed := bytes.TrimSpace(data); trimmed[0] == '{' || trimmed[0] == '[' {
		doc, err = decodeJSON(trimmed)
		if err != nil {
			return Document{}, fmt.Errorf("golden: decode JSON: %w", err)
		}
	} else {
		doc, err = decodeYAML(data)
		if err != nil {
			return Document{}, fmt.Errorf("golden: decode YAML: %w", err)
		}
	}

	if err := doc.validate(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func decodeJSON(data []byte) (Document, error) {
	var doc Document
	if data[0] == '[' {
		if err := json.Unmarshal(data, &doc.Cases); err != nil {
			return Document{}, err
		}
		return doc, nil
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// decodeYAML accepts either a sequence of cases or a mapping with a "cases"
// key. It decodes through a node first so the two shapes can be told apart
// without relying on one shape's type error to fall back to the other.
func decodeYAML(data []byte) (Document, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Document{}, err
	}
	if len(root.Content) == 0 {
		return Document{}, errors.New("no document")
	}

	var doc Document
	switch node := root.Content[0]; node.Kind {
	case yaml.SequenceNode:
		if err := node.Decode(&doc.Cases); err != nil {
			return Document{}, err
		}
	case yaml.MappingNode:
		if err := node.Decode(&doc); err != nil {
			return Document{}, err
		}
	default:
		return Document{}, errors.New(`expected a sequence of cases or a mapping with a "cases" key`)
	}
	return doc, nil
}

func (d Document) validate() error {
	if len(d.Cases) == 0 {
		return ErrNoCases
	}
	for i, c := range d.Cases {
		if strings.TrimSpace(c.Name) == "" {
			return fmt.Errorf("golden: case %d: missing name", i)
		}
		if c.Template == "" {
			return fmt.Errorf("golden: case %q: missing template", c.Name)
		}
	}
	return nil
}

// Interpolate renders tmpl with values. A missing key is an error rather than
// an empty string, and output beyond MaxOutputBytes returns ErrOutputTooLarge.
// A malformed template returns the parser's error; neither case panics.
func Interpolate(tmpl string, values map[string]any) (string, error) {
	t, err := template.New("golden").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("golden: parse template: %w", err)
	}
	if values == nil {
		values = map[string]any{}
	}

	out := limitedBuffer{limit: MaxOutputBytes}
	if err := t.Execute(&out, values); err != nil {
		return "", fmt.Errorf("golden: execute template: %w", err)
	}
	return out.String(), nil
}

// limitedBuffer rejects writes that would take it past limit, so a template
// cannot grow the result without bound.
type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, ErrOutputTooLarge
	}
	return b.Buffer.Write(p)
}
