// SPDX-License-Identifier: Apache-2.0

package gemara

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document is one parsed Gemara artifact that retains its original bytes and
// an immutable YAML syntax tree. Reading does not project the document into a
// generated model; callers that need a typed view use Project.
type Document struct {
	raw      []byte
	node     *yaml.Node
	metadata DocumentMetadata
}

// DocumentMetadata is the artifact metadata retained by ReadDocument. Values
// are the original strings from the document, not normalized forms.
type DocumentMetadata struct {
	ID            string
	Type          string
	GemaraVersion string
	Version       string
}

// DocumentError describes why a document could not be read or converted.
// Path is an RFC 6901 JSON pointer to the offending value.
type DocumentError struct {
	Kind    string
	Path    string
	Message string
}

func (e *DocumentError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("gemara document: %s: %s", e.Kind, e.Message)
	}
	return fmt.Sprintf("gemara document: %s at %s: %s", e.Kind, e.Path, e.Message)
}

func documentError(kind, path, message string) *DocumentError {
	return &DocumentError{Kind: kind, Path: path, Message: message}
}

// Raw returns a copy of the exact bytes supplied to ReadDocument.
func (d *Document) Raw() []byte {
	return bytes.Clone(d.raw)
}

// Metadata returns a copy of the document's artifact metadata.
func (d *Document) Metadata() DocumentMetadata {
	return d.metadata
}

// Node returns a deep copy of the parsed YAML document node. Alias links point
// into the returned copy, never into the Document, and cyclic aliases are
// copied without recursion.
func (d *Document) Node() *yaml.Node {
	return cloneDocumentNode(d.node)
}

func cloneDocumentNode(node *yaml.Node) *yaml.Node {
	return cloneYAMLNode(node, make(map[*yaml.Node]*yaml.Node))
}

func cloneYAMLNode(node *yaml.Node, visited map[*yaml.Node]*yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if copy, ok := visited[node]; ok {
		return copy
	}
	copy := &yaml.Node{
		Kind:        node.Kind,
		Style:       node.Style,
		Tag:         node.Tag,
		Value:       node.Value,
		Anchor:      node.Anchor,
		Alias:       nil,
		Content:     nil,
		HeadComment: node.HeadComment,
		LineComment: node.LineComment,
		FootComment: node.FootComment,
		Line:        node.Line,
		Column:      node.Column,
	}
	visited[node] = copy
	if len(node.Content) > 0 {
		copy.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			copy.Content[i] = cloneYAMLNode(child, visited)
		}
	}
	copy.Alias = cloneYAMLNode(node.Alias, visited)
	return copy
}

func parsePointerToken(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}
