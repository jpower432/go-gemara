// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"

	"github.com/gemaraproj/go-gemara"
	"github.com/gemaraproj/go-gemara/internal/codec"
)

// Assembler walks the full import graph of Gemara artifacts and produces
// a self-contained Bundle with all transitive dependencies fetched. WithResolvedGraph
// instead supplies a complete graph without acquisition.
type Assembler struct {
	fetcher gemara.Fetcher
}

// NewAssembler creates an Assembler backed by the given Fetcher. The Fetcher may
// be nil when Assemble is called with WithResolvedGraph.
func NewAssembler(f gemara.Fetcher) *Assembler {
	return &Assembler{fetcher: f}
}

// parsedFile pairs a bundle File with its parsed outbound reference data.
type parsedFile struct {
	File
	id          string
	version     string
	artType     gemara.ArtifactType
	refIDs      []string
	refURLs     map[string]string
	mappingRefs []gemara.MappingReference
	refSlots    []referenceSlot
}

// referenceSlot retains authored operation order, including empty IDs so explicit
// graphs can reject malformed slots rather than silently skipping them.
type referenceSlot struct {
	relationship string
	referenceID  string
}

// parseFile decodes a Gemara YAML file into either a gemara.Catalog
// or gemara.Policy and extracts the identity and outbound reference
// information needed for dependency resolution.
func parseFile(f File) (*parsedFile, error) {
	pf := &parsedFile{File: f}
	artType, err := gemara.DetectType(f.Data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", f.Name, err)
	}

	switch artType {
	case gemara.PolicyArtifact:
		var pol gemara.Policy
		if err := codec.UnmarshalYAML(f.Data, &pol); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f.Name, err)
		}
		pf.id = pol.Metadata.Id
		pf.version = pol.Metadata.Version
		pf.artType = pol.Metadata.Type
		pf.mappingRefs = pol.Metadata.MappingReferences
		pf.refURLs = mappingRefURLs(pol.Metadata.MappingReferences)
		pf.refSlots = policyReferenceSlots(pol.Imports)
	default:
		var cat gemara.Catalog
		if err := codec.UnmarshalYAML(f.Data, &cat); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f.Name, err)
		}
		pf.id = cat.Metadata.Id
		pf.version = cat.Metadata.Version
		pf.artType = cat.Metadata.Type
		pf.mappingRefs = cat.Metadata.MappingReferences
		pf.refURLs = mappingRefURLs(cat.Metadata.MappingReferences)
		pf.refSlots = catalogReferenceSlots(cat.Extends, cat.Imports)
	}
	pf.refIDs = referenceIDs(pf.refSlots)

	return pf, nil
}

// Assemble parses the source file, fetches every artifact referenced in
// its extends and imports via mapping-references URLs, then recursively
// parses fetched artifacts for their own references until the full
// dependency tree is resolved. WithResolvedGraph instead validates and projects
// the supplied graph, without using the Fetcher or resolving any target.
func (a *Assembler) Assemble(ctx context.Context, m Manifest, source File, opts ...AssembleOption) (*Bundle, error) {
	var aopts assembleOptions
	for _, o := range opts {
		o(&aopts)
	}
	if source.Name == "" {
		return nil, fmt.Errorf("source file is required")
	}
	if aopts.resolvedGraph != nil {
		return assembleResolved(ctx, m, source, *aopts.resolvedGraph, aopts.continueOnError)
	}

	seen := make(map[string]bool)
	depMap := make(map[string][]string)

	sourceParsed, err := parseFile(source)
	if err != nil {
		return nil, err
	}
	queue := enqueueRefs(sourceParsed, seen, depMap)

	if m.BundleVersion == "" {
		m.BundleVersion = sourceParsed.version
	}

	var importParsed []*parsedFile

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		if seen[item.url] {
			continue
		}
		seen[item.url] = true

		data, err := a.fetchAll(ctx, item.url)
		if err != nil {
			return nil, fmt.Errorf("fetching dependency %q from %s: %w", item.refID, item.url, err)
		}

		f := File{
			Name: importFileName(item.refID, item.url),
			Data: data,
		}

		pf, err := parseFile(f)
		if err != nil {
			return nil, fmt.Errorf("parsing transitive refs from %s: %w", f.Name, err)
		}
		importParsed = append(importParsed, pf)
		queue = append(queue, enqueueRefs(pf, seen, depMap)...)
	}

	var imports []File
	for _, pf := range importParsed {
		imports = append(imports, pf.File)
	}

	allParsed := make([]*parsedFile, 0, 1+len(importParsed))
	allParsed = append(allParsed, sourceParsed)
	allParsed = append(allParsed, importParsed...)

	m.Artifacts = buildArtifactTree(sourceParsed, importParsed, depMap)

	warnings := validateMappingRefs(allParsed)
	if len(warnings) > 0 && !aopts.continueOnError {
		return nil, mappingRefError(warnings)
	}

	return &Bundle{
		Manifest: m,
		Source:   source,
		Imports:  imports,
		Warnings: warnings,
	}, nil
}

// fetchRef is a URL-keyed reference waiting to be fetched.
type fetchRef struct {
	refID string
	url   string
}

// enqueueRefs builds fetchRef items for each outbound reference that
// has a resolvable URL and hasn't been seen yet. It also records
// dependency edges in depMap.
func enqueueRefs(pf *parsedFile, seen map[string]bool, depMap map[string][]string) []fetchRef {
	var refs []fetchRef
	for _, refID := range pf.refIDs {
		fetchURL, ok := pf.refURLs[refID]
		if !ok {
			continue
		}

		depName := importFileName(refID, fetchURL)
		depMap[pf.Name] = append(depMap[pf.Name], depName)

		if seen[fetchURL] {
			continue
		}
		refs = append(refs, fetchRef{refID: refID, url: fetchURL})
	}
	return refs
}

// mappingRefURLs builds an id -> url lookup from MappingReferences.
func mappingRefURLs(refs []gemara.MappingReference) map[string]string {
	m := make(map[string]string, len(refs))
	for _, ref := range refs {
		if ref.Url != "" {
			m[ref.Id] = ref.Url
		}
	}
	return m
}

func catalogReferenceSlots(extends []gemara.ArtifactMapping, imports []gemara.MultiEntryMapping) []referenceSlot {
	var slots []referenceSlot
	for _, ext := range extends {
		slots = append(slots, referenceSlot{relationship: "extends", referenceID: ext.ReferenceId})
	}
	for _, imp := range imports {
		slots = append(slots, referenceSlot{relationship: "import", referenceID: imp.ReferenceId})
	}
	return slots
}

func policyReferenceSlots(imports gemara.Imports) []referenceSlot {
	var slots []referenceSlot
	for _, p := range imports.Policies {
		slots = append(slots, referenceSlot{relationship: "policy", referenceID: p.ReferenceId})
	}
	for _, c := range imports.Catalogs {
		slots = append(slots, referenceSlot{relationship: "catalog", referenceID: c.ReferenceId})
	}
	for _, g := range imports.Guidance {
		slots = append(slots, referenceSlot{relationship: "guidance", referenceID: g.ReferenceId})
	}
	return slots
}

// referenceIDs preserves legacy traversal's omission of empty operation IDs.
func referenceIDs(slots []referenceSlot) []string {
	var ids []string
	for _, slot := range slots {
		if slot.referenceID != "" {
			ids = append(ids, slot.referenceID)
		}
	}
	return ids
}

// buildArtifactTree constructs the Manifest.Artifacts slice from the
// parsed source and its dependency relationships.
func buildArtifactTree(source *parsedFile, imports []*parsedFile, depMap map[string][]string) []Artifact {
	artifacts := make([]Artifact, 0, 1+len(imports))
	artifacts = append(artifacts, Artifact{
		Name:         source.Name,
		Type:         source.artType.String(),
		ID:           source.id,
		Role:         roleArtifact,
		Dependencies: depMap[source.Name],
	})
	for _, pf := range imports {
		artifacts = append(artifacts, Artifact{
			Name:         pf.Name,
			Type:         pf.artType.String(),
			ID:           pf.id,
			Role:         roleImport,
			Dependencies: depMap[pf.Name],
		})
	}
	return artifacts
}

func (a *Assembler) fetchAll(ctx context.Context, source string) ([]byte, error) {
	rc, err := a.fetcher.Fetch(ctx, source)
	if err != nil {
		return nil, err
	}
	defer rc.Close() //nolint:errcheck
	return io.ReadAll(rc)
}

// importFileName derives a bundle-relative filename for a resolved import.
func importFileName(refID, rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
			return base
		}
	}
	return refID + ".yaml"
}

// mappingRefError builds a joined error from all unmatched
// mapping-reference IDs found during assembly.
func mappingRefError(warnings []MappingWarning) error {
	errs := make([]error, len(warnings))
	for i, w := range warnings {
		errs[i] = fmt.Errorf("unmatched mapping-reference: %s", w)
	}
	return errors.Join(errs...)
}

// String formats the warning as a human-readable diagnostic message.
func (w MappingWarning) String() string {
	return fmt.Sprintf(
		"%s (artifact %q): mapping-reference %q does not match any artifact metadata.id in the set",
		w.File, w.ArtifactID, w.ReferenceID,
	)
}

// validateMappingRefs checks that every URL-less mapping-reference id in
// each artifact matches at least one metadata.id in the full set.
func validateMappingRefs(parsed []*parsedFile) []MappingWarning {
	knownIDs := make(map[string]bool, len(parsed))
	for _, pf := range parsed {
		if pf.id != "" {
			knownIDs[pf.id] = true
		}
	}

	var warnings []MappingWarning
	for _, pf := range parsed {
		for _, ref := range pf.mappingRefs {
			if ref.Url != "" {
				continue
			}
			if !knownIDs[ref.Id] {
				warnings = append(warnings, MappingWarning{
					File:        pf.Name,
					ArtifactID:  pf.id,
					ReferenceID: ref.Id,
				})
			}
		}
	}
	return warnings
}
