// SPDX-License-Identifier: Apache-2.0

package gemara

import (
	"fmt"
	"regexp"
)

// The SDK reads every SemVer 2.0.0 document in the Gemara v1 major line,
// including versions produced after this SDK's schema version. An optional
// leading v is accepted for compatibility with published artifact spellings.
var gemaraVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

const gemaraVersionPath = "/metadata/gemara-version"

func validateGemaraVersion(version string) *DocumentError {
	match := gemaraVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return documentError("malformed_version", gemaraVersionPath,
			fmt.Sprintf("%q is not a SemVer 2.0.0 version", version))
	}
	if major := match[1]; major != "1" {
		return documentError("unsupported_major", gemaraVersionPath,
			fmt.Sprintf("Gemara major version %s is not supported (readable major: 1)", major))
	}
	return nil
}
