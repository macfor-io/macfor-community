package sysinfo

import "testing"

// TestGetModelName tests the model name lookup function.
func TestGetModelName(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		// Apple Silicon models.
		{"Mac14,2", "MacBook Air (M2, 2022)"},
		{"Mac15,3", "MacBook Pro (14-inch, M3, 2023)"},
		{"MacBookAir10,1", "MacBook Air (M1, 2020)"},
		{"MacBookPro17,1", "MacBook Pro (13-inch, M1, 2020)"},
		{"Macmini9,1", "Mac mini (M1, 2020)"},
		{"Mac13,1", "Mac Studio (M1 Max, 2022)"},
		{"iMac21,1", "iMac (24-inch, M1, 2021)"},

		// Intel models.
		{"MacBookPro16,1", "MacBook Pro (16-inch, 2019)"},
		{"MacBookAir9,1", "MacBook Air (Retina, 13-inch, 2020)"},
		{"iMac20,1", "iMac (Retina 5K, 27-inch, 2020)"},
		{"Macmini8,1", "Mac mini (2018)"},
		{"MacPro7,1", "Mac Pro (2019)"},

		// Unknown model returns identifier.
		{"Unknown,1", "Unknown,1"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := GetModelName(tt.model)
			if got != tt.want {
				t.Errorf("GetModelName(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

// TestGetModelGeneration tests the chip generation detection.
func TestGetModelGeneration(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		// M4 models.
		{"Mac16,1", "M4"},
		{"Mac16,5", "M4"},
		{"Mac16,12", "M4"},

		// M3 models.
		{"Mac15,3", "M3"},
		{"Mac15,12", "M3"},
		{"Mac15,4", "M3"},

		// M2 models.
		{"Mac14,2", "M2"},
		{"Mac14,7", "M2"},
		{"Mac14,3", "M2"},

		// M1 Ultra.
		{"Mac13,1", "M1 Ultra"},
		{"Mac13,2", "M1 Ultra"},

		// M1 models.
		{"MacBookAir10,1", "M1"},
		{"MacBookPro17,1", "M1"},
		{"MacBookPro18,1", "M1"},
		{"MacBookPro18,4", "M1"},
		{"Macmini9,1", "M1"},
		{"iMac21,1", "M1"},
		{"iMac21,2", "M1"},

		// Intel models.
		{"MacBookPro16,1", "Intel"},
		{"MacBookAir9,1", "Intel"},
		{"iMac20,1", "Intel"},
		{"Macmini8,1", "Intel"},
		{"MacPro7,1", "Intel"},

		// Unknown models.
		{"Unknown,1", "Intel"},
		{"", "Intel"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := GetModelGeneration(tt.model)
			if got != tt.want {
				t.Errorf("GetModelGeneration(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

// TestIsAppleSilicon tests Apple Silicon detection.
func TestIsAppleSilicon(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		// Apple Silicon.
		{"Mac16,1", true},
		{"Mac15,3", true},
		{"Mac14,2", true},
		{"Mac13,1", true},
		{"MacBookAir10,1", true},
		{"MacBookPro17,1", true},
		{"MacBookPro18,1", true},
		{"Macmini9,1", true},
		{"iMac21,1", true},

		// Intel.
		{"MacBookPro16,1", false},
		{"MacBookAir9,1", false},
		{"iMac20,1", false},
		{"Macmini8,1", false},
		{"MacPro7,1", false},

		// Unknown.
		{"Unknown,1", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := IsAppleSilicon(tt.model)
			if got != tt.want {
				t.Errorf("IsAppleSilicon(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

// TestHardwareModelsCoverage verifies the hardware models map has expected entries.
func TestHardwareModelsCoverage(t *testing.T) {
	// Verify we have a reasonable number of models.
	if len(HardwareModels) < 50 {
		t.Errorf("HardwareModels has only %d entries, expected at least 50", len(HardwareModels))
	}

	// Check that all model names are non-empty.
	for id, name := range HardwareModels {
		if name == "" {
			t.Errorf("HardwareModels[%q] has empty name", id)
		}
	}

	// Spot check some expected models.
	expectedModels := []string{
		"Mac14,2",        // M2 MacBook Air.
		"MacBookAir10,1", // M1 MacBook Air.
		"MacBookPro16,1", // Intel MacBook Pro.
		"iMac21,1",       // M1 iMac.
		"Macmini9,1",     // M1 Mac mini.
	}

	for _, id := range expectedModels {
		if _, ok := HardwareModels[id]; !ok {
			t.Errorf("HardwareModels missing expected model %q", id)
		}
	}
}
