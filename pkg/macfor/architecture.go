package macfor

import (
	"regexp"

	"github.com/macfor-io/macfor-community/internal/sysinfo"
)

// CPU architecture names reported by HardwareArchitecture. They use Go's
// GOARCH names, matching what the live source reports via runtime.GOARCH.
const (
	// ArchARM64 is the architecture of Apple silicon Macs.
	ArchARM64 = "arm64"
	// ArchAMD64 is the architecture of Intel Macs. It uses Go's name, as the
	// live source reports runtime.GOARCH, so every source type agrees.
	ArchAMD64 = "amd64"
)

// appleSiliconChip matches an Apple silicon chip token in a marketing model
// name, such as "M1" in "MacBook Air (M1, 2020)" or "M4" in
// "Mac mini (M4 Pro, 2024)". Intel model names never contain such a token
// ("Mid 2012" does not match because the token must be M followed by digits).
var appleSiliconChip = regexp.MustCompile(`\bM[1-9][0-9]*\b`)

// LookupHardwareModelName returns the marketing name for a hardware model
// identifier and whether the identifier is present in the hardware model
// database. Both the public HardwareModels subset and the more comprehensive
// internal database are consulted, the public subset first.
func LookupHardwareModelName(model string) (string, bool) {
	if model == "" {
		return "", false
	}
	if name, ok := HardwareModels[model]; ok {
		return name, true
	}
	if name, ok := sysinfo.HardwareModels[model]; ok {
		return name, true
	}
	return "", false
}

// HardwareArchitecture derives the CPU architecture of a Mac from its hardware
// model identifier (e.g., "Mac14,2"). It returns ArchARM64 when the model's
// marketing name names an Apple silicon chip (M1, M2 Pro, M4 Max, ...),
// ArchAMD64 for any other model present in the hardware model database, and
// an empty string when the model is empty or unknown. Unknown models are never
// guessed, since a future identifier could be either architecture.
func HardwareArchitecture(model string) string {
	name, ok := LookupHardwareModelName(model)
	if !ok {
		return ""
	}
	if appleSiliconChip.MatchString(name) {
		return ArchARM64
	}
	return ArchAMD64
}
