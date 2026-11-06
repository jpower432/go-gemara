// SPDX-License-Identifier: Apache-2.0

package gemara

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func requireDocumentError(t *testing.T, err error, kind, path string) *DocumentError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var de *DocumentError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DocumentError, got %T: %v", err, err)
	}
	if de.Kind != kind {
		t.Fatalf("kind = %q, want %q (%v)", de.Kind, kind, err)
	}
	if de.Path != path {
		t.Fatalf("path = %q, want %q (%v)", de.Path, path, err)
	}
	return de
}

func TestReadDocumentRetainsFutureV1(t *testing.T) {
	raw := []byte(`{"metadata":{"id":"synthetic","type":"FutureCatalog","gemara-version":"v1.99.0-rc.1+build.7"},"future":{"counter":900719925474099312345,"state":"future-state"}}`)
	d, err := ReadDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Metadata().Type != "FutureCatalog" {
		t.Fatal(d.Metadata())
	}
	if !bytes.Equal(d.Raw(), raw) {
		t.Fatal("source bytes changed")
	}
	out, err := d.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("900719925474099312345")) {
		t.Fatal(string(out))
	}
	copyRaw := d.Raw()
	copyRaw[0] = 'x'
	if !bytes.Equal(d.Raw(), raw) {
		t.Fatal("mutable raw accessor")
	}
}

func TestReadDocumentAcceptsEveryV1Version(t *testing.T) {
	for _, version := range []string{
		"1.2.0",
		"1.3.0",
		"1.4.1",
		"1.5.0",
		"1.6.0",
		"v1.99.0-rc.1+build.7",
		"1.5.0+build.9",
		"1.5.0-0",
	} {
		t.Run(version, func(t *testing.T) {
			for name, raw := range map[string][]byte{
				"yaml": []byte("metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: " + version + "\n"),
				"json": []byte(`{"metadata":{"id":"synthetic","type":"FutureCatalog","gemara-version":"` + version + `"}}`),
			} {
				t.Run(name, func(t *testing.T) {
					d, err := ReadDocument(raw)
					if err != nil {
						t.Fatal(err)
					}
					metadata := d.Metadata()
					if metadata.ID != "synthetic" || metadata.Type != "FutureCatalog" || metadata.GemaraVersion != version {
						t.Fatalf("metadata = %+v", metadata)
					}
					if !bytes.Equal(d.Raw(), raw) {
						t.Fatal("raw bytes changed")
					}
				})
			}
		})
	}
}

func TestReadDocumentVersionErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		kind string
		path string
	}{
		{name: "missing", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n", kind: "missing_version", path: "/metadata/gemara-version"},
		{name: "empty", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: ''\n", kind: "missing_version", path: "/metadata/gemara-version"},
		{name: "incomplete", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.5'\n", kind: "malformed_version", path: "/metadata/gemara-version"},
		{name: "leading zero minor", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.05.0'\n", kind: "malformed_version", path: "/metadata/gemara-version"},
		{name: "leading zero prerelease", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.5.0-01'\n", kind: "malformed_version", path: "/metadata/gemara-version"},
		{name: "major two", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '2.0.0'\n", kind: "unsupported_major", path: "/metadata/gemara-version"},
		{name: "v major two", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: 'v2.0.0'\n", kind: "unsupported_major", path: "/metadata/gemara-version"},
		{name: "numeric version", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: 1.5\n", kind: "invalid_metadata", path: "/metadata/gemara-version"},
		{name: "timestamp version", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: 2026-11-02T00:00:00Z\n", kind: "invalid_metadata", path: "/metadata/gemara-version"},
		{name: "numeric id", raw: "metadata:\n  id: 7\n  type: FutureCatalog\n  gemara-version: '1.5.0'\n", kind: "invalid_metadata", path: "/metadata/id"},
		{name: "numeric type", raw: "metadata:\n  id: synthetic\n  type: 7\n  gemara-version: '1.5.0'\n", kind: "invalid_metadata", path: "/metadata/type"},
		{name: "numeric optional version", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.5.0'\n  version: 7\n", kind: "invalid_metadata", path: "/metadata/version"},
		{name: "scalar metadata", raw: "metadata: synthetic\n", kind: "invalid_metadata", path: "/metadata"},
		{name: "missing metadata", raw: "future: true\n", kind: "invalid_metadata", path: "/metadata"},
		{name: "empty metadata", raw: "metadata: {}\n", kind: "invalid_metadata", path: "/metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadDocument([]byte(tc.raw))
			_ = requireDocumentError(t, err, tc.kind, tc.path)
		})
	}
}

func TestReadDocumentRejectsAmbiguousInput(t *testing.T) {
	base := `"metadata":{"id":"synthetic","type":"FutureCatalog","gemara-version":"1.5.0"}`
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "duplicate nested key", raw: `{` + base + `,"outer":{"same":1,"same":2}}`},
		{name: "duplicate metadata", raw: `{` + base + `,"metadata":{"id":"other","type":"FutureCatalog","gemara-version":"1.5.0"}}`},
		{name: "two json documents", raw: `{` + base + `} {` + base + `}`},
		{name: "second yaml document", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.5.0'\n---\nmetadata:\n  id: other\n  type: FutureCatalog\n  gemara-version: '1.5.0'\n"},
		{name: "trailing garbage", raw: `{` + base + `} garbage`},
		{name: "null root", raw: "null\n"},
		{name: "empty document", raw: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadDocument([]byte(tc.raw))
			if err == nil {
				t.Fatal("expected an error")
			}
			var de *DocumentError
			if !errors.As(err, &de) {
				t.Fatalf("expected *DocumentError, got %T: %v", err, err)
			}
		})
	}
}

func TestReadDocumentPreservesExactNumbers(t *testing.T) {
	raw := []byte(`metadata:
  id: synthetic
  type: FutureCatalog
  gemara-version: '1.6.0'
numbers:
  big: 900719925474099312345
  decimal: 0.1000000000000000000001
  hex: 0x10
  underscore: 1_000
  negative: -42
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
		"900719925474099312345",
		"0.1000000000000000000001",
		"16",
		"1000",
		"-42",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Fatalf("JSON missing %q: %s", want, out)
		}
	}
}

func TestReadDocumentRetainsUnknownAndAliasedContent(t *testing.T) {
	raw := []byte(`metadata:
  id: synthetic
  type: FutureCatalog
  gemara-version: '1.6.0'
items:
  - &item
    name: first
    future: {state: future-state}
  - *item
`)
	d, err := ReadDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := d.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(out), `"future-state"`); got != 2 {
		t.Fatalf("alias was not preserved: %s", out)
	}
}

func TestDocumentJSONRefusesUnsupportedValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		path string
	}{
		{name: "custom tag", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.6.0'\nvalue: !future thing\n", path: "/value"},
		{name: "timestamp", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.6.0'\nvalue: 2026-11-02T00:00:00Z\n", path: "/value"},
		{name: "binary", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.6.0'\nvalue: !!binary aGVsbG8=\n", path: "/value"},
		{name: "non-string key", raw: "metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.6.0'\nvalues:\n  1: one\n", path: "/values/1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := ReadDocument([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.JSON()
			_ = requireDocumentError(t, err, "unsupported_value", tc.path)
		})
	}
}

func TestDocumentAccessorsAreImmutable(t *testing.T) {
	raw := []byte("metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.5.0'\nvalue: original\n")
	d, err := ReadDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	originalRaw := bytes.Clone(raw)
	originalJSON, err := d.JSON()
	if err != nil {
		t.Fatal(err)
	}

	raw[0] = 'x'
	node := d.Node()
	node.Content[0].Content[0].Value = "changed"
	returned := d.Raw()
	returned[0] = 'y'

	if !bytes.Equal(d.Raw(), originalRaw) {
		t.Fatal("Raw exposed internal bytes")
	}
	if again, err := d.JSON(); err != nil || !bytes.Equal(again, originalJSON) {
		t.Fatalf("JSON changed after caller mutation: %s (%v)", again, err)
	}
	fresh := d.Node()
	if fresh.Content[0].Content[0].Value != "metadata" {
		t.Fatal("Node exposed internal AST")
	}
}

func TestDocumentNodeCopiesAliases(t *testing.T) {
	raw := []byte("metadata:\n  id: synthetic\n  type: FutureCatalog\n  gemara-version: '1.5.0'\nitems:\n  - &item {name: first}\n  - *item\n")
	d, err := ReadDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	node := d.Node()
	sequence := node.Content[0].Content[3]
	if sequence.Kind != 0 {
		second := sequence.Content[1]
		if second.Alias == nil {
			t.Fatal("alias link was lost")
		}
		second.Alias.Value = "changed"
	}
	if alias := d.Node().Content[0].Content[3].Content[1].Alias; alias.Value == "changed" {
		t.Fatal("Node copy shares alias targets with the document")
	}
}
