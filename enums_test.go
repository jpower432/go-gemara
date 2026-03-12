package gemara

import (
	"testing"
)

func TestResultString(t *testing.T) {
	tests := []struct {
		name     string
		result   Result
		expected string
	}{
		{
			result:   Passed,
			expected: "Passed",
		},
		{
			result:   Failed,
			expected: "Failed",
		},
		{
			result:   NeedsReview,
			expected: "Needs Review",
		},
		{
			result:   NotRun,
			expected: "Not Run",
		},
		{
			result:   NotApplicable,
			expected: "Not Applicable",
		},
		{
			result:   Unknown,
			expected: "Unknown",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := test.result.String()
			if actual != test.expected {
				t.Errorf("expected %q, got %q", test.expected, actual)
			}
		})
	}
}

func TestUpdateAggregateResult(t *testing.T) {
	tests := []struct {
		name     string
		prev     Result
		new      Result
		expected Result
	}{
		{
			name:     "NotRun should not overwrite anything",
			prev:     Passed,
			new:      NotRun,
			expected: Passed,
		},
		{
			name:     "Failed should not be overwritten by anything",
			prev:     Failed,
			new:      Passed,
			expected: Failed,
		},
		{
			name:     "Failed should overwrite anything",
			prev:     Passed,
			new:      Failed,
			expected: Failed,
		},
		{
			name:     "Unknown should not be overwritten by NeedsReview",
			prev:     Unknown,
			new:      NeedsReview,
			expected: Unknown,
		},
		{
			name:     "Unknown should not be overwritten by Passed",
			prev:     Unknown,
			new:      Passed,
			expected: Unknown,
		},
		{
			name:     "NeedsReview should not be overwritten by Passed",
			prev:     NeedsReview,
			new:      Passed,
			expected: NeedsReview,
		},
		{
			name:     "NeedsReview should overwrite Passed",
			prev:     Passed,
			new:      NeedsReview,
			expected: NeedsReview,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := UpdateAggregateResult(test.prev, test.new)
			if actual != test.expected {
				t.Errorf("expected %s, got %s", test.expected, actual)
			}
		})
	}
}

func TestArtifactTypeString(t *testing.T) {
	tests := []struct {
		v        ArtifactType
		expected string
	}{
		{ControlCatalogArtifact, "ControlCatalog"},
		{EvaluationLogArtifact, "EvaluationLog"},
		{GuidanceCatalogArtifact, "GuidanceCatalog"},
		{MappingDocumentArtifact, "MappingDocument"},
		{PolicyArtifact, "Policy"},
		{ThreatCatalogArtifact, "ThreatCatalog"},
		{VectorCatalogArtifact, "VectorCatalog"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestLifecycleString(t *testing.T) {
	tests := []struct {
		v        Lifecycle
		expected string
	}{
		{LifecycleActive, "Active"},
		{LifecycleDraft, "Draft"},
		{LifecycleDeprecated, "Deprecated"},
		{LifecycleRetired, "Retired"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestEntryTypeString(t *testing.T) {
	tests := []struct {
		v        EntryType
		expected string
	}{
		{EntryTypeGuideline, "Guideline"},
		{EntryTypeStatement, "Statement"},
		{EntryTypeControl, "Control"},
		{EntryTypeAssessmentRequirement, "AssessmentRequirement"},
		{EntryTypeVector, "Vector"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestConfidenceLevelString(t *testing.T) {
	tests := []struct {
		v        ConfidenceLevel
		expected string
	}{
		{Undetermined, "Undetermined"},
		{Low, "Low"},
		{Medium, "Medium"},
		{High, "High"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRelationshipTypeString(t *testing.T) {
	tests := []struct {
		v        RelationshipType
		expected string
	}{
		{RelImplements, "implements"},
		{RelImplementedBy, "implemented-by"},
		{RelSupports, "supports"},
		{RelSupportedBy, "supported-by"},
		{RelEquivalent, "equivalent"},
		{RelSubsumes, "subsumes"},
		{RelNoMatch, "no-match"},
		{RelRelatesTo, "relates-to"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestMethodTypeString(t *testing.T) {
	tests := []struct {
		v        MethodType
		expected string
	}{
		{MethodManual, "Manual"},
		{MethodBehavioral, "Behavioral"},
		{MethodAutomated, "Automated"},
		{MethodAutoremediation, "Autoremediation"},
		{MethodGate, "Gate"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSeverityString(t *testing.T) {
	tests := []struct {
		v        Severity
		expected string
	}{
		{SeverityLow, "Low"},
		{SeverityMedium, "Medium"},
		{SeverityHigh, "High"},
		{SeverityCritical, "Critical"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGuidanceTypeString(t *testing.T) {
	tests := []struct {
		v        GuidanceType
		expected string
	}{
		{GuidanceStandard, "Standard"},
		{GuidanceRegulation, "Regulation"},
		{GuidanceBestPractice, "Best Practice"},
		{GuidanceFramework, "Framework"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRiskAppetiteString(t *testing.T) {
	tests := []struct {
		v        RiskAppetite
		expected string
	}{
		{RiskAppetiteZero, "Zero"},
		{RiskAppetiteLow, "Low"},
		{RiskAppetiteModerate, "Moderate"},
		{RiskAppetiteHigh, "High"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestModTypeString(t *testing.T) {
	tests := []struct {
		v        ModType
		expected string
	}{
		{ModAdd, "Add"},
		{ModModify, "Modify"},
		{ModRemove, "Remove"},
		{ModReplace, "Replace"},
		{ModOverride, "Override"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestEntityTypeString(t *testing.T) {
	tests := []struct {
		v        EntityType
		expected string
	}{
		{Human, "Human"},
		{Software, "Software"},
		{SoftwareAssisted, "Software-Assisted"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.v.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestLifecycleMarshalUnmarshalJSON(t *testing.T) {
	var l Lifecycle
	if err := l.UnmarshalJSON([]byte(`"Draft"`)); err != nil {
		t.Fatal(err)
	}
	if l != LifecycleDraft {
		t.Errorf("UnmarshalJSON: got %v, want LifecycleDraft", l)
	}
	out, err := l.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"Draft"` {
		t.Errorf("MarshalJSON: got %s", out)
	}
}

func TestConfidenceLevelMarshalUnmarshalJSON(t *testing.T) {
	var c ConfidenceLevel
	if err := c.UnmarshalJSON([]byte(`"High"`)); err != nil {
		t.Fatal(err)
	}
	if c != High {
		t.Errorf("UnmarshalJSON: got %v, want High", c)
	}
	out, err := c.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"High"` {
		t.Errorf("MarshalJSON: got %s", out)
	}
}

func TestRelationshipTypeUnmarshalJSONInvalid(t *testing.T) {
	var r RelationshipType
	err := r.UnmarshalJSON([]byte(`"invalid"`))
	if err == nil {
		t.Error("expected error for invalid RelationshipType")
	}
}
