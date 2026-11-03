// SPDX-License-Identifier: Apache-2.0

package gemara

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/gemaraproj/go-gemara/internal/codec"
	"gopkg.in/yaml.v3"
)

// CompatibilityIssue describes one part of a document that this version of the
// schema cannot interpret. Path is an RFC 6901 JSON Pointer into the source
// document. Kind is one of unknown_field, unknown_enum, unsupported_type, or
// unsupported_value.
type CompatibilityIssue struct {
	Kind         string `json:"kind"`
	Path         string `json:"path"`
	InputVersion string `json:"input_version"`
	SDKVersion   string `json:"sdk_version"`
}

// CompatibilityError reports semantic incompatibilities that prevent a usable
// typed projection. Reading the original Document still succeeds.
type CompatibilityError struct {
	Issues []CompatibilityIssue `json:"issues"`
}

func (e *CompatibilityError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "compatibility: no issues"
	}
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		parts = append(parts, fmt.Sprintf("%s at %s", issue.Kind, issue.Path))
	}
	return fmt.Sprintf("compatibility: %d issue(s): %s", len(e.Issues), strings.Join(parts, "; "))
}

// Projection is a typed view of a Document. Unsupported holds unknown fields
// that were omitted from the projection; they are not silently supported.
type Projection struct {
	Artifact    any                  `json:"artifact"`
	Unsupported []CompatibilityIssue `json:"unsupported,omitempty"`
	jsonValue   any
}

// Project decodes the document into the typed model this version of the
// schema understands. Unknown fields are reported and omitted. An unknown
// enum, artifact type, or value shape prevents projection entirely so callers
// cannot act on partially interpreted semantics.
func (d *Document) Project() (*Projection, error) {
	if d == nil || d.node == nil || d.node.Kind != yaml.DocumentNode || len(d.node.Content) != 1 {
		return nil, documentError("unsupported_value", "", "document has no single root value")
	}
	modelType, ok := documentModelTypes[d.metadata.Type]
	if !ok {
		return nil, newCompatibilityError(d.metadata, []CompatibilityIssue{{
			Kind: "unsupported_type",
			Path: "/metadata/type",
		}})
	}
	walker := &projectionWalker{document: d}
	value := walker.walk(d.node.Content[0], modelType, "", make(map[*yaml.Node]bool))
	sortCompatibilityIssues(walker.issues)
	for _, issue := range walker.issues {
		if issue.Kind != "unknown_field" {
			return nil, newCompatibilityError(d.metadata, walker.issues)
		}
	}
	artifact, err := decodeProjectedArtifact(d.raw, modelType)
	if err != nil {
		return nil, fmt.Errorf("project %s: %w", d.metadata.Type, err)
	}
	unsupported := make([]CompatibilityIssue, 0, len(walker.issues))
	unsupported = append(unsupported, walker.issues...)
	return &Projection{Artifact: artifact, Unsupported: unsupported, jsonValue: value}, nil
}

func newCompatibilityError(metadata DocumentMetadata, issues []CompatibilityIssue) *CompatibilityError {
	filled := make([]CompatibilityIssue, len(issues))
	for i, issue := range issues {
		if issue.InputVersion == "" {
			issue.InputVersion = metadata.GemaraVersion
		}
		if issue.SDKVersion == "" {
			issue.SDKVersion = SchemaVersion
		}
		filled[i] = issue
	}
	sortCompatibilityIssues(filled)
	return &CompatibilityError{Issues: filled}
}

func sortCompatibilityIssues(issues []CompatibilityIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Kind != issues[j].Kind {
			return issues[i].Kind < issues[j].Kind
		}
		if issues[i].InputVersion != issues[j].InputVersion {
			return issues[i].InputVersion < issues[j].InputVersion
		}
		return issues[i].SDKVersion < issues[j].SDKVersion
	})
}

func decodeProjectedArtifact(raw []byte, modelType reflect.Type) (any, error) {
	target := reflect.New(modelType)
	if json.Valid(raw) {
		if err := codec.DecodeJSON(bytes.NewReader(raw), target.Interface()); err != nil {
			return nil, err
		}
		return target.Interface(), nil
	}
	if err := codec.UnmarshalYAML(raw, target.Interface()); err != nil {
		return nil, err
	}
	return target.Interface(), nil
}

type projectionWalker struct {
	document *Document
	issues   []CompatibilityIssue
}

func (w *projectionWalker) addIssue(kind, path string) {
	w.issues = append(w.issues, CompatibilityIssue{
		Kind:         kind,
		Path:         path,
		InputVersion: w.document.metadata.GemaraVersion,
		SDKVersion:   SchemaVersion,
	})
}

func (w *projectionWalker) walk(node *yaml.Node, modelType reflect.Type, path string, stack map[*yaml.Node]bool) any {
	node = resolveAlias(node)
	if node == nil {
		w.addIssue("unsupported_value", path)
		return nil
	}
	if stack[node] {
		w.addIssue("unsupported_value", path)
		return nil
	}

	for modelType.Kind() == reflect.Pointer {
		if isNullNode(node) {
			return nil
		}
		modelType = modelType.Elem()
	}
	if modelType.Kind() == reflect.Interface {
		return w.walkAny(node, path, stack)
	}
	stack[node] = true
	defer delete(stack, node)
	if textType := reflect.PointerTo(modelType); textType.Implements(reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()) {
		return w.walkText(node, modelType, path)
	}

	switch modelType.Kind() {
	case reflect.Struct:
		return w.walkStruct(node, modelType, path, stack)
	case reflect.Slice:
		return w.walkSlice(node, modelType, path, stack)
	case reflect.Map:
		return w.walkMap(node, modelType, path, stack)
	case reflect.String:
		if node.Kind != yaml.ScalarNode || node.Tag == "!!null" {
			w.addIssue("unsupported_value", path)
			return nil
		}
		return node.Value
	case reflect.Bool:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
			w.addIssue("unsupported_value", path)
			return nil
		}
		return strings.EqualFold(node.Value, "true")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
			w.addIssue("unsupported_value", path)
			return nil
		}
		text, err := canonicalInt(node.Value)
		if err != nil {
			w.addIssue("unsupported_value", path)
			return nil
		}
		if _, parseErr := strconv.ParseInt(text, 10, modelType.Bits()); parseErr != nil {
			w.addIssue("unsupported_value", path)
			return nil
		}
		return json.Number(text)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
			w.addIssue("unsupported_value", path)
			return nil
		}
		text, err := canonicalInt(node.Value)
		if err != nil {
			w.addIssue("unsupported_value", path)
			return nil
		}
		if _, parseErr := strconv.ParseUint(strings.TrimPrefix(text, "+"), 10, modelType.Bits()); parseErr != nil {
			w.addIssue("unsupported_value", path)
			return nil
		}
		return json.Number(text)
	case reflect.Float32, reflect.Float64:
		if node.Kind != yaml.ScalarNode || (node.Tag != "!!float" && node.Tag != "!!int") {
			w.addIssue("unsupported_value", path)
			return nil
		}
		text, err := canonicalFloat(node.Value)
		if node.Tag == "!!int" {
			text, err = canonicalInt(node.Value)
		}
		if err != nil {
			w.addIssue("unsupported_value", path)
			return nil
		}
		if _, parseErr := strconv.ParseFloat(text, modelType.Bits()); parseErr != nil {
			w.addIssue("unsupported_value", path)
			return nil
		}
		return json.Number(text)
	default:
		w.addIssue("unsupported_value", path)
		return nil
	}
}

func (w *projectionWalker) walkText(node *yaml.Node, modelType reflect.Type, path string) any {
	if node.Kind != yaml.ScalarNode || (node.Tag != "" && node.Tag != "!!str") {
		w.addIssue("unsupported_value", path)
		return nil
	}
	target := reflect.New(modelType)
	unmarshaler, ok := target.Interface().(encoding.TextUnmarshaler)
	if !ok {
		w.addIssue("unsupported_value", path)
		return nil
	}
	if err := unmarshaler.UnmarshalText([]byte(node.Value)); err != nil {
		w.addIssue("unknown_enum", path)
		return nil
	}
	return node.Value
}

func (w *projectionWalker) walkStruct(node *yaml.Node, modelType reflect.Type, path string, stack map[*yaml.Node]bool) any {
	if isNullNode(node) {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		w.addIssue("unsupported_value", path)
		return nil
	}
	fields := structFields(modelType)
	result := make(map[string]any, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := resolveAlias(node.Content[i])
		if key == nil || key.Kind != yaml.ScalarNode {
			w.addIssue("unsupported_value", path)
			continue
		}
		keyPath := path + "/" + parsePointerToken(key.Value)
		field, known := fields[key.Value]
		if !known {
			w.addIssue("unknown_field", keyPath)
			continue
		}
		result[key.Value] = w.walk(node.Content[i+1], field.Type, keyPath, stack)
	}
	return result
}

func (w *projectionWalker) walkSlice(node *yaml.Node, modelType reflect.Type, path string, stack map[*yaml.Node]bool) any {
	if isNullNode(node) {
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		w.addIssue("unsupported_value", path)
		return nil
	}
	result := make([]any, 0, len(node.Content))
	for i, child := range node.Content {
		result = append(result, w.walk(child, modelType.Elem(), path+"/"+strconv.Itoa(i), stack))
	}
	return result
}

func (w *projectionWalker) walkMap(node *yaml.Node, modelType reflect.Type, path string, stack map[*yaml.Node]bool) any {
	if isNullNode(node) {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		w.addIssue("unsupported_value", path)
		return nil
	}
	if modelType.Key().Kind() != reflect.String {
		w.addIssue("unsupported_value", path)
		return nil
	}
	result := make(map[string]any, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := resolveAlias(node.Content[i])
		if key == nil || key.Kind != yaml.ScalarNode {
			w.addIssue("unsupported_value", path)
			continue
		}
		keyPath := path + "/" + parsePointerToken(key.Value)
		result[key.Value] = w.walk(node.Content[i+1], modelType.Elem(), keyPath, stack)
	}
	return result
}

func (w *projectionWalker) walkAny(node *yaml.Node, path string, stack map[*yaml.Node]bool) any {
	node = resolveAlias(node)
	if node == nil {
		w.addIssue("unsupported_value", path)
		return nil
	}
	if stack[node] {
		w.addIssue("unsupported_value", path)
		return nil
	}
	stack[node] = true
	defer delete(stack, node)
	switch node.Kind {
	case yaml.MappingNode:
		result := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := resolveAlias(node.Content[i])
			if key == nil || key.Kind != yaml.ScalarNode || (key.Tag != "" && key.Tag != "!!str") {
				w.addIssue("unsupported_value", path)
				continue
			}
			keyPath := path + "/" + parsePointerToken(key.Value)
			result[key.Value] = w.walkAny(resolveAlias(node.Content[i+1]), keyPath, stack)
		}
		return result
	case yaml.SequenceNode:
		result := make([]any, 0, len(node.Content))
		for i, child := range node.Content {
			result = append(result, w.walkAny(resolveAlias(child), path+"/"+strconv.Itoa(i), stack))
		}
		return result
	case yaml.ScalarNode:
		switch node.Tag {
		case "", "!!str", "!!timestamp":
			return node.Value
		case "!!int":
			text, err := canonicalInt(node.Value)
			if err != nil {
				w.addIssue("unsupported_value", path)
				return nil
			}
			return json.Number(text)
		case "!!float":
			text, err := canonicalFloat(node.Value)
			if err != nil {
				w.addIssue("unsupported_value", path)
				return nil
			}
			return json.Number(text)
		case "!!bool":
			return strings.EqualFold(node.Value, "true")
		case "!!null":
			return nil
		default:
			w.addIssue("unsupported_value", path)
			return nil
		}
	default:
		w.addIssue("unsupported_value", path)
		return nil
	}
}

func structFields(modelType reflect.Type) map[string]reflect.StructField {
	fields := make(map[string]reflect.StructField, modelType.NumField())
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			name = strings.Split(field.Tag.Get("yaml"), ",")[0]
		}
		if name == "" || name == "-" {
			continue
		}
		fields[name] = field
	}
	return fields
}

func isNullNode(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!null"
}
