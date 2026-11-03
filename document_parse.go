// SPDX-License-Identifier: Apache-2.0

package gemara

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReadDocument parses exactly one YAML or JSON document and retains its exact
// bytes and syntax tree. Unknown fields, tags, aliases, enum spellings, and
// numeric values are preserved; only artifact metadata is interpreted.
func ReadDocument(raw []byte) (*Document, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, documentError("syntax", "", "empty document")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var node yaml.Node
	if err := decoder.Decode(&node); err != nil {
		return nil, documentError("syntax", "", err.Error())
	}
	var extra yaml.Node
	switch err := decoder.Decode(&extra); {
	case err == nil:
		return nil, documentError("syntax", "", "single artifact document required")
	case !errors.Is(err, io.EOF):
		return nil, documentError("syntax", "", err.Error())
	}
	if node.Kind != yaml.DocumentNode || len(node.Content) != 1 {
		return nil, documentError("invalid_metadata", "", "artifact document must have one mapping root")
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, documentError("invalid_metadata", "", "artifact root must be a mapping")
	}
	if err := rejectDuplicateKeys(root, "", make(map[*yaml.Node]bool)); err != nil {
		return nil, err
	}
	metadata, err := readMetadata(root)
	if err != nil {
		return nil, err
	}
	return &Document{raw: bytes.Clone(raw), node: &node, metadata: metadata}, nil
}

func rejectDuplicateKeys(node *yaml.Node, path string, visited map[*yaml.Node]bool) *DocumentError {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		if node.Alias == nil || visited[node.Alias] {
			return nil
		}
		visited[node.Alias] = true
		return rejectDuplicateKeys(node.Alias, path, visited)
	}
	switch node.Kind {
	case yaml.MappingNode:
		seen := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := resolveAlias(node.Content[i])
			if key == nil {
				return documentError("syntax", path, "mapping key alias has no target")
			}
			childPath := path
			if key.Kind == yaml.ScalarNode {
				if _, ok := seen[key.Value]; ok {
					return documentError("syntax", path+"/"+parsePointerToken(key.Value),
						fmt.Sprintf("duplicate mapping key %q", key.Value))
				}
				seen[key.Value] = struct{}{}
				childPath = path + "/" + parsePointerToken(key.Value)
			}
			if err := rejectDuplicateKeys(node.Content[i+1], childPath, visited); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			if err := rejectDuplicateKeys(child, path+"/"+strconv.Itoa(i), visited); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveAlias(node *yaml.Node) *yaml.Node {
	seen := make(map[*yaml.Node]bool)
	for node != nil && node.Kind == yaml.AliasNode {
		if node.Alias == nil || seen[node.Alias] {
			return nil
		}
		seen[node.Alias] = true
		node = node.Alias
	}
	return node
}

func readMetadata(root *yaml.Node) (DocumentMetadata, *DocumentError) {
	var metadataNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := resolveAlias(root.Content[i])
		if key != nil && key.Kind == yaml.ScalarNode && key.Value == "metadata" {
			metadataNode = resolveAlias(root.Content[i+1])
			break
		}
	}
	if metadataNode == nil || metadataNode.Kind != yaml.MappingNode {
		return DocumentMetadata{}, documentError("invalid_metadata", "/metadata",
			"metadata must be a mapping")
	}
	fields := make(map[string]*yaml.Node, len(metadataNode.Content)/2)
	for i := 0; i+1 < len(metadataNode.Content); i += 2 {
		key := resolveAlias(metadataNode.Content[i])
		if key == nil || key.Kind != yaml.ScalarNode {
			continue
		}
		fields[key.Value] = resolveAlias(metadataNode.Content[i+1])
	}
	id, ok := fields["id"]
	if !ok {
		return DocumentMetadata{}, documentError("invalid_metadata", "/metadata",
			"metadata.id is required")
	}
	if !isStringScalar(id) || strings.TrimSpace(id.Value) == "" {
		return DocumentMetadata{}, documentError("invalid_metadata", "/metadata/id",
			"metadata.id must be a non-blank string")
	}
	artifactType, ok := fields["type"]
	if !ok {
		return DocumentMetadata{}, documentError("invalid_metadata", "/metadata/type",
			"metadata.type is required")
	}
	if !isStringScalar(artifactType) || strings.TrimSpace(artifactType.Value) == "" {
		return DocumentMetadata{}, documentError("invalid_metadata", "/metadata/type",
			"metadata.type must be a non-blank string")
	}
	gemaraVersion, ok := fields["gemara-version"]
	if !ok {
		return DocumentMetadata{}, documentError("missing_version", gemaraVersionPath,
			"metadata.gemara-version is required")
	}
	if !isStringScalar(gemaraVersion) {
		return DocumentMetadata{}, documentError("invalid_metadata", gemaraVersionPath,
			"metadata.gemara-version must be a string")
	}
	if strings.TrimSpace(gemaraVersion.Value) == "" {
		return DocumentMetadata{}, documentError("missing_version", gemaraVersionPath,
			"metadata.gemara-version is required")
	}
	if err := validateGemaraVersion(gemaraVersion.Value); err != nil {
		return DocumentMetadata{}, err
	}
	version := ""
	if optional, ok := fields["version"]; ok {
		if !isStringScalar(optional) {
			return DocumentMetadata{}, documentError("invalid_metadata", "/metadata/version",
				"metadata.version must be a string when supplied")
		}
		version = optional.Value
	}
	return DocumentMetadata{
		ID:            id.Value,
		Type:          artifactType.Value,
		GemaraVersion: gemaraVersion.Value,
		Version:       version,
	}, nil
}

func isStringScalar(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && (node.Tag == "" || node.Tag == "!!str")
}

// JSON converts the retained syntax tree to JSON-compatible bytes. Values are
// converted without passing numbers through float64. YAML tags with no JSON
// representation produce a path-qualified unsupported_value error rather than
// being stringified.
func (d *Document) JSON() ([]byte, error) {
	if d == nil || d.node == nil || d.node.Kind != yaml.DocumentNode || len(d.node.Content) != 1 {
		return nil, documentError("unsupported_value", "", "document has no single root value")
	}
	var out bytes.Buffer
	if err := writeJSONValue(&out, d.node.Content[0], "", make(map[*yaml.Node]bool)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeJSONValue(out *bytes.Buffer, node *yaml.Node, path string, stack map[*yaml.Node]bool) *DocumentError {
	if node == nil {
		return documentError("unsupported_value", path, "missing value")
	}
	if node.Kind == yaml.AliasNode {
		target := resolveAlias(node)
		if target == nil {
			return documentError("unsupported_value", path, "alias has no target")
		}
		if stack[target] {
			return documentError("unsupported_value", path, "cyclic alias has no JSON representation")
		}
		return writeJSONValue(out, target, path, stack)
	}
	if stack[node] {
		return documentError("unsupported_value", path, "cyclic alias has no JSON representation")
	}
	stack[node] = true
	defer delete(stack, node)

	switch node.Kind {
	case yaml.MappingNode:
		out.WriteByte('{')
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := resolveAlias(node.Content[i])
			if key == nil || key.Kind != yaml.ScalarNode || !isStringScalar(key) {
				return documentError("unsupported_value", path+"/"+parsePointerToken(scalarText(key)),
					"JSON object keys must be strings")
			}
			if i > 0 {
				out.WriteByte(',')
			}
			encoded, err := json.Marshal(key.Value)
			if err != nil {
				return documentError("unsupported_value", path+"/"+parsePointerToken(key.Value), err.Error())
			}
			out.Write(encoded)
			out.WriteByte(':')
			if err := writeJSONValue(out, node.Content[i+1], path+"/"+parsePointerToken(key.Value), stack); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case yaml.SequenceNode:
		out.WriteByte('[')
		for i, child := range node.Content {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeJSONValue(out, child, path+"/"+strconv.Itoa(i), stack); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case yaml.ScalarNode:
		value, err := scalarJSON(node)
		if err != nil {
			err.Path = path
			return err
		}
		out.WriteString(value)
	default:
		return documentError("unsupported_value", path,
			fmt.Sprintf("YAML node kind %d has no JSON representation", node.Kind))
	}
	return nil
}

func scalarText(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	return node.Value
}

func scalarJSON(node *yaml.Node) (string, *DocumentError) {
	switch node.Tag {
	case "", "!!str":
		encoded, err := json.Marshal(node.Value)
		if err != nil {
			return "", documentError("unsupported_value", "", err.Error())
		}
		return string(encoded), nil
	case "!!int":
		return canonicalInt(node.Value)
	case "!!float":
		return canonicalFloat(node.Value)
	case "!!bool":
		switch strings.ToLower(node.Value) {
		case "true":
			return "true", nil
		case "false":
			return "false", nil
		default:
			return "", documentError("unsupported_value", "",
				fmt.Sprintf("boolean value %q has no JSON representation", node.Value))
		}
	case "!!null":
		return "null", nil
	default:
		return "", documentError("unsupported_value", "",
			fmt.Sprintf("YAML tag %q has no JSON representation", node.Tag))
	}
}

func canonicalInt(raw string) (string, *DocumentError) {
	text := strings.ReplaceAll(strings.TrimSpace(raw), "_", "")
	if text == "" {
		return "", documentError("unsupported_value", "", "empty integer")
	}
	negative := false
	switch text[0] {
	case '+':
		text = text[1:]
	case '-':
		negative = true
		text = text[1:]
	}
	if text == "" {
		return "", documentError("unsupported_value", "", fmt.Sprintf("invalid integer %q", raw))
	}
	value, ok := new(big.Int).SetString(text, 0)
	if !ok {
		return "", documentError("unsupported_value", "", fmt.Sprintf("invalid integer %q", raw))
	}
	if negative {
		value.Neg(value)
	}
	return value.String(), nil
}

func canonicalFloat(raw string) (string, *DocumentError) {
	text := strings.ReplaceAll(strings.TrimSpace(raw), "_", "")
	lower := strings.ToLower(text)
	if strings.Contains(lower, ".inf") || strings.Contains(lower, ".nan") {
		return "", documentError("unsupported_value", "",
			fmt.Sprintf("float value %q has no JSON representation", raw))
	}
	if text == "" {
		return "", documentError("unsupported_value", "", "empty float")
	}
	sign := ""
	switch text[0] {
	case '+':
		text = text[1:]
	case '-':
		sign = "-"
		text = text[1:]
	}
	exponent := ""
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponent = text[index+1:]
		text = text[:index]
	}
	integer, fraction, hasDot := strings.Cut(text, ".")
	if !allDigits(integer) || !allDigits(fraction) || (integer == "" && fraction == "") {
		return "", documentError("unsupported_value", "", fmt.Sprintf("invalid float %q", raw))
	}
	if integer == "" {
		integer = "0"
	} else {
		integer = strings.TrimLeft(integer, "0")
		if integer == "" {
			integer = "0"
		}
	}
	if hasDot {
		if fraction == "" {
			fraction = "0"
		}
		integer += "." + fraction
	}
	if exponent != "" {
		exponentSign := ""
		switch exponent[0] {
		case '+':
			exponent = exponent[1:]
		case '-':
			exponentSign = "-"
			exponent = exponent[1:]
		}
		if !allDigits(exponent) || exponent == "" {
			return "", documentError("unsupported_value", "", fmt.Sprintf("invalid float %q", raw))
		}
		exponent = strings.TrimLeft(exponent, "0")
		if exponent == "" {
			exponent = "0"
		}
		integer += "e" + exponentSign + exponent
	}
	return sign + integer, nil
}

func allDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
