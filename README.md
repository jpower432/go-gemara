# go-gemara

[![Go Reference](https://pkg.go.dev/badge/github.com/gemaraproj/go-gemara.svg)](https://pkg.go.dev/github.com/gemaraproj/go-gemara)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go%20version-1.23+-00ADD8.svg)](https://go.dev/)
[![CI](https://github.com/gemaraproj/go-gemara/actions/workflows/ci.yml/badge.svg)](https://github.com/gemaraproj/go-gemara/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/gemaraproj/go-gemara)](https://goreportcard.com/report/github.com/gemaraproj/go-gemara)

Go SDK for parsing and converting Gemara documents.

## Overview

This repository provides Go types and utilities for working with [Gemara](https://gemara.openssf.org/) documents.
The Go types are generated from CUE schemas published in the [Gemara CUE module](https://registry.cue.works/docs/github.com/gemaraproj/gemara@v0) (`github.com/gemaraproj/gemara@v0`) available in the [CUE Central Registry](https://registry.cue.works/).

## Installation

```bash
go get github.com/gemaraproj/go-gemara
```

## Usage

### CLI Tool

The `oscalexport` command-line tool converts Gemara documents to OSCAL format.

#### Building the CLI

```bash
make build
```

This builds binaries to `./bin/` directory.

#### Converting a Control Catalog

```bash
./bin/oscalexport catalog ./path/to/catalog.yaml --output ./catalog.json
```

#### Converting a Guidance Catalog

```bash
./bin/oscalexport guidance ./path/to/guidance.yaml \
    --catalog-output ./guidance.json \
    --profile-output ./profile.json
```

### Library Usage

#### Loading Gemara Documents

```go
package main

import (
    "github.com/gemaraproj/go-gemara"
    "github.com/gemaraproj/go-gemara/fetcher"
)

func main() {
    f := &fetcher.File{}

    // Load a Guidance Catalog
    guidance, err := gemara.Load[gemara.GuidanceCatalog](f, "path/to/guidance.yaml")
    if err != nil {
        panic(err)
    }

    // Load a Control Catalog
    catalog, err := gemara.Load[gemara.ControlCatalog](f, "path/to/catalog.yaml")
    if err != nil {
        panic(err)
    }

    _ = guidance
    _ = catalog
}
```

`Load` and `Decode` are typed convenience readers. They ignore unknown
fields, apply only the schema version linked into this SDK, and do not
provide a lossless view of newer or extended documents.

#### Reading Without Interpretation

Use `ReadDocument` when the original content must be retained, including
unknown fields, enum values, prose, and exact numeric values from any
Gemara v1 schema version.

```go
raw, err := os.ReadFile("path/to/artifact.yaml")
if err != nil {
    panic(err)
}
doc, err := gemara.ReadDocument(raw)
if err != nil {
    panic(err)
}
metadata := doc.Metadata()

// Raw returns the exact input bytes; Node and JSON expose the retained tree.
if !bytes.Equal(doc.Raw(), raw) {
    panic("read altered the source bytes")
}

// Project is a separate, opt-in typed view. It reports fields this SDK does
// not understand instead of silently dropping them.
projection, err := doc.Project()
if err != nil {
    panic(err)
}
if len(projection.Unsupported) > 0 {
    // Interpret these as compatibility findings before acting on the model.
    fmt.Println(projection.Unsupported)
}
```

#### Writing Artifacts

`EncodeArtifactJSON` and `EncodeArtifactYAML` write registered generated
models using the schema version linked into this SDK. They replace
`metadata.gemara-version` with that version, preserve the artifact's own
`metadata.version`, and never mutate the caller's value.

```go
encoded, err := gemara.EncodeArtifactYAML(policy)
if err != nil {
    panic(err)
}
```

Passing a `*Document` makes the encoder refuse any content that does not
project cleanly, rather than writing a lossy conversion. A typed model has
already discarded fields this SDK does not know about, and a successful
encode is not by itself full CUE schema validation.

#### Converting to OSCAL

```go
package main

import (
    "github.com/gemaraproj/go-gemara"
    "github.com/gemaraproj/go-gemara/fetcher"
    "github.com/gemaraproj/go-gemara/gemaraconv"
)

func main() {
    f := &fetcher.File{}

    // Convert Control Catalog to OSCAL
    catalog, err := gemara.Load[gemara.ControlCatalog](f, "path/to/catalog.yaml")
    if err != nil {
        panic(err)
    }

    oscalCatalog, err := gemaraconv.ControlCatalog(catalog).ToOSCAL()
    if err != nil {
        panic(err)
    }

    // Convert Guidance Catalog to OSCAL
    guidance, err := gemara.Load[gemara.GuidanceCatalog](f, "path/to/guidance.yaml")
    if err != nil {
        panic(err)
    }

    _, oscalProfile, err := gemaraconv.GuidanceCatalog(guidance).ToOSCAL("relative/path/to/catalog.json")
    if err != nil {
        panic(err)
    }

    _ = oscalCatalog
    _ = oscalProfile
}
```

#### Bundling and Distributing Artifacts via OCI

```go
package main

import (
	"context"
	"os"

	"github.com/gemaraproj/go-gemara/bundle"
	"github.com/gemaraproj/go-gemara/fetcher"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
)

func main() {
	ctx := context.Background()
	
	data, _ := os.ReadFile("policy.yaml")
	src := bundle.File{Name: "policy.yaml", Data: data}

	// Assemble the full dependency tree (extends + imports)
	m := bundle.Manifest{BundleVersion: "1", GemaraVersion: "v1.0.0"}
	asm := bundle.NewAssembler(&fetcher.URI{})
	b, _ := asm.Assemble(ctx, m, src)

	// Pack into a local OCI layout
	layoutStore, _ := oci.New("./bundle-output")
	desc, _ := bundle.Pack(ctx, layoutStore, b)
	_ = layoutStore.Tag(ctx, desc, "v1.0.0")

	// Push to a remote OCI registry
	repo, _ := remote.NewRepository("registry.example.com/org/bundle")
	tagDesc, _ := layoutStore.Resolve(ctx, "v1.0.0")
	_ = oras.CopyGraph(ctx, layoutStore, repo, tagDesc, oras.DefaultCopyGraphOptions)
	_ = repo.Tag(ctx, tagDesc, "v1.0.0")

	// Unpack from the registry
	unpacked, _ := bundle.Unpack(ctx, repo, "v1.0.0")
	_ = unpacked 
}
```

#### Converting to SARIF

```go
package main

import (
    "github.com/gemaraproj/go-gemara"
    "github.com/gemaraproj/go-gemara/fetcher"
    "github.com/gemaraproj/go-gemara/gemaraconv"
)

func main() {
    f := &fetcher.File{}

    // Load Control Catalog (required for SARIF conversion)
    catalog, err := gemara.Load[gemara.ControlCatalog](f, "path/to/catalog.yaml")
    if err != nil {
        panic(err)
    }

    // Convert EvaluationLog to SARIF
    evaluationLog := gemara.EvaluationLog{
        // ... populate evaluation log ...
    }

    sarifBytes, err := gemaraconv.EvaluationLog(evaluationLog).ToSARIF(gemaraconv.WithArtifactURI("file:///path/to/artifact.md"), gemaraconv.WithCatalog(catalog))
    if err != nil {
        panic(err)
    }

    _ = sarifBytes
}
```

### Fetching Remote Artifacts

The `fetcher.HTTP` and `fetcher.URI` types follow any URL without filtering,
including private network addresses. Applications that process untrusted
URLs should supply an `http.Client` with a safe dialer — see
[`ExampleURI_ssrfSafe`](fetcher/example_test.go) for a complete example
that blocks private, loopback, and link-local addresses at connect time.

## Development

### Building

```bash
make build
```

### Testing

```bash
# Run all tests
make test

# Run tests with coverage
make testcov

# Check coverage threshold
make coverage-check
```

### Linting

```bash
make lint
```

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.
