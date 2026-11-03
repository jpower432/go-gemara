// SPDX-License-Identifier: Apache-2.0

package gemara

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// EncodeArtifactJSON writes a registered generated artifact model as JSON that
// conforms to the schema version linked into this SDK. The artifact's own
// metadata.version is preserved; metadata.gemara-version is replaced with the
// linked schema version. The input is never mutated.
//
// A *Document is accepted only when it projects without unsupported content.
// Typed values cannot reveal fields that an earlier decode discarded, and a
// successful encode does not by itself claim full schema validation.
func EncodeArtifactJSON(value any) ([]byte, error) {
	return encodeArtifact(value)
}

// EncodeArtifactYAML writes a registered generated artifact model as YAML that
// conforms to the schema version linked into this SDK. It has the same input,
// version-stamping, and refusal behavior as EncodeArtifactJSON.
func EncodeArtifactYAML(value any) ([]byte, error) {
	encoded, err := encodeArtifact(value)
	if err != nil {
		return nil, err
	}
	var document any
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		return nil, fmt.Errorf("encode artifact yaml: %w", err)
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode artifact yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("encode artifact yaml: %w", err)
	}
	return out.Bytes(), nil
}

func encodeArtifact(value any) ([]byte, error) {
	if document, ok := value.(*Document); ok {
		if document == nil {
			return nil, fmt.Errorf("encode artifact: nil document")
		}
		projection, err := document.Project()
		if err != nil {
			return nil, err
		}
		if len(projection.Unsupported) > 0 {
			return nil, newCompatibilityError(document.metadata, projection.Unsupported)
		}
		value = projection.Artifact
	}
	if value == nil {
		return nil, fmt.Errorf("encode artifact: nil input")
	}

	modelType := reflect.TypeOf(value)
	for modelType.Kind() == reflect.Pointer {
		if reflect.ValueOf(value).IsNil() {
			return nil, fmt.Errorf("encode artifact: nil %s", modelType.Elem().Name())
		}
		modelType = modelType.Elem()
	}
	modelName, ok := artifactNameForType(modelType)
	if !ok {
		return nil, fmt.Errorf("encode artifact: unsupported type %T", value)
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode artifact %s: %w", modelName, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("encode artifact %s: %w", modelName, err)
	}
	metadata, ok := object["metadata"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("encode artifact %s: metadata is required", modelName)
	}
	declaredType, ok := metadata["type"].(string)
	if !ok || declaredType != modelName {
		return nil, fmt.Errorf("encode artifact: metadata.type %v does not match %s", metadata["type"], modelName)
	}
	metadata["gemara-version"] = strings.TrimPrefix(SchemaVersion, "v")

	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("encode artifact %s: %w", modelName, err)
	}

	document, err := ReadDocument(encoded)
	if err != nil {
		return nil, fmt.Errorf("encode artifact %s: %w", modelName, err)
	}
	projection, err := document.Project()
	if err != nil {
		return nil, err
	}
	if len(projection.Unsupported) > 0 {
		return nil, newCompatibilityError(document.metadata, projection.Unsupported)
	}
	return encoded, nil
}

func artifactNameForType(modelType reflect.Type) (string, bool) {
	for name, candidate := range documentModelTypes {
		if candidate == modelType {
			return name, true
		}
	}
	return "", false
}
