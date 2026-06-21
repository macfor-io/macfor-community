package timestamp

import (
	"math"
	"testing"
	"time"
)

func TestCoreDataToTime(t *testing.T) {
	tests := []struct {
		name      string
		timestamp float64
		want      time.Time
	}{
		{
			name:      "zero timestamp",
			timestamp: 0.0,
			want:      time.Time{},
		},
		{
			name:      "Core Data epoch (2001-01-01 00:00:00 UTC)",
			timestamp: 0.0,
			want:      time.Time{},
		},
		{
			name:      "2024-01-01 00:00:00 UTC",
			timestamp: 725760000.0,
			want:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "2024-06-15 12:30:45 UTC",
			timestamp: 740147445.0,
			want:      time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC),
		},
		{
			name:      "fractional seconds (nanosecond precision)",
			timestamp: 725760000.123456789,
			want:      time.Date(2024, 1, 1, 0, 0, 0, 123456789, time.UTC),
		},
		{
			name:      "negative timestamp (pre-2001)",
			timestamp: -86400.0, // One day before epoch
			want:      time.Date(2000, 12, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "far future timestamp (2050-01-01)",
			timestamp: 1546300800.0,
			want:      time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "Safari typical timestamp (2023-03-15)",
			timestamp: 700531200.0,
			want:      time.Date(2023, 3, 15, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CoreDataToTime(tt.timestamp)

			// Special handling for zero time
			if tt.timestamp == 0.0 {
				if !got.IsZero() {
					t.Errorf("CoreDataToTime(%f) = %v, want zero time", tt.timestamp, got)
				}
				return
			}

			// For fractional seconds, allow small floating-point precision loss
			diff := got.Sub(tt.want)
			if diff < 0 {
				diff = -diff
			}
			maxAcceptableDiff := 200 * time.Nanosecond

			if diff > maxAcceptableDiff {
				t.Errorf("CoreDataToTime(%f) = %v, want %v (diff: %v)", tt.timestamp, got, tt.want, diff)
			}

			// Verify timezone is UTC
			if got.Location() != time.UTC {
				t.Errorf("CoreDataToTime(%f) timezone = %v, want UTC", tt.timestamp, got.Location())
			}
		})
	}
}

func TestTimeToCoreData(t *testing.T) {
	tests := []struct {
		name string
		time time.Time
		want float64
	}{
		{
			name: "zero time",
			time: time.Time{},
			want: 0.0,
		},
		{
			name: "Core Data epoch (2001-01-01 00:00:00 UTC)",
			time: time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 0.0,
		},
		{
			name: "2024-01-01 00:00:00 UTC",
			time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 725760000.0,
		},
		{
			name: "2024-06-15 12:30:45 UTC",
			time: time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC),
			want: 740147445.0,
		},
		{
			name: "fractional seconds (nanosecond precision)",
			time: time.Date(2024, 1, 1, 0, 0, 0, 123456789, time.UTC),
			want: 725760000.123456789,
		},
		{
			name: "pre-2001 timestamp (2000-12-31)",
			time: time.Date(2000, 12, 31, 0, 0, 0, 0, time.UTC),
			want: -86400.0,
		},
		{
			name: "far future (2050-01-01)",
			time: time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 1546300800.0,
		},
		{
			name: "non-UTC timezone (should convert)",
			time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.FixedZone("PST", -8*3600)),
			want: 725788800.0, // 8 hours later in UTC
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TimeToCoreData(tt.time)

			// Special handling for zero time
			if tt.time.IsZero() {
				if got != 0.0 {
					t.Errorf("TimeToCoreData(zero time) = %f, want 0.0", got)
				}
				return
			}

			// Use a small epsilon for floating-point comparison
			epsilon := 0.000001
			if math.Abs(got-tt.want) > epsilon {
				t.Errorf("TimeToCoreData(%v) = %f, want %f", tt.time, got, tt.want)
			}
		})
	}
}

func TestRoundTripConversion(t *testing.T) {
	tests := []struct {
		name      string
		timestamp float64
	}{
		{"zero", 0.0},
		{"2024-01-01", 725760000.0},
		{"with fractional seconds", 725760000.123456789},
		{"negative (pre-2001)", -86400.0},
		{"far future", 1546300800.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Core Data -> Time -> Core Data
			converted := CoreDataToTime(tt.timestamp)
			roundTrip := TimeToCoreData(converted)

			// Special handling for zero
			if tt.timestamp == 0.0 {
				if roundTrip != 0.0 {
					t.Errorf("round trip for zero: got %f, want 0.0", roundTrip)
				}
				return
			}

			epsilon := 0.000001
			if math.Abs(roundTrip-tt.timestamp) > epsilon {
				t.Errorf("round trip: %f -> %v -> %f (diff: %e)",
					tt.timestamp, converted, roundTrip, math.Abs(roundTrip-tt.timestamp))
			}
		})
	}
}

func TestIsCoreDataTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		timestamp float64
		want      bool
	}{
		{
			name:      "zero",
			timestamp: 0.0,
			want:      false, // Ambiguous - could be either format
		},
		{
			name:      "typical Core Data (2024-01-01)",
			timestamp: 725760000.0,
			want:      true,
		},
		{
			name:      "Core Data near upper bound (2026)",
			timestamp: 788918400.0,
			want:      true,
		},
		{
			name:      "typical Unix timestamp (2024-01-01)",
			timestamp: 1704067200.0,
			want:      false,
		},
		{
			name:      "Unix timestamp for 2001",
			timestamp: 978307200.0,
			want:      false,
		},
		{
			name:      "negative Core Data (pre-2001)",
			timestamp: -86400.0,
			want:      true,
		},
		{
			name:      "very small positive (likely Core Data)",
			timestamp: 100.0,
			want:      true,
		},
		{
			name:      "boundary case - 1 billion",
			timestamp: 1000000000.0,
			want:      false,
		},
		{
			name:      "just below 1 billion (year 2032 in Core Data, unlikely)",
			timestamp: 999999999.0,
			want:      false, // Beyond practical Core Data range, more likely Unix
		},
		{
			name:      "far future Core Data (2050)",
			timestamp: 1546300800.0,
			want:      false, // Beyond reasonable Core Data range
		},
		{
			name:      "very large (clearly Unix)",
			timestamp: 1800000000.0,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCoreDataTimestamp(tt.timestamp)
			if got != tt.want {
				t.Errorf("IsCoreDataTimestamp(%f) = %v, want %v", tt.timestamp, got, tt.want)
			}
		})
	}
}

func TestCoreDataEpochUnix(t *testing.T) {
	// Verify the constant is correct
	// 2001-01-01 00:00:00 UTC in Unix time should be 978307200
	epoch := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	expectedUnix := epoch.Unix()

	if CoreDataEpochUnix != expectedUnix {
		t.Errorf("CoreDataEpochUnix = %d, want %d", CoreDataEpochUnix, expectedUnix)
	}

	if CoreDataEpochUnix != 978307200 {
		t.Errorf("CoreDataEpochUnix = %d, want 978307200", CoreDataEpochUnix)
	}
}

func TestNanosecondPrecision(t *testing.T) {
	// Test that we preserve nanosecond precision within float64 limitations
	// Note: float64 has ~15-17 decimal digits of precision, so we can't
	// guarantee exact nanosecond precision for large timestamps, but we
	// should get very close (within ~100 nanoseconds)
	originalTime := time.Date(2024, 1, 1, 12, 30, 45, 123456789, time.UTC)

	// Convert to Core Data and back
	coreData := TimeToCoreData(originalTime)
	recovered := CoreDataToTime(coreData)

	// Calculate difference in nanoseconds
	diff := recovered.Sub(originalTime)
	maxAcceptableDiff := 200 * time.Nanosecond // Allow up to 200ns difference

	if diff < 0 {
		diff = -diff
	}

	if diff > maxAcceptableDiff {
		t.Errorf("precision loss too large: original=%v, recovered=%v, diff=%v",
			originalTime, recovered, diff)
	}
}

func TestEdgeCases(t *testing.T) {
	t.Run("max float64", func(t *testing.T) {
		// Should not panic
		result := CoreDataToTime(math.MaxFloat64)
		if result.IsZero() {
			t.Error("expected non-zero time for large timestamp")
		}
	})

	t.Run("negative infinity", func(_ *testing.T) {
		// Should not panic - just verify it doesn't crash
		_ = CoreDataToTime(math.Inf(-1))
	})

	t.Run("positive infinity", func(_ *testing.T) {
		// Should not panic - just verify it doesn't crash
		_ = CoreDataToTime(math.Inf(1))
	})

	t.Run("NaN", func(_ *testing.T) {
		// Should not panic - just verify it doesn't crash
		_ = CoreDataToTime(math.NaN())
	})
}
