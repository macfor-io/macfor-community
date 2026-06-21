// Package timestamp provides utilities for converting timestamps between
// different formats commonly encountered in macOS forensic artifacts.
//
// Core Data timestamps are commonly used by macOS applications including
// Safari, Photos, Notes, and other system applications. They represent
// the number of seconds since the reference date of 2001-01-01 00:00:00 UTC.
package timestamp

import (
	"time"
)

// CoreDataEpochUnix is the Unix timestamp (seconds since 1970-01-01 00:00:00 UTC)
// corresponding to the Core Data epoch (2001-01-01 00:00:00 UTC).
const CoreDataEpochUnix = 978307200

// CoreDataToTime converts a Core Data timestamp to a Go time.Time value.
//
// Core Data timestamps represent the number of seconds (including fractional
// seconds) since 2001-01-01 00:00:00 UTC. This format is used extensively in
// macOS applications including Safari browsing history, Photos library, and
// other system databases.
//
// A zero timestamp returns the zero time value (time.Time{}). Negative
// timestamps represent dates before 2001-01-01. Fractional seconds are
// preserved with nanosecond precision.
//
// Example:
//
//	// Safari history timestamp
//	timestamp := 725760000.0 // 2024-01-01 00:00:00 UTC
//	t := CoreDataToTime(timestamp)
//	fmt.Println(t) // 2024-01-01 00:00:00 +0000 UTC
func CoreDataToTime(timestamp float64) time.Time {
	if timestamp == 0 {
		return time.Time{}
	}

	// Convert Core Data timestamp to Unix timestamp
	unixTimestamp := timestamp + float64(CoreDataEpochUnix)

	// Split into seconds and fractional part for nanosecond precision
	sec := int64(unixTimestamp)
	nsec := int64((unixTimestamp - float64(sec)) * 1e9)

	return time.Unix(sec, nsec).UTC()
}

// TimeToCoreData converts a Go time.Time value to a Core Data timestamp.
//
// The returned value represents the number of seconds (including fractional
// seconds) since 2001-01-01 00:00:00 UTC. This is the inverse operation of
// CoreDataToTime.
//
// A zero time value (time.Time{}) returns 0.0. The input time is always
// converted to UTC before calculation, so times in any timezone will be
// correctly converted.
//
// Example:
//
//	t := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
//	timestamp := TimeToCoreData(t)
//	fmt.Printf("%.0f\n", timestamp) // 725760000
func TimeToCoreData(t time.Time) float64 {
	if t.IsZero() {
		return 0.0
	}

	// Convert to UTC and get Unix timestamp with nanosecond precision
	utc := t.UTC()
	unixTimestamp := float64(utc.Unix()) + float64(utc.Nanosecond())/1e9

	// Subtract the Core Data epoch to get Core Data timestamp
	return unixTimestamp - float64(CoreDataEpochUnix)
}

// IsCoreDataTimestamp uses heuristics to determine if a timestamp is likely
// in Core Data format rather than Unix timestamp format.
//
// Core Data timestamps for dates between 2001 and ~2026 are typically in the
// range of 0 to ~800,000,000, while Unix timestamps for the same period are
// in the range of ~978,307,200 to ~1,800,000,000.
//
// The heuristic considers:
//   - Zero is ambiguous and returns false (could be either format or null)
//   - Negative values are likely Core Data (represent pre-2001 dates)
//   - Values < 1,000,000,000 are likely Core Data
//   - Values >= 1,000,000,000 are likely Unix timestamps
//
// This is a best-effort heuristic and may not be accurate for all edge cases,
// particularly for timestamps far in the future or in unusual ranges.
//
// Example:
//
//	// Typical Safari history timestamp
//	if IsCoreDataTimestamp(725760000.0) {
//	    t := CoreDataToTime(725760000.0)
//	}
//
//	// Typical Unix timestamp
//	if !IsCoreDataTimestamp(1704067200.0) {
//	    t := time.Unix(1704067200, 0)
//	}
func IsCoreDataTimestamp(timestamp float64) bool {
	// Zero is ambiguous - could be null, Core Data epoch, or Unix epoch
	if timestamp == 0 {
		return false
	}

	// Negative timestamps are likely Core Data (pre-2001 dates)
	// Unix timestamps are always positive
	if timestamp < 0 {
		return true
	}

	// The key discriminator: Core Data timestamps for reasonable dates
	// (2001-2031) are less than ~915 million (2030-01-01), while Unix
	// timestamps for the same period start at ~978 million (2001-01-01)
	//
	// Using 950 million as the threshold provides a safe boundary:
	// - Core Data 950,000,000 = ~2031-02-04 (beyond typical range)
	// - Unix 950,000,000 = 2000-02-08 (before macOS X, unlikely)
	//
	// This threshold cleanly separates the two formats for all practical dates.
	return timestamp < 950_000_000
}
