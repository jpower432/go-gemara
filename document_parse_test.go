// SPDX-License-Identifier: Apache-2.0

package gemara

import (
	"bytes"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDocumentJSONConvertsYAMLScalars(t *testing.T) {
	raw := []byte(`metadata:
  id: synthetic
  type: FutureCatalog
  gemara-version: '1.6.0'
values:
  yes: true
  nothing: null
  half: .5
  whole: 5.
  exponent: +1.25e+03
  octal: 0o17
  binary: 0b101
  text: "a < b"
`)
	d, err := ReadDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := d.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"yes":true`,
		`"nothing":null`,
		`"half":0.5`,
		`"whole":5.0`,
		`"exponent":1.25e3`,
		`"octal":15`,
		`"binary":5`,
		`"text":"a \u003c b"`,
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Fatalf("JSON missing %q: %s", want, out)
		}
	}
}

func TestDocumentCycleIsDetected(t *testing.T) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "self"}
	alias := &yaml.Node{Kind: yaml.AliasNode, Value: "root", Alias: root}
	root.Content = []*yaml.Node{key, alias}
	d := &Document{node: &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}}

	copied := d.Node()
	rootCopy := copied.Content[0]
	if rootCopy.Content[1].Alias != rootCopy {
		t.Fatal("Node copy alias does not point into the copy")
	}

	_, err := d.JSON()
	requireDocumentError(t, err, "unsupported_value", "/self")
}
