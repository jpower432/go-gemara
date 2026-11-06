// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	"github.com/gemaraproj/go-gemara"
)

type resolvedFile struct {
	parsed    *parsedFile
	digest    [sha256.Size]byte
	targets   []*resolvedFile // indexed by authored operation ordinal
	canonical *resolvedFile
}

// assembleResolved projects selected edges; it never selects a dependency or
// invokes a Fetcher. Graph validation precedes content coalescing, so byte-identical
// parents at distinct acquisition contexts cannot hide incompatible descendants.
func assembleResolved(ctx context.Context, m Manifest, source File, g ResolvedGraph, continueOnError bool) (*Bundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g.Root == "" {
		return nil, fmt.Errorf("resolved graph root key is required")
	}
	nodes := make(map[string]*resolvedFile, len(g.Nodes))
	identities := make(map[string]*resolvedFile, len(g.Nodes))
	contents := make(map[[sha256.Size]byte]*resolvedFile, len(g.Nodes))
	for _, node := range g.Nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.TrimSpace(node.Key) == "" || nodes[node.Key] != nil {
			return nil, fmt.Errorf("resolved graph node key %q is empty or duplicate", node.Key)
		}
		pf, err := parseFile(File{Name: node.Key, Data: node.Data})
		if err != nil {
			return nil, fmt.Errorf("resolved graph node %q: %w", node.Key, err)
		}
		if strings.TrimSpace(pf.id) == "" {
			return nil, fmt.Errorf("resolved graph node %q has empty artifact identity", node.Key)
		}
		if pf.artType == gemara.InvalidArtifact {
			return nil, fmt.Errorf("resolved graph node %q has missing artifact type", node.Key)
		}
		if err := validateResolvedSlots(node.Key, pf); err != nil {
			return nil, err
		}
		f := &resolvedFile{parsed: pf, digest: sha256.Sum256(node.Data), targets: make([]*resolvedFile, len(pf.refSlots))}
		if prior := identities[pf.id]; prior != nil && !bytes.Equal(prior.parsed.Data, node.Data) {
			return nil, fmt.Errorf("resolved graph artifact identity %q has conflicting content at %q and %q", pf.id, prior.parsed.Name, node.Key)
		}
		identities[pf.id] = f
		f.canonical = f
		if prior := contents[f.digest]; prior != nil {
			if !bytes.Equal(prior.parsed.Data, node.Data) {
				return nil, fmt.Errorf("resolved graph content digest collision at node %q", node.Key)
			}
			f.canonical = prior
		} else {
			contents[f.digest] = f
		}
		nodes[node.Key] = f
	}
	root := nodes[g.Root]
	if root == nil || !bytes.Equal(root.parsed.Data, source.Data) {
		return nil, fmt.Errorf("resolved graph root %q is absent or differs from source bytes", g.Root)
	}
	for _, edge := range g.Edges {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		from, to := nodes[edge.From], nodes[edge.To]
		if from == nil || to == nil {
			return nil, fmt.Errorf("resolved graph edge %q -> %q has an absent endpoint", edge.From, edge.To)
		}
		if edge.Ordinal < 0 || edge.Ordinal >= len(from.targets) {
			return nil, fmt.Errorf("resolved graph node %q has no required slot %d", edge.From, edge.Ordinal)
		}
		slot := from.parsed.refSlots[edge.Ordinal]
		if from.targets[edge.Ordinal] != nil {
			return nil, fmt.Errorf("resolved graph node %q has duplicate edge for slot %d", edge.From, edge.Ordinal)
		}
		if edge.Relationship != slot.relationship || edge.ReferenceID != slot.referenceID || edge.DeclaredURL != from.parsed.refURLs[slot.referenceID] {
			return nil, fmt.Errorf("resolved graph node %q slot %d (%s %q) differs from supplied relationship/reference/URL", edge.From, edge.Ordinal, slot.relationship, slot.referenceID)
		}
		from.targets[edge.Ordinal] = to
	}
	for _, node := range g.Nodes {
		f := nodes[node.Key]
		for i, target := range f.targets {
			if target == nil {
				return nil, fmt.Errorf("resolved graph node %q is missing required slot %d (%q)", node.Key, i, f.parsed.refSlots[i].referenceID)
			}
		}
	}
	// Reachability uses context nodes, not coalesced files. Extra acquisition or
	// analysis inputs cannot enter the bundle just because their bytes were supplied.
	visited := map[*resolvedFile]bool{}
	queue := []*resolvedFile{root}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if visited[f] {
			continue
		}
		visited[f] = true
		queue = append(queue, f.targets...)
	}
	for _, node := range g.Nodes {
		f := nodes[node.Key]
		if !visited[f] {
			return nil, fmt.Errorf("resolved graph node %q is unreachable from root %q", node.Key, g.Root)
		}
		for i, target := range f.targets {
			if target.canonical != f.canonical.targets[i].canonical {
				return nil, fmt.Errorf("resolved graph node %q has incompatible mirror adjacency at slot %d", node.Key, i)
			}
		}
	}
	// Reserve the root filename for its canonical content, even when an identical
	// mirror appeared earlier in the input slice.
	root = root.canonical
	root.parsed.Name = source.Name
	root.parsed.Type = root.parsed.artType.String()
	var imports []*resolvedFile
	for _, f := range contents {
		if f == root {
			continue
		}
		f.parsed.Name = fmt.Sprintf("sha256-%x.yaml", f.digest)
		if f.parsed.Name == source.Name {
			f.parsed.Name = fmt.Sprintf("sha256-%x-import.yaml", f.digest)
		}
		f.parsed.Type = f.parsed.artType.String()
		imports = append(imports, f)
	}
	slices.SortFunc(imports, func(a, b *resolvedFile) int { return strings.Compare(a.parsed.Name, b.parsed.Name) })
	all := append([]*resolvedFile{root}, imports...)
	parsed := make([]*parsedFile, 0, len(all))
	var files []File
	depMap := make(map[string][]string, len(all))
	names := make(map[string]bool, len(all))
	for i, f := range all {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if names[f.parsed.Name] {
			return nil, fmt.Errorf("resolved graph generated duplicate filename %q", f.parsed.Name)
		}
		names[f.parsed.Name] = true
		parsed = append(parsed, f.parsed)
		if i != 0 {
			files = append(files, f.parsed.File)
		}
		for _, target := range f.targets {
			depMap[f.parsed.Name] = append(depMap[f.parsed.Name], target.canonical.parsed.Name)
		}
	}
	warnings := validateMappingRefs(parsed)
	warnings = slices.DeleteFunc(warnings, func(w MappingWarning) bool {
		// These required references are satisfied by explicit bindings, even when
		// they have no URL and the selected artifact has a different metadata.id.
		for _, f := range all {
			if f.parsed.Name == w.File {
				return slices.Contains(f.parsed.refIDs, w.ReferenceID)
			}
		}
		return false
	})
	if len(warnings) > 0 && !continueOnError {
		return nil, mappingRefError(warnings)
	}
	if m.BundleVersion == "" {
		m.BundleVersion = root.parsed.version
	}
	m.Artifacts = buildArtifactTree(root.parsed, parsed[1:], depMap)
	return &Bundle{Manifest: m, Source: root.parsed.File, Imports: files, Warnings: warnings}, nil
}

func validateResolvedSlots(key string, pf *parsedFile) error {
	refs := make(map[string]bool, len(pf.mappingRefs))
	for _, ref := range pf.mappingRefs {
		if strings.TrimSpace(ref.Id) == "" || refs[ref.Id] {
			return fmt.Errorf("resolved graph node %q has empty or duplicate mapping-reference %q", key, ref.Id)
		}
		refs[ref.Id] = true
	}
	for i, slot := range pf.refSlots {
		if slot.referenceID == "" || !refs[slot.referenceID] {
			return fmt.Errorf("resolved graph node %q slot %d has absent mapping-reference %q", key, i, slot.referenceID)
		}
	}
	return nil
}
