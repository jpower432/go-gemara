// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"slices"
	"testing"

	"github.com/gemaraproj/go-gemara"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An explicit graph must never cross the acquisition boundary, even on error.
type forbiddenGraphFetcher struct{}

func (forbiddenGraphFetcher) Fetch(context.Context, string) (io.ReadCloser, error) {
	panic("resolved graph attempted acquisition")
}

func resolvedPolicy(t *testing.T, refs []gemara.MappingReference, imports gemara.Imports) []byte {
	t.Helper()
	return append([]byte("# authored policy: preserve comments and URLs\n"), mustMarshal(t, gemara.Policy{
		Title:    "Resolved policy",
		Metadata: gemara.Metadata{Id: "root", Type: gemara.PolicyArtifact, Version: "1.3.0", GemaraVersion: "1.0.0", Author: testAuthor, MappingReferences: refs},
		Imports:  imports,
	})...)
}

func resolvedCatalog(t *testing.T, id string, refs []gemara.MappingReference, imports ...string) []byte {
	t.Helper()
	var mappings []gemara.MultiEntryMapping
	for _, id := range imports {
		mappings = append(mappings, gemara.MultiEntryMapping{ReferenceId: id})
	}
	return append([]byte("# authored catalog\n"), mustMarshal(t, testControlCatalogWithVersion(id, "1.0.0", refs, nil, mappings))...)
}

// Both basename collision and context-free shared.yaml collapse occurred in
// legacy assembly. Each owner here selects its own explicit target.
func resolvedFixture(t *testing.T) (File, ResolvedGraph) {
	t.Helper()
	root := resolvedPolicy(t, []gemara.MappingReference{{Id: "A", Url: "a/catalog.yaml"}, {Id: "B", Url: "b/catalog.yaml"}}, gemara.Imports{Catalogs: []gemara.CatalogImport{{ReferenceId: "A"}, {ReferenceId: "B"}}})
	g := ResolvedGraph{
		Root: "r",
		Nodes: []ResolvedNode{
			{Key: "r", Data: root},
			{Key: "a", Data: resolvedCatalog(t, "A", []gemara.MappingReference{{Id: "SA", Url: "shared.yaml"}}, "SA")},
			{Key: "b", Data: resolvedCatalog(t, "B", []gemara.MappingReference{{Id: "SB", Url: "shared.yaml"}}, "SB")},
			{Key: "sa", Data: resolvedCatalog(t, "SA", nil)},
			{Key: "sb", Data: resolvedCatalog(t, "SB", nil)},
		},
		Edges: []ResolvedEdge{
			{From: "r", To: "a", Relationship: "catalog", Ordinal: 0, ReferenceID: "A", DeclaredURL: "a/catalog.yaml"},
			{From: "r", To: "b", Relationship: "catalog", Ordinal: 1, ReferenceID: "B", DeclaredURL: "b/catalog.yaml"},
			{From: "a", To: "sa", Relationship: "import", Ordinal: 0, ReferenceID: "SA", DeclaredURL: "shared.yaml"},
			{From: "b", To: "sb", Relationship: "import", Ordinal: 0, ReferenceID: "SB", DeclaredURL: "shared.yaml"},
		},
	}
	return File{Name: "policy.yaml", Data: root}, g
}

func assembleGraph(t *testing.T, source File, g ResolvedGraph, opts ...AssembleOption) *Bundle {
	t.Helper()
	opts = append([]AssembleOption{WithResolvedGraph(g)}, opts...)
	b, err := NewAssembler(forbiddenGraphFetcher{}).Assemble(context.Background(), Manifest{GemaraVersion: "1.0.0"}, source, opts...)
	require.NoError(t, err)
	return b
}

func graphInventory(t *testing.T, b *Bundle) map[string]Artifact {
	t.Helper()
	byID := map[string]Artifact{}
	names := map[string]bool{}
	for _, art := range b.Manifest.Artifacts {
		require.NotContains(t, byID, art.ID)
		require.False(t, names[art.Name], "duplicate name %q", art.Name)
		byID[art.ID], names[art.Name] = art, true
	}
	return byID
}

func TestResolvedGraphPreservesContextAndAdjacency(t *testing.T) {
	source, g := resolvedFixture(t)
	b := assembleGraph(t, source, g)
	require.Len(t, b.Imports, 4)
	assert.Equal(t, "1.3.0", b.Manifest.BundleVersion)
	assert.Equal(t, "Policy", b.Source.Type)
	assert.Equal(t, source.Data, b.Source.Data)
	byID := graphInventory(t, b)
	require.Len(t, byID, 5)
	assert.Equal(t, []string{byID["A"].Name, byID["B"].Name}, byID["root"].Dependencies)
	assert.Equal(t, []string{byID["SA"].Name}, byID["A"].Dependencies)
	assert.Equal(t, []string{byID["SB"].Name}, byID["B"].Dependencies)
	assert.Empty(t, byID["SA"].Dependencies)
	assert.Empty(t, byID["SB"].Dependencies)
	var names []string
	for _, f := range b.Imports {
		assert.Equal(t, fmt.Sprintf("sha256-%x.yaml", sha256.Sum256(f.Data)), f.Name)
		assert.Equal(t, "ControlCatalog", f.Type)
		names = append(names, f.Name)
	}
	assert.True(t, slices.IsSorted(names))
	slices.Reverse(g.Nodes)
	slices.Reverse(g.Edges)
	reordered := assembleGraph(t, source, g)
	assert.Equal(t, b, reordered, "graph input order must not select filenames or adjacency")
}

func TestResolvedGraphRejectsInvalidGraphsWithoutFallback(t *testing.T) {
	tests := []struct {
		name string
		edit func(*File, *ResolvedGraph)
		want string
	}{
		{"empty graph", func(_ *File, g *ResolvedGraph) { *g = ResolvedGraph{} }, "root"},
		{"missing root", func(_ *File, g *ResolvedGraph) { g.Root = "absent" }, "root"},
		{"empty key", func(_ *File, g *ResolvedGraph) { g.Nodes[1].Key = "" }, "key"},
		{"duplicate key", func(_ *File, g *ResolvedGraph) { g.Nodes = append(g.Nodes, g.Nodes[1]) }, "key"},
		{"source bytes mismatch", func(_ *File, g *ResolvedGraph) { g.Nodes[0].Data = append(bytes.Clone(g.Nodes[0].Data), '\n') }, "root"},
		{"source name empty", func(f *File, _ *ResolvedGraph) { f.Name = "" }, "source"},
		{"missing slot", func(_ *File, g *ResolvedGraph) { g.Edges = g.Edges[:3] }, "slot"},
		{"duplicate slot", func(_ *File, g *ResolvedGraph) { g.Edges = append(g.Edges, g.Edges[0]) }, "slot"},
		{"negative ordinal", func(_ *File, g *ResolvedGraph) { g.Edges[0].Ordinal = -1 }, "slot"},
		{"extra ordinal", func(_ *File, g *ResolvedGraph) { g.Edges[0].Ordinal = 99 }, "slot"},
		{"missing owner", func(_ *File, g *ResolvedGraph) { g.Edges[0].From = "absent" }, "endpoint"},
		{"missing target", func(_ *File, g *ResolvedGraph) { g.Edges[0].To = "absent" }, "endpoint"},
		{"wrong relationship", func(_ *File, g *ResolvedGraph) { g.Edges[0].Relationship = "extends" }, "slot"},
		{"wrong reference", func(_ *File, g *ResolvedGraph) { g.Edges[0].ReferenceID = "other" }, "slot"},
		{"wrong URL", func(_ *File, g *ResolvedGraph) { g.Edges[0].DeclaredURL += " " }, "slot"},
		{"extra leaf edge", func(_ *File, g *ResolvedGraph) { g.Edges = append(g.Edges, ResolvedEdge{From: "sa", To: "sb"}) }, "slot"},
		{"unreachable analysis input", func(_ *File, g *ResolvedGraph) {
			g.Nodes = append(g.Nodes, ResolvedNode{Key: "analysis", Data: resolvedCatalog(t, "analysis", nil)})
		}, "unreachable"},
		{"malformed artifact", func(_ *File, g *ResolvedGraph) { g.Nodes[1].Data = []byte("[broken") }, "a"},
		{"missing type", func(_ *File, g *ResolvedGraph) { g.Nodes[3].Data = []byte("metadata:\n  id: SA\n") }, "type"},
		{"empty identity", func(_ *File, g *ResolvedGraph) { g.Nodes[1].Data = []byte("metadata:\n  type: ControlCatalog\n") }, "identity"},
		{"duplicate references", func(_ *File, g *ResolvedGraph) {
			g.Nodes[1].Data = resolvedCatalog(t, "A", []gemara.MappingReference{{Id: "SA"}, {Id: "SA"}}, "SA")
		}, "mapping-reference"},
		{"missing mapping declaration", func(_ *File, g *ResolvedGraph) { g.Nodes[1].Data = resolvedCatalog(t, "A", nil, "SA") }, "mapping-reference"},
		{"empty operation ID", func(_ *File, g *ResolvedGraph) { g.Nodes[1].Data = resolvedCatalog(t, "A", nil, "") }, "slot"},
		{"conflicting identity", func(_ *File, g *ResolvedGraph) { g.Nodes[4].Data = append(bytes.Clone(g.Nodes[3].Data), '\n') }, "identity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, g := resolvedFixture(t)
			tt.edit(&source, &g)
			b, err := NewAssembler(forbiddenGraphFetcher{}).Assemble(context.Background(), Manifest{}, source, WithResolvedGraph(g), WithContinueOnError())
			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, b)
		})
	}
}

func TestResolvedGraphBindingsWarningsAndMetadataOnlyReferences(t *testing.T) {
	for _, url := range []string{"", "git+https://example.org/repo.git?ref=v1#catalog.yaml"} {
		t.Run(url, func(t *testing.T) {
			source := File{Name: "policy.yaml", Data: resolvedPolicy(t, []gemara.MappingReference{{Id: "alias", Url: url}, {Id: "informational"}, {Id: "analysis-only", Url: "https://example.org/mapping.yaml"}}, gemara.Imports{Catalogs: []gemara.CatalogImport{{ReferenceId: "alias"}}})}
			g := ResolvedGraph{Root: "r", Nodes: []ResolvedNode{{Key: "r", Data: source.Data}, {Key: "bound", Data: resolvedCatalog(t, "actual-target", nil)}}, Edges: []ResolvedEdge{{From: "r", To: "bound", Relationship: "catalog", ReferenceID: "alias", DeclaredURL: url}}}
			_, err := NewAssembler(forbiddenGraphFetcher{}).Assemble(context.Background(), Manifest{}, source, WithResolvedGraph(g))
			require.ErrorContains(t, err, "informational")
			b := assembleGraph(t, source, g, WithContinueOnError())
			require.Len(t, b.Imports, 1)
			require.Len(t, b.Warnings, 1)
			assert.Equal(t, "informational", b.Warnings[0].ReferenceID)
			assert.Equal(t, []string{b.Imports[0].Name}, b.Manifest.Artifacts[0].Dependencies)
			g.Edges = nil
			_, err = NewAssembler(forbiddenGraphFetcher{}).Assemble(context.Background(), Manifest{}, source, WithResolvedGraph(g), WithContinueOnError())
			require.ErrorContains(t, err, "slot")
		})
	}
}

func TestResolvedGraphCoalescesMirrorsAndRejectsIncompatibleAdjacency(t *testing.T) {
	source, g := resolvedFixture(t)
	// B's separate context becomes a byte-identical mirror of A.
	g.Nodes[2].Data = bytes.Clone(g.Nodes[1].Data)
	g.Nodes[4].Data = bytes.Clone(g.Nodes[3].Data)
	g.Edges[3].ReferenceID = "SA"
	b := assembleGraph(t, source, g)
	require.Len(t, b.Imports, 2)
	byID := graphInventory(t, b)
	assert.Equal(t, []string{byID["A"].Name, byID["A"].Name}, byID["root"].Dependencies)
	assert.Equal(t, []string{byID["SA"].Name}, byID["A"].Dependencies)
	// Same parent bytes cannot silently merge different selected targets.
	g.Nodes[4].Data = resolvedCatalog(t, "SB", nil)
	_, err := NewAssembler(forbiddenGraphFetcher{}).Assemble(context.Background(), Manifest{}, source, WithResolvedGraph(g))
	require.ErrorContains(t, err, "adjacency")
}

func TestResolvedGraphRootNameCollision(t *testing.T) {
	source, g := resolvedFixture(t)
	digest := sha256.Sum256(g.Nodes[1].Data)
	source.Name = fmt.Sprintf("sha256-%x.yaml", digest)
	b := assembleGraph(t, source, g)
	byID := graphInventory(t, b)
	assert.Equal(t, source.Name, byID["root"].Name)
	assert.Equal(t, fmt.Sprintf("sha256-%x-import.yaml", digest), byID["A"].Name)
	assert.Equal(t, []string{byID["A"].Name, byID["B"].Name}, byID["root"].Dependencies)
}

func TestResolvedGraphDiamondSharedArtifact(t *testing.T) {
	source, g := resolvedFixture(t)
	g.Nodes[2].Data = resolvedCatalog(t, "B", []gemara.MappingReference{{Id: "SA", Url: "shared.yaml"}}, "SA")
	g.Nodes[4].Data = bytes.Clone(g.Nodes[3].Data)
	g.Edges[3].ReferenceID = "SA"
	b := assembleGraph(t, source, g)
	require.Len(t, b.Imports, 3)
	byID := graphInventory(t, b)
	assert.Equal(t, []string{byID["SA"].Name}, byID["A"].Dependencies)
	assert.Equal(t, []string{byID["SA"].Name}, byID["B"].Dependencies)
}

func TestResolvedGraphSnapshotsInputAndOptionCanBeReused(t *testing.T) {
	source, g := resolvedFixture(t)
	wantRoot := bytes.Clone(source.Data)
	option := WithResolvedGraph(g)
	// An option is a snapshot, not a deferred view of mutable caller storage.
	g.Nodes[0].Data[0] = '!'
	g.Nodes[1].Key = "changed"
	g.Edges[0].To = "changed"
	source.Data = wantRoot
	b, err := NewAssembler(nil).Assemble(context.Background(), Manifest{}, source, option)
	require.NoError(t, err)
	b.Source.Data[0] = '!'
	b.Imports[0].Data[0] = '!'
	again, err := NewAssembler(nil).Assemble(context.Background(), Manifest{}, source, option)
	require.NoError(t, err)
	assert.Equal(t, wantRoot, again.Source.Data)
	for _, f := range again.Imports {
		assert.Equal(t, byte('#'), f.Data[0])
	}
}

func TestResolvedGraphCancellation(t *testing.T) {
	source, g := resolvedFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b, err := NewAssembler(forbiddenGraphFetcher{}).Assemble(ctx, Manifest{}, source, WithResolvedGraph(g))
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, b)
}

func TestResolvedGraphTypedOperationOrder(t *testing.T) {
	root := resolvedPolicy(t, []gemara.MappingReference{{Id: "P"}, {Id: "C"}, {Id: "G"}}, gemara.Imports{
		Policies: []gemara.ArtifactMapping{{ReferenceId: "P"}},
		Catalogs: []gemara.CatalogImport{{ReferenceId: "C"}, {ReferenceId: "C"}},
		Guidance: []gemara.GuidanceImport{{ReferenceId: "G"}},
	})
	catalog := mustMarshal(t, testControlCatalog("C", []gemara.MappingReference{{Id: "CX"}, {Id: "CI"}}, []gemara.ArtifactMapping{{ReferenceId: "CX"}}, []gemara.MultiEntryMapping{{ReferenceId: "CI"}}))
	guidance := testGuidanceCatalog("G")
	guidance.Metadata.MappingReferences = []gemara.MappingReference{{Id: "GX"}, {Id: "GI"}}
	guidance.Extends = []gemara.ArtifactMapping{{ReferenceId: "GX"}}
	guidance.Imports = []gemara.MultiEntryMapping{{ReferenceId: "GI"}}
	g := ResolvedGraph{Root: "root", Nodes: []ResolvedNode{
		{Key: "root", Data: root},
		{Key: "P", Data: resolvedPolicy(t, nil, gemara.Imports{})},
		{Key: "C", Data: catalog}, {Key: "G", Data: mustMarshal(t, guidance)},
		{Key: "CX", Data: resolvedCatalog(t, "CX", nil)}, {Key: "CI", Data: resolvedCatalog(t, "CI", nil)},
		{Key: "GX", Data: mustMarshal(t, testGuidanceCatalog("GX"))}, {Key: "GI", Data: mustMarshal(t, testGuidanceCatalog("GI"))},
	}, Edges: []ResolvedEdge{
		{From: "root", To: "G", Relationship: "guidance", Ordinal: 3, ReferenceID: "G"},
		{From: "root", To: "C", Relationship: "catalog", Ordinal: 2, ReferenceID: "C"},
		{From: "root", To: "P", Relationship: "policy", Ordinal: 0, ReferenceID: "P"},
		{From: "root", To: "C", Relationship: "catalog", Ordinal: 1, ReferenceID: "C"},
		{From: "C", To: "CI", Relationship: "import", Ordinal: 1, ReferenceID: "CI"},
		{From: "C", To: "CX", Relationship: "extends", Ordinal: 0, ReferenceID: "CX"},
		{From: "G", To: "GI", Relationship: "import", Ordinal: 1, ReferenceID: "GI"},
		{From: "G", To: "GX", Relationship: "extends", Ordinal: 0, ReferenceID: "GX"},
	}}
	// Give the imported policy its own identity, independent of its context key.
	g.Nodes[1].Data = bytes.Replace(g.Nodes[1].Data, []byte("id: root"), []byte("id: P"), 1)
	b := assembleGraph(t, File{Name: "policy.yaml", Data: root}, g)
	byID := graphInventory(t, b)
	assert.Equal(t, []string{byID["P"].Name, byID["C"].Name, byID["C"].Name, byID["G"].Name}, byID["root"].Dependencies)
	assert.Equal(t, []string{byID["CX"].Name, byID["CI"].Name}, byID["C"].Dependencies)
	assert.Equal(t, []string{byID["GX"].Name, byID["GI"].Name}, byID["G"].Dependencies)
}

func TestResolvedGraphCyclesCoalesceRootMirrors(t *testing.T) {
	for _, guidance := range []bool{false, true} {
		t.Run(fmt.Sprint(guidance), func(t *testing.T) {
			a := testControlCatalog("A", []gemara.MappingReference{{Id: "B"}}, []gemara.ArtifactMapping{{ReferenceId: "B"}}, nil)
			b := testControlCatalog("B", []gemara.MappingReference{{Id: "A"}}, nil, []gemara.MultiEntryMapping{{ReferenceId: "A"}})
			if guidance {
				a.Metadata.Type, b.Metadata.Type = gemara.GuidanceCatalogArtifact, gemara.GuidanceCatalogArtifact
			}
			rootBytes := mustMarshal(t, a)
			g := ResolvedGraph{Root: "root", Nodes: []ResolvedNode{{Key: "mirror", Data: rootBytes}, {Key: "root", Data: rootBytes}, {Key: "b", Data: mustMarshal(t, b)}}, Edges: []ResolvedEdge{
				{From: "root", To: "b", Relationship: "extends", ReferenceID: "B"},
				{From: "mirror", To: "b", Relationship: "extends", ReferenceID: "B"},
				{From: "b", To: "mirror", Relationship: "import", ReferenceID: "A"},
			}}
			built := assembleGraph(t, File{Name: "root.yaml", Data: rootBytes}, g)
			require.Len(t, built.Imports, 1)
			byID := graphInventory(t, built)
			assert.Equal(t, []string{byID["B"].Name}, byID["A"].Dependencies)
			assert.Equal(t, []string{"root.yaml"}, byID["B"].Dependencies)
			slices.Reverse(g.Nodes)
			assert.Equal(t, built, assembleGraph(t, File{Name: "root.yaml", Data: rootBytes}, g))
		})
	}
}

func TestResolvedGraphGitFragmentNamesAndURLAliases(t *testing.T) {
	source, g := resolvedFixture(t)
	original := assembleGraph(t, source, g)
	aURL := "git+https://example.org/controls.git?ref=v1#catalog-a.yaml"
	bURL := "git+https://example.org/controls.git?ref=v1#catalog-b.yaml"
	g.Nodes[0].Data = resolvedPolicy(t, []gemara.MappingReference{{Id: "A", Url: aURL}, {Id: "B", Url: bURL}}, gemara.Imports{Catalogs: []gemara.CatalogImport{{ReferenceId: "A"}, {ReferenceId: "B"}}})
	g.Edges[0].DeclaredURL, g.Edges[1].DeclaredURL = aURL, bURL
	source.Data = g.Nodes[0].Data
	b := assembleGraph(t, source, g)
	assert.Equal(t, original.Imports, b.Imports, "import names depend on content, not authored transport URLs")
	assert.Equal(t, original.Manifest, b.Manifest)
	assert.Contains(t, string(b.Source.Data), aURL)
	assert.Contains(t, string(b.Source.Data), bURL)
}

func TestResolvedGraphSingleSourceAndLegacyBehavior(t *testing.T) {
	source := File{Name: "policy.yaml", Data: resolvedPolicy(t, nil, gemara.Imports{})}
	g := ResolvedGraph{Root: "r", Nodes: []ResolvedNode{{Key: "r", Data: source.Data}}}
	b := assembleGraph(t, source, g)
	assert.Empty(t, b.Imports)
	assert.Len(t, b.Manifest.Artifacts, 1)
	legacy, err := NewAssembler(forbiddenGraphFetcher{}).Assemble(context.Background(), Manifest{}, source)
	require.NoError(t, err)
	assert.Equal(t, source, legacy.Source, "legacy source Type and raw bytes remain untouched")
}

func ExampleWithResolvedGraph() {
	root := []byte("metadata:\n  id: root\n  type: ControlCatalog\n  mapping-references:\n    - id: bound-alias\nimports:\n  - reference-id: bound-alias\n")
	target := []byte("metadata:\n  id: actual-target\n  type: ControlCatalog\n")
	g := ResolvedGraph{Root: "root-context", Nodes: []ResolvedNode{{Key: "root-context", Data: root}, {Key: "target-context", Data: target}}, Edges: []ResolvedEdge{
		{From: "root-context", To: "target-context", Relationship: "import", ReferenceID: "bound-alias"},
	}}
	b, err := NewAssembler(nil).Assemble(context.Background(), Manifest{}, File{Name: "root.yaml", Data: root}, WithResolvedGraph(g))
	if err != nil {
		panic(err)
	}
	fmt.Println(b.Source.Type, len(b.Imports))
	// Output: ControlCatalog 1
}
