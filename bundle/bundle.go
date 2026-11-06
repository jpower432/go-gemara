// SPDX-License-Identifier: Apache-2.0

// Package bundle defines the Gemara OCI artifact format and provides
// Assemble, Pack, and Unpack operations for building, storing, and
// retrieving Gemara artifacts in OCI-compliant registries.
package bundle

import (
	"bytes"
	"slices"
)

const (
	// DefaultSizeLimitBytes caps bundle reads at 256 MB.
	DefaultSizeLimitBytes int64 = 256 * 1024 * 1024
)

// OCI media types for the Gemara bundle format.
// All artifact layers are YAML; the assembler and resolver expect YAML input.
const (
	// MediaTypeBundle is the OCI artifactType written into the image manifest.
	MediaTypeBundle = "application/vnd.gemara.bundle.v1"

	// MediaTypeManifest is the media type for the bundle manifest blob.
	MediaTypeManifest = "application/vnd.gemara.manifest.v1+json"

	// MediaTypeArtifact is the media type for individual Gemara YAML layers.
	MediaTypeArtifact = "application/vnd.gemara.artifact.v1+yaml"
)

// Internal annotations carried on layer descriptors.
const (
	annotationRole = "org.gemara.artifact.role"
	annotationType = "org.gemara.artifact.type"
	roleArtifact   = "artifact"
	roleImport     = "import"
)

// Manifest defines the OCI config blob stored in the bundle.
type Manifest struct {
	BundleVersion string         `json:"bundle-version"`
	GemaraVersion string         `json:"gemara-version"`
	Revision      string         `json:"revision,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Artifacts     []Artifact     `json:"artifacts,omitempty"`
}

// Empty reports whether the manifest carries no meaningful data.
func (m Manifest) Empty() bool {
	return m.BundleVersion == "" && m.GemaraVersion == "" &&
		m.Revision == "" && len(m.Metadata) == 0 &&
		len(m.Artifacts) == 0
}

// Artifact describes a single file in the bundle and its position
// in the dependency tree, similar to rootfs in an OCI image config.
type Artifact struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	ID           string   `json:"id"`
	Role         string   `json:"role"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// File is a single Gemara YAML artifact within the bundle.
type File struct {
	// Name is the bundle-relative path, e.g. "controls.yaml".
	Name string
	// Type is the Gemara artifact type, e.g. "ControlCatalog".
	Type string
	// Data is the raw YAML content of the artifact.
	Data []byte
}

// Bundle is an in-memory representation of a Gemara OCI artifact.
type Bundle struct {
	// Manifest is the OCI config blob describing the bundle contents.
	Manifest Manifest
	// Source is the primary artifact file that anchors this bundle.
	Source File
	// Imports are the resolved transitive dependency files.
	Imports []File
	// Warnings holds non-fatal issues found during assembly,
	// such as mapping-reference IDs that don't match any artifact.
	Warnings []MappingWarning
	// Etag is the OCI manifest digest used for cache comparison.
	Etag string

	sizeLimitBytes int64
}

// MappingWarning describes a mapping-reference whose id does not match
// any artifact metadata.id in the provided set.
type MappingWarning struct {
	File        string
	ArtifactID  string
	ReferenceID string
}

// SizeLimitBytes returns the configured size limit.
// If unset, DefaultSizeLimitBytes is returned.
func (b *Bundle) SizeLimitBytes() int64 {
	if b.sizeLimitBytes > 0 {
		return b.sizeLimitBytes
	}
	return DefaultSizeLimitBytes
}

// SetSizeLimitBytes configures the maximum allowed bundle size.
func (b *Bundle) SetSizeLimitBytes(n int64) {
	b.sizeLimitBytes = n
}

// Version returns the bundle version from the manifest.
func (b *Bundle) Version() string {
	return b.Manifest.BundleVersion
}

// AssembleOption configures Assemble behaviour.
type AssembleOption func(*assembleOptions)

type assembleOptions struct {
	continueOnError bool
	resolvedGraph   *ResolvedGraph
}

// ResolvedGraph supplies a complete, already-selected dependency graph to
// Assemble. Node keys identify acquisition contexts, not artifact IDs or URLs.
// The caller owns binding authorization and target identity/type/version checks.
// Assemble validates graph completeness and projects it without acquisition.
type ResolvedGraph struct {
	Root  string
	Nodes []ResolvedNode
	Edges []ResolvedEdge
}

// ResolvedNode contains the original authored bytes at one acquisition context.
// Distinct keys may contain identical bytes; compatible copies become one file.
type ResolvedNode struct {
	Key  string
	Data []byte
}

// ResolvedEdge binds one authored required operation to an already-selected node.
// Ordinal is zero-based across all operations of From: Policy imports are ordered
// policies, catalogs, guidance; catalog operations are ordered extends, imports.
// Relationship is respectively policy, catalog, guidance, extends, or import.
// ReferenceID and DeclaredURL must exactly match the authored declaration, even
// when ReferenceID is an alias for To's metadata.id or DeclaredURL is empty.
type ResolvedEdge struct {
	From, To     string
	Relationship string
	Ordinal      int
	ReferenceID  string
	DeclaredURL  string
}

// WithResolvedGraph snapshots a complete graph for explicit-graph assembly.
// Root must identify a node whose bytes equal the source passed to Assemble.
// No Fetcher is used, including on invalid graphs; NewAssembler(nil) is valid
// with this option. An empty graph is an error, not a request for URL traversal.
// Imports receive deterministic content-addressed names; source.Name is retained.
// Pack and Unpack use the existing format: artifact inventory and adjacency, not
// the original per-context binding/provenance records. Without this option the
// legacy URL-based assembly behavior and naming remain unchanged.
func WithResolvedGraph(g ResolvedGraph) AssembleOption {
	snapshot := cloneResolvedGraph(g)
	return func(o *assembleOptions) {
		copy := cloneResolvedGraph(snapshot)
		o.resolvedGraph = &copy
	}
}

func cloneResolvedGraph(g ResolvedGraph) ResolvedGraph {
	g.Nodes = slices.Clone(g.Nodes)
	for i := range g.Nodes {
		g.Nodes[i].Data = bytes.Clone(g.Nodes[i].Data)
	}
	g.Edges = slices.Clone(g.Edges)
	return g
}

// WithContinueOnError makes Assemble populate Bundle.Warnings instead of
// returning an error when mapping-reference IDs don't match any artifact
// metadata.id in the assembled set.
func WithContinueOnError() AssembleOption {
	return func(o *assembleOptions) {
		o.continueOnError = true
	}
}

// PackOption configures Pack behaviour.
type PackOption func(*packOptions)

type packOptions struct {
	annotations map[string]string
	version     *string
}

// WithAnnotations adds custom annotations to the OCI manifest.
func WithAnnotations(annotations map[string]string) PackOption {
	return func(o *packOptions) {
		o.annotations = annotations
	}
}

// WithVersion overrides the bundle's Manifest.BundleVersion at pack time.
// When not set, the version assembled from the main artifact metadata is used.
func WithVersion(v string) PackOption {
	return func(o *packOptions) {
		o.version = &v
	}
}
