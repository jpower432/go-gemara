// SPDX-License-Identifier: Apache-2.0

package gemara

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func encoderMetadata(artifactType ArtifactType) Metadata {
	return Metadata{
		Id:            "synthetic-" + strings.ToLower(artifactType.String()),
		Type:          artifactType,
		GemaraVersion: "1.99.0",
		Version:       "release-A",
		Description:   "Synthetic artifact for encoder tests.",
		Author: Actor{
			Id:   "synthetic-author",
			Name: "Synthetic Author",
			Type: Human,
		},
	}
}

func encoderPolicy() Policy {
	return Policy{
		Title:    "Synthetic Policy",
		Metadata: encoderMetadata(PolicyArtifact),
		Contacts: RACI{
			Responsible: []Contact{{Name: "Synthetic Team"}},
			Accountable: []Contact{{Name: "Synthetic Owner"}},
		},
		Scope:   Scope{In: Dimensions{}},
		Imports: Imports{},
		ImplementationPlan: ImplementationPlan{
			EvaluationTimeline:  ImplementationDetails{Start: "2030-01-01T00:00:00Z", Notes: "Evaluate annually."},
			EnforcementTimeline: ImplementationDetails{Start: "2030-01-01T00:00:00Z", Notes: "Enforce continuously."},
		},
		Adherence: Adherence{},
	}
}

func encoderControlCatalog() ControlCatalog {
	return ControlCatalog{
		Title:    "Synthetic Control Catalog",
		Metadata: encoderMetadata(ControlCatalogArtifact),
	}
}

func encoderGuidanceCatalog() GuidanceCatalog {
	return GuidanceCatalog{
		Title:        "Synthetic Guidance Catalog",
		Metadata:     encoderMetadata(GuidanceCatalogArtifact),
		GuidanceType: GuidanceFramework,
	}
}

func encoderMappingDocument() MappingDocument {
	metadata := encoderMetadata(MappingDocumentArtifact)
	metadata.MappingReferences = []MappingReference{
		{Id: "source-catalog", Title: "Synthetic Source Catalog", Version: "1.0.0"},
		{Id: "target-catalog", Title: "Synthetic Target Catalog", Version: "2.0.0"},
	}
	return MappingDocument{
		Title:    "Synthetic Mapping Document",
		Metadata: metadata,
		SourceReference: TypedMapping{
			EntryType:   EntryTypeControl,
			ReferenceId: "source-catalog",
		},
		TargetReference: TypedMapping{
			EntryType:   EntryTypeControl,
			ReferenceId: "target-catalog",
		},
		Mappings: []Mapping{{
			Id:           "mapping-1",
			Source:       "SRC-1",
			Relationship: RelImplements,
			Targets:      []MappingTarget{{EntryId: "TGT-1", Strength: 5}},
		}},
	}
}

func TestEncodeArtifactJSONStampsLinkedSchemaVersion(t *testing.T) {
	policy := encoderPolicy()
	original := encoderPolicy()

	encoded, err := EncodeArtifactJSON(&policy)
	if err != nil {
		t.Fatalf("EncodeArtifactJSON: %v", err)
	}
	doc, err := ReadDocument(encoded)
	if err != nil {
		t.Fatalf("ReadDocument: %v", err)
	}
	metadata := doc.Metadata()
	if metadata.GemaraVersion != strings.TrimPrefix(SchemaVersion, "v") {
		t.Errorf("gemara-version = %q, want %q", metadata.GemaraVersion, strings.TrimPrefix(SchemaVersion, "v"))
	}
	if metadata.Version != "release-A" {
		t.Errorf("artifact version = %q, want release-A", metadata.Version)
	}
	if metadata.Type != PolicyArtifact.String() {
		t.Errorf("artifact type = %q, want %q", metadata.Type, PolicyArtifact.String())
	}
	if !reflect.DeepEqual(policy, original) {
		t.Errorf("EncodeArtifactJSON mutated its input")
	}

	again, err := EncodeArtifactJSON(&policy)
	if err != nil {
		t.Fatalf("second EncodeArtifactJSON: %v", err)
	}
	if string(again) != string(encoded) {
		t.Errorf("EncodeArtifactJSON is not deterministic")
	}
}

func TestEncodeArtifactYAMLStampsLinkedSchemaVersion(t *testing.T) {
	catalog := encoderControlCatalog()
	original := encoderControlCatalog()

	encoded, err := EncodeArtifactYAML(catalog)
	if err != nil {
		t.Fatalf("EncodeArtifactYAML: %v", err)
	}
	doc, err := ReadDocument(encoded)
	if err != nil {
		t.Fatalf("ReadDocument: %v", err)
	}
	metadata := doc.Metadata()
	if metadata.GemaraVersion != strings.TrimPrefix(SchemaVersion, "v") {
		t.Errorf("gemara-version = %q, want %q", metadata.GemaraVersion, strings.TrimPrefix(SchemaVersion, "v"))
	}
	if metadata.Version != "release-A" {
		t.Errorf("artifact version = %q, want release-A", metadata.Version)
	}
	if !reflect.DeepEqual(catalog, original) {
		t.Errorf("EncodeArtifactYAML mutated its input")
	}
	if !strings.Contains(string(encoded), "metadata:\n") || strings.HasPrefix(string(encoded), "{") {
		t.Errorf("EncodeArtifactYAML did not produce block-style YAML:\n%s", encoded)
	}
}

func TestEncodeArtifactAcceptsRegisteredValueTypes(t *testing.T) {
	mapping := encoderMappingDocument()
	inputs := []any{
		encoderPolicy(),
		encoderControlCatalog(),
		encoderGuidanceCatalog(),
		encoderMappingDocument(),
		&mapping,
	}
	for _, input := range inputs {
		if _, err := EncodeArtifactJSON(input); err != nil {
			t.Errorf("EncodeArtifactJSON(%T): %v", input, err)
		}
		if _, err := EncodeArtifactYAML(input); err != nil {
			t.Errorf("EncodeArtifactYAML(%T): %v", input, err)
		}
	}
}

func TestEncodeArtifactRejectsRawDocumentWithUnknownContent(t *testing.T) {
	raw := []byte(`{
		"metadata": {"id": "c", "type": "ControlCatalog", "gemara-version": "1.99.0"},
		"controls": [{"id": "C1", "state": "Active", "remarks": "not yet supported"}]
	}`)
	doc, err := ReadDocument(raw)
	if err != nil {
		t.Fatalf("ReadDocument: %v", err)
	}
	for _, encode := range []func(any) ([]byte, error){EncodeArtifactJSON, EncodeArtifactYAML} {
		encoded, err := encode(doc)
		if err == nil {
			t.Errorf("encode accepted a document with unsupported content: %s", encoded)
		}
		if len(encoded) != 0 {
			t.Errorf("failed encode returned output: %q", encoded)
		}
		var compatibility *CompatibilityError
		if !errors.As(err, &compatibility) {
			t.Errorf("error = %T, want CompatibilityError", err)
		}
	}
}

func TestEncodeArtifactRejectsUnknownEnum(t *testing.T) {
	catalog := encoderControlCatalog()
	catalog.Controls = []Control{{
		Id:                     "C1",
		Title:                  "Control",
		Objective:              "Objective",
		Group:                  "group-1",
		AssessmentRequirements: []AssessmentRequirement{{Id: "R1", Text: "Text", Applicability: []string{"all"}}},
		State:                  Lifecycle(99),
	}}
	for _, encode := range []func(any) ([]byte, error){EncodeArtifactJSON, EncodeArtifactYAML} {
		encoded, err := encode(catalog)
		if err == nil {
			t.Fatalf("encode accepted an unknown enum: %s", encoded)
		}
		if len(encoded) != 0 {
			t.Errorf("failed encode returned output: %q", encoded)
		}
	}
}

func TestEncodeArtifactRejectsTypeMismatch(t *testing.T) {
	policy := encoderPolicy()
	policy.Metadata.Type = ControlCatalogArtifact
	encoded, err := EncodeArtifactJSON(policy)
	if err == nil {
		t.Fatalf("encode accepted mismatched metadata.type: %s", encoded)
	}
	if len(encoded) != 0 {
		t.Errorf("failed encode returned output: %q", encoded)
	}
}

func TestEncodeArtifactRejectsNilAndUnregisteredInputs(t *testing.T) {
	var typedNil *Policy
	var nilDocument *Document
	inputs := []any{nil, typedNil, nilDocument, map[string]string{"metadata": "value"}}
	for _, input := range inputs {
		if encoded, err := EncodeArtifactJSON(input); err == nil {
			t.Errorf("EncodeArtifactJSON(%T) accepted input: %s", input, encoded)
		}
		if encoded, err := EncodeArtifactYAML(input); err == nil {
			t.Errorf("EncodeArtifactYAML(%T) accepted input: %s", input, encoded)
		}
	}
}

func TestEncodeArtifactDocumentRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("test-data/compatibility/mapping.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doc, err := ReadDocument(raw)
	if err != nil {
		t.Fatalf("ReadDocument: %v", err)
	}
	encoded, err := EncodeArtifactYAML(doc)
	if err != nil {
		t.Fatalf("EncodeArtifactYAML: %v", err)
	}
	roundTripped, err := ReadDocument(encoded)
	if err != nil {
		t.Fatalf("ReadDocument(encoded): %v", err)
	}
	if roundTripped.Metadata().GemaraVersion != strings.TrimPrefix(SchemaVersion, "v") {
		t.Errorf("gemara-version = %q, want linked schema version", roundTripped.Metadata().GemaraVersion)
	}
	if roundTripped.Metadata().Version != "release-A" {
		t.Errorf("artifact version = %q, want release-A", roundTripped.Metadata().Version)
	}
	projection, err := roundTripped.Project()
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	artifact, ok := projection.Artifact.(*MappingDocument)
	if !ok {
		t.Fatalf("projected artifact = %T, want *MappingDocument", projection.Artifact)
	}
	if len(artifact.Mappings) != 1 || artifact.Mappings[0].Id != "mapping-1" {
		t.Errorf("mapping content was not retained: %+v", artifact.Mappings)
	}
}

func TestEncoderFixturesMatchAcrossFormats(t *testing.T) {
	for _, name := range []string{"policy", "control-catalog", "guidance-catalog", "mapping"} {
		yamlRaw, err := os.ReadFile("test-data/compatibility/" + name + ".yaml")
		if err != nil {
			t.Fatalf("%s yaml fixture: %v", name, err)
		}
		jsonRaw, err := os.ReadFile("test-data/compatibility/" + name + ".json")
		if err != nil {
			t.Fatalf("%s json fixture: %v", name, err)
		}
		yamlDocument, err := ReadDocument(yamlRaw)
		if err != nil {
			t.Fatalf("%s yaml ReadDocument: %v", name, err)
		}
		jsonDocument, err := ReadDocument(jsonRaw)
		if err != nil {
			t.Fatalf("%s json ReadDocument: %v", name, err)
		}
		yamlProjection, err := yamlDocument.Project()
		if err != nil {
			t.Fatalf("%s yaml Project: %v", name, err)
		}
		jsonProjection, err := jsonDocument.Project()
		if err != nil {
			t.Fatalf("%s json Project: %v", name, err)
		}
		if len(yamlProjection.Unsupported) != 0 || len(jsonProjection.Unsupported) != 0 {
			t.Errorf("%s fixtures contain unsupported content", name)
		}
		if !reflect.DeepEqual(yamlProjection.Artifact, jsonProjection.Artifact) {
			t.Errorf("%s YAML and JSON fixtures project to different values", name)
		}
		if yamlDocument.Metadata().GemaraVersion != strings.TrimPrefix(SchemaVersion, "v") {
			t.Errorf("%s yaml gemara-version = %q", name, yamlDocument.Metadata().GemaraVersion)
		}
		if jsonDocument.Metadata().Version != "release-A" {
			t.Errorf("%s json artifact version = %q", name, jsonDocument.Metadata().Version)
		}
	}
}

func TestEncodeArtifactPreservesKnownValues(t *testing.T) {
	policy := encoderPolicy()
	encoded, err := EncodeArtifactJSON(policy)
	if err != nil {
		t.Fatalf("EncodeArtifactJSON: %v", err)
	}
	var view map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &view); err != nil {
		t.Fatalf("encoded JSON is invalid: %v", err)
	}
	if string(view["title"]) != `"Synthetic Policy"` {
		t.Errorf("title = %s, want preserved value", view["title"])
	}
}
