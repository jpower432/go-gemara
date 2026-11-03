// SPDX-License-Identifier: Apache-2.0

package gemara

import "reflect"

// documentModelTypes is the set of artifacts this version of the library can
// project. The keys are the values of metadata.type. Projection is separate
// from reading: a document whose type is not listed here is still readable
// losslessly through Document, but has no typed projection in this version.
var documentModelTypes = map[string]reflect.Type{
	"AuditLog":          reflect.TypeOf(AuditLog{}),
	"CapabilityCatalog": reflect.TypeOf(CapabilityCatalog{}),
	"ControlCatalog":    reflect.TypeOf(ControlCatalog{}),
	"EnforcementLog":    reflect.TypeOf(EnforcementLog{}),
	"EvaluationLog":     reflect.TypeOf(EvaluationLog{}),
	"GuidanceCatalog":   reflect.TypeOf(GuidanceCatalog{}),
	"Lexicon":           reflect.TypeOf(Lexicon{}),
	"MappingDocument":   reflect.TypeOf(MappingDocument{}),
	"Policy":            reflect.TypeOf(Policy{}),
	"PrincipleCatalog":  reflect.TypeOf(PrincipleCatalog{}),
	"RiskCatalog":       reflect.TypeOf(RiskCatalog{}),
	"ThreatCatalog":     reflect.TypeOf(ThreatCatalog{}),
	"VectorCatalog":     reflect.TypeOf(VectorCatalog{}),
}
