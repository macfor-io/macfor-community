package artifacts

import (
	"strconv"
	"strings"
)

// CompareVersions compares two version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
//
// Version strings are expected to be in dotted format (e.g., "10.15.7", "14.2.1").
// Non-numeric suffixes like "-beta" are stripped before comparison.
func CompareVersions(a, b string) int {
	partsA := parseVersion(a)
	partsB := parseVersion(b)

	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			numA = partsA[i]
		}
		if i < len(partsB) {
			numB = partsB[i]
		}

		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
	}

	return 0
}

// parseVersion splits a version string into numeric components.
func parseVersion(v string) []int {
	if v == "" {
		return nil
	}

	parts := strings.Split(v, ".")
	result := make([]int, len(parts))

	for i, part := range parts {
		// Handle non-numeric suffixes like "14.2.1-beta".
		numPart := strings.Split(part, "-")[0]
		num, err := strconv.Atoi(numPart)
		if err != nil {
			// If parsing fails, treat as 0.
			num = 0
		}
		result[i] = num
	}

	return result
}

// VersionAtLeast checks if version is >= minimum.
func VersionAtLeast(version, minimum string) bool {
	return CompareVersions(version, minimum) >= 0
}

// VersionAtMost checks if version is <= maximum.
func VersionAtMost(version, maximum string) bool {
	return CompareVersions(version, maximum) <= 0
}

// VersionInRange checks if version is within [minVer, maxVer].
// Empty string for minVer/maxVer means unbounded.
func VersionInRange(version, minVer, maxVer string) bool {
	if minVer != "" && CompareVersions(version, minVer) < 0 {
		return false
	}
	if maxVer != "" && CompareVersions(version, maxVer) > 0 {
		return false
	}
	return true
}
