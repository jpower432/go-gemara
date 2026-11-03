// SPDX-License-Identifier: Apache-2.0

package gemara

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func readForProjection(t *testing.T, raw string) *Document {
	t.Helper()
	doc, err := ReadDocument([]byte(raw))
	if err != nil {
		t.Fatalf("ReadDocument() error = %v", err)
	}
	return doc
}

func requireCompatibilityError(t *testing.T, err error) *CompatibilityError {
	t.Helper()
	var compatibility *CompatibilityError
	if !errors.As(err, &compatibility) {
		t.Fatalf("error = %v (%T), want *CompatibilityError", err, err)
	}
	if len(compatibility.Issues) == 0 {
		t.Fatal("CompatibilityError has no issues")
	}
	return compatibility
}

func TestProjectReportsUnknownField(t *testing.T) {
	raw := `{"metadata":{"id":"c","type":"ControlCatalog","gemara-version":"1.99.0"},"controls":[{"id":"C1","state":"Active","remarks":"keep me"}]}`
	projection, err := readForProjection(t, raw).Project()
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if _, ok := projection.Artifact.(*ControlCatalog); !ok {
		t.Fatalf("Artifact = %T, want *ControlCatalog", projection.Artifact)
	}
	if len(projection.Unsupported) != 1 {
		t.Fatalf("Unsupported = %+v, want one issue", projection.Unsupported)
	}
	issue := projection.Unsupported[0]
	if issue.Kind != "unknown_field" || issue.Path != "/controls/0/remarks" {
		t.Errorf("issue = %+v, want unknown_field at /controls/0/remarks", issue)
	}
	if issue.InputVersion != "1.99.0" || issue.SDKVersion != SchemaVersion {
		t.Errorf("issue versions = %q/%q, want 1.99.0/%s", issue.InputVersion, issue.SDKVersion, SchemaVersion)
	}
}

func TestProjectReportsUnknownFieldYAML(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.99.0
controls:
  - id: C1
    state: Active
    remarks: keep me
`
	projection, err := readForProjection(t, raw).Project()
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if len(projection.Unsupported) != 1 || projection.Unsupported[0].Path != "/controls/0/remarks" {
		t.Fatalf("Unsupported = %+v, want unknown_field at /controls/0/remarks", projection.Unsupported)
	}
}

func TestProjectReportsNestedUnknownField(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.5.0
controls:
  - id: C1
    state: Active
    assessment-requirements:
      - id: R1
        state: Active
        future-extension: yes
`
	projection, err := readForProjection(t, raw).Project()
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	wantPath := "/controls/0/assessment-requirements/0/future-extension"
	if len(projection.Unsupported) != 1 || projection.Unsupported[0].Path != wantPath {
		t.Fatalf("Unsupported = %+v, want unknown_field at %s", projection.Unsupported, wantPath)
	}
}

func TestProjectEscapesJSONPointerTokens(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.5.0
controls:
  - id: C1
    state: Active
    md~/x: value
`
	projection, err := readForProjection(t, raw).Project()
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	wantPath := "/controls/0/md~0~1x"
	if len(projection.Unsupported) != 1 || projection.Unsupported[0].Path != wantPath {
		t.Fatalf("Unsupported = %+v, want unknown_field at %s", projection.Unsupported, wantPath)
	}
}

func TestProjectSortsUnsupportedIssues(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.5.0
zeta: 1
alpha: 2
controls:
  - id: C1
    state: Active
    zulu: 3
    bravo: 4
`
	projection, err := readForProjection(t, raw).Project()
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	var paths []string
	for _, issue := range projection.Unsupported {
		paths = append(paths, issue.Path)
	}
	want := []string{"/alpha", "/controls/0/bravo", "/controls/0/zulu", "/zeta"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestProjectRejectsUnknownLifecycle(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.99.0
controls:
  - id: C1
    state: future-state
`
	projection, err := readForProjection(t, raw).Project()
	if projection != nil {
		t.Errorf("Project() = %+v, want no usable projection", projection)
	}
	compatibility := requireCompatibilityError(t, err)
	issue := compatibility.Issues[0]
	if issue.Kind != "unknown_enum" || issue.Path != "/controls/0/state" {
		t.Errorf("issue = %+v, want unknown_enum at /controls/0/state", issue)
	}
	if issue.InputVersion != "1.99.0" || issue.SDKVersion != SchemaVersion {
		t.Errorf("issue versions = %q/%q, want 1.99.0/%s", issue.InputVersion, issue.SDKVersion, SchemaVersion)
	}
}

func TestProjectRejectsUnknownAssessmentRequirementLifecycle(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.5.0
controls:
  - id: C1
    state: Active
    assessment-requirements:
      - id: R1
        state: future-state
`
	_, err := readForProjection(t, raw).Project()
	compatibility := requireCompatibilityError(t, err)
	wantPath := "/controls/0/assessment-requirements/0/state"
	if len(compatibility.Issues) != 1 || compatibility.Issues[0].Path != wantPath {
		t.Fatalf("issues = %+v, want unknown_enum at %s", compatibility.Issues, wantPath)
	}
}

func TestProjectRejectsUnsupportedArtifactType(t *testing.T) {
	raw := `
metadata:
  id: c
  type: FutureCatalog
  gemara-version: 1.99.0
`
	projection, err := readForProjection(t, raw).Project()
	if projection != nil {
		t.Errorf("Project() = %+v, want no usable projection", projection)
	}
	compatibility := requireCompatibilityError(t, err)
	issue := compatibility.Issues[0]
	if issue.Kind != "unsupported_type" || issue.Path != "/metadata/type" {
		t.Errorf("issue = %+v, want unsupported_type at /metadata/type", issue)
	}
}

func TestProjectRejectsWrongShape(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.5.0
controls: not-a-list
`
	projection, err := readForProjection(t, raw).Project()
	if projection != nil {
		t.Errorf("Project() = %+v, want no usable projection", projection)
	}
	compatibility := requireCompatibilityError(t, err)
	issue := compatibility.Issues[0]
	if issue.Kind != "unsupported_value" || issue.Path != "/controls" {
		t.Errorf("issue = %+v, want unsupported_value at /controls", issue)
	}
}

func TestProjectRejectsWrongScalarShape(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.5.0
title:
  - not
  - a
  - string
`
	_, err := readForProjection(t, raw).Project()
	compatibility := requireCompatibilityError(t, err)
	issue := compatibility.Issues[0]
	if issue.Kind != "unsupported_value" || issue.Path != "/title" {
		t.Errorf("issue = %+v, want unsupported_value at /title", issue)
	}
}

func TestProjectPreservesKnownValuesAndExtensions(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.99.0
title: Catalog
controls:
  - &control
    id: C1
    title: Control
    objective: Objective
    group: G1
    state: Active
    assessment-requirements:
      - id: R1
        text: Requirement
        applicability: [all]
        state: Active
  - *control
future-top-level:
  keep: me
`
	projection, err := readForProjection(t, raw).Project()
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	catalog, ok := projection.Artifact.(*ControlCatalog)
	if !ok {
		t.Fatalf("Artifact = %T, want *ControlCatalog", projection.Artifact)
	}
	if catalog.Title != "Catalog" || len(catalog.Controls) != 2 || catalog.Controls[1].Id != "C1" {
		t.Errorf("catalog = %+v, want projected alias content", catalog)
	}
	if len(projection.Unsupported) != 1 || projection.Unsupported[0].Path != "/future-top-level" {
		t.Errorf("Unsupported = %+v, want /future-top-level", projection.Unsupported)
	}
	known, err := json.Marshal(projection.jsonValue)
	if err != nil {
		t.Fatalf("marshal known projection: %v", err)
	}
	if !strings.Contains(string(known), `"title":"Catalog"`) {
		t.Errorf("known projection JSON = %s, want title", known)
	}
}

func TestProjectYAMLAndJSONProseEqual(t *testing.T) {
	yamlRaw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.5.0
title: "prose <tag> & \"quotes\" | pipes ` + "`ticks`" + `"
controls: []
`
	jsonRaw := `{"metadata":{"id":"c","type":"ControlCatalog","gemara-version":"1.5.0"},"title":"prose \u003ctag\u003e \u0026 \"quotes\" | pipes ` + "`ticks`" + `","controls":[]}`

	yamlProjection, err := readForProjection(t, yamlRaw).Project()
	if err != nil {
		t.Fatalf("YAML Project() error = %v", err)
	}
	jsonProjection, err := readForProjection(t, jsonRaw).Project()
	if err != nil {
		t.Fatalf("JSON Project() error = %v", err)
	}
	if yamlProjection.Artifact.(*ControlCatalog).Title != jsonProjection.Artifact.(*ControlCatalog).Title {
		t.Errorf("titles differ: %q vs %q",
			yamlProjection.Artifact.(*ControlCatalog).Title,
			jsonProjection.Artifact.(*ControlCatalog).Title)
	}
}

func TestProjectReturnsNoProjectionOnUnknownFieldAndUnknownEnum(t *testing.T) {
	raw := `
metadata:
  id: c
  type: ControlCatalog
  gemara-version: 1.5.0
controls:
  - id: C1
    state: future-state
    remarks: keep
`
	_, err := readForProjection(t, raw).Project()
	compatibility := requireCompatibilityError(t, err)
	var kinds []string
	for _, issue := range compatibility.Issues {
		kinds = append(kinds, issue.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"unknown_field", "unknown_enum"}) {
		t.Errorf("issue kinds = %v, want [unknown_field unknown_enum]", kinds)
	}
	if compatibility.Issues[0].Path != "/controls/0/remarks" {
		t.Errorf("first issue path = %q, want /controls/0/remarks", compatibility.Issues[0].Path)
	}
}

func TestDecodeJSONPreservesEscapedProse(t *testing.T) {
	raw := []byte(`{"metadata":{"id":"c","type":"ControlCatalog","gemara-version":"1.5.0"},"title":"prose \u003ctag\u003e \u0026 \"quotes\" | pipes ` + "`ticks`" + `","controls":[]}`)
	catalog, err := Decode[ControlCatalog](raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	want := "prose <tag> & \"quotes\" | pipes `ticks`"
	if catalog.Title != want {
		t.Errorf("title = %q, want %q", catalog.Title, want)
	}
}

func TestProjectRejectsCyclicPayloadAlias(t *testing.T) {
	raw := `metadata:
  id: cyclic-audit
  type: AuditLog
  gemara-version: '1.99.0'
summary: Synthetic cyclic payload.
criteria: []
results:
- id: result
  title: Result
  type: Observation
  description: Synthetic result.
  criteria-reference: {}
  evidence:
  - id: evidence
    type: log
    collected-at: '2026-01-01T00:00:00Z'
    payload: &payload
      self: *payload
target: {}
`
	projection, err := readForProjection(t, raw).Project()
	if projection != nil {
		t.Fatalf("Project() = %#v, want no projection for a cyclic alias", projection)
	}
	compatibility := requireCompatibilityError(t, err)
	if compatibility.Issues[0].Kind != "unsupported_value" ||
		compatibility.Issues[0].Path != "/results/0/evidence/0/payload/self" {
		t.Fatalf("issues = %#v, want unsupported cyclic payload path", compatibility.Issues)
	}
}
