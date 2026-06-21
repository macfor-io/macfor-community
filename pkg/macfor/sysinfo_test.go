package macfor

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSystemInfoJSONMarshal(t *testing.T) {
	bootTime := time.Date(2026, 1, 28, 9, 15, 30, 0, time.UTC)
	collectionTime := time.Date(2026, 1, 29, 14, 30, 0, 0, time.UTC)

	info := &SystemInfo{
		// OS
		OSName:        "macOS",
		OSVersion:     "14.2.1",
		OSBuild:       "23C71",
		OSCodename:    "Sonoma",
		KernelVersion: "23.2.0",

		// Hardware
		HardwareModel:     "Mac14,2",
		HardwareModelName: "MacBook Air (M2, 2022)",
		SerialNumber:      "C02XX123XXXX",
		Architecture:      "arm64",
		CPUType:           "Apple M2",
		CPUCores:          8,
		MemoryGB:          16,

		// Storage
		BootVolumeName:      "Macintosh HD",
		BootVolumeUUID:      "XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX",
		BootVolumeFS:        "apfs",
		BootVolumeEncrypted: true,

		// Security
		SIPEnabled:        true,
		FileVaultEnabled:  true,
		SecureBootLevel:   "full",
		GatekeeperEnabled: true,

		// Network
		Hostname:      "investigators-mac.local",
		LocalHostname: "investigators-mac",
		ComputerName:  "Investigator's Mac",

		// Temporal
		Timezone:       "America/Los_Angeles",
		TimezoneOffset: "-08:00",
		BootTime:       bootTime,
		UptimeSeconds:  105870,
		CollectionTime: collectionTime,

		// Collection
		CollectionUser: "investigator",
		CollectionUID:  501,
		IsRoot:         true,
		SourceType:     SourceTypeLive,
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal SystemInfo: %v", err)
	}

	// Verify key fields are present in JSON.
	jsonStr := string(data)

	expectedFields := []string{
		`"os_name": "macOS"`,
		`"os_version": "14.2.1"`,
		`"os_build": "23C71"`,
		`"os_codename": "Sonoma"`,
		`"kernel_version": "23.2.0"`,
		`"hardware_model": "Mac14,2"`,
		`"hardware_model_name": "MacBook Air (M2, 2022)"`,
		`"serial_number": "C02XX123XXXX"`,
		`"architecture": "arm64"`,
		`"cpu_type": "Apple M2"`,
		`"cpu_cores": 8`,
		`"memory_gb": 16`,
		`"boot_volume_name": "Macintosh HD"`,
		`"boot_volume_fs": "apfs"`,
		`"boot_volume_encrypted": true`,
		`"sip_enabled": true`,
		`"filevault_enabled": true`,
		`"secure_boot_level": "full"`,
		`"gatekeeper_enabled": true`,
		`"hostname": "investigators-mac.local"`,
		`"local_hostname": "investigators-mac"`,
		`"computer_name": "Investigator's Mac"`,
		`"timezone": "America/Los_Angeles"`,
		`"timezone_offset": "-08:00"`,
		`"uptime_seconds": 105870`,
		`"collection_user": "investigator"`,
		`"collection_uid": 501`,
		`"is_root": true`,
		`"source_type": "live"`,
	}

	for _, field := range expectedFields {
		if !contains(jsonStr, field) {
			t.Errorf("expected JSON to contain %s", field)
		}
	}
}

func TestSystemInfoJSONUnmarshal(t *testing.T) {
	jsonData := `{
		"os_name": "macOS",
		"os_version": "14.2.1",
		"os_build": "23C71",
		"os_codename": "Sonoma",
		"kernel_version": "23.2.0",
		"hardware_model": "Mac14,2",
		"hardware_model_name": "MacBook Air (M2, 2022)",
		"architecture": "arm64",
		"cpu_type": "Apple M2",
		"cpu_cores": 8,
		"memory_gb": 16,
		"boot_volume_name": "Macintosh HD",
		"boot_volume_fs": "apfs",
		"boot_volume_encrypted": true,
		"sip_enabled": true,
		"filevault_enabled": true,
		"gatekeeper_enabled": true,
		"hostname": "test-mac.local",
		"timezone": "America/New_York",
		"collection_time": "2026-01-29T14:30:00Z",
		"collection_user": "admin",
		"collection_uid": 501,
		"is_root": false,
		"source_type": "live"
	}`

	var info SystemInfo
	err := json.Unmarshal([]byte(jsonData), &info)
	if err != nil {
		t.Fatalf("failed to unmarshal SystemInfo: %v", err)
	}

	// Verify fields were parsed correctly.
	if info.OSName != "macOS" {
		t.Errorf("expected OSName 'macOS', got '%s'", info.OSName)
	}
	if info.OSVersion != "14.2.1" {
		t.Errorf("expected OSVersion '14.2.1', got '%s'", info.OSVersion)
	}
	if info.HardwareModel != "Mac14,2" {
		t.Errorf("expected HardwareModel 'Mac14,2', got '%s'", info.HardwareModel)
	}
	if info.Architecture != "arm64" {
		t.Errorf("expected Architecture 'arm64', got '%s'", info.Architecture)
	}
	if info.CPUCores != 8 {
		t.Errorf("expected CPUCores 8, got %d", info.CPUCores)
	}
	if info.MemoryGB != 16 {
		t.Errorf("expected MemoryGB 16, got %d", info.MemoryGB)
	}
	if !info.SIPEnabled {
		t.Error("expected SIPEnabled true")
	}
	if !info.FileVaultEnabled {
		t.Error("expected FileVaultEnabled true")
	}
	if !info.BootVolumeEncrypted {
		t.Error("expected BootVolumeEncrypted true")
	}
	if info.SourceType != SourceTypeLive {
		t.Errorf("expected SourceType 'live', got '%s'", info.SourceType)
	}
	if info.CollectionTime.IsZero() {
		t.Error("expected CollectionTime to be parsed")
	}
}

func TestSystemInfoOmitEmpty(t *testing.T) {
	// Create a minimal SystemInfo with only required fields.
	info := &SystemInfo{
		OSName:         "macOS",
		OSVersion:      "14.2.1",
		Architecture:   "arm64",
		Hostname:       "test-mac",
		Timezone:       "UTC",
		CollectionTime: time.Now(),
		CollectionUser: "admin",
		CollectionUID:  501,
		SourceType:     SourceTypeLive,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal SystemInfo: %v", err)
	}

	jsonStr := string(data)

	// Verify omitempty fields are not present when empty.
	omittedFields := []string{
		"serial_number",
		"image_path",
		"image_format",
	}

	for _, field := range omittedFields {
		if contains(jsonStr, `"`+field+`"`) {
			t.Errorf("expected field %s to be omitted when empty", field)
		}
	}

	// Verify required fields are present.
	requiredFields := []string{
		"os_name",
		"os_version",
		"architecture",
		"hostname",
		"source_type",
	}

	for _, field := range requiredFields {
		if !contains(jsonStr, `"`+field+`"`) {
			t.Errorf("expected required field %s to be present", field)
		}
	}
}

func TestSystemInfoDiskImage(t *testing.T) {
	info := &SystemInfo{
		OSName:         "macOS",
		OSVersion:      "13.5.2",
		Architecture:   "arm64",
		Hostname:       "extracted-hostname",
		Timezone:       "UTC",
		CollectionTime: time.Now(),
		CollectionUser: "examiner",
		CollectionUID:  501,
		SourceType:     SourceTypeImage,
		ImagePath:      "/evidence/case001/disk.dmg",
		ImageFormat:    "dmg",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal SystemInfo: %v", err)
	}

	jsonStr := string(data)

	// Note: json.Marshal produces compact output without spaces.
	if !contains(jsonStr, `"source_type":"image"`) {
		t.Error("expected source_type to be 'image'")
	}
	if !contains(jsonStr, `"image_path":"/evidence/case001/disk.dmg"`) {
		t.Error("expected image_path to be present")
	}
	if !contains(jsonStr, `"image_format":"dmg"`) {
		t.Error("expected image_format to be present")
	}
}

func TestGetOSCodename(t *testing.T) {
	tests := []struct {
		version  string
		expected string
	}{
		{"15.0", "Sequoia"},
		{"15.1.2", "Sequoia"},
		{"14.2.1", "Sonoma"},
		{"14.0", "Sonoma"},
		{"13.5", "Ventura"},
		{"12.0.1", "Monterey"},
		{"11.6.5", "Big Sur"},
		{"10.15.7", "Catalina"},
		{"10.14.6", "Mojave"},
		{"10.13.6", "High Sierra"},
		{"10.12.6", "Sierra"},
		{"10.11.6", "El Capitan"},
		{"10.10.5", "Yosemite"},
		{"", ""},
		{"9.0", ""},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := GetOSCodename(tt.version)
			if got != tt.expected {
				t.Errorf("GetOSCodename(%q) = %q, want %q", tt.version, got, tt.expected)
			}
		})
	}
}

func TestGetHardwareModelName(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"Mac14,2", "MacBook Air (M2, 2022)"},
		{"Mac14,5", "MacBook Pro (14-inch, M2 Pro, 2023)"},
		{"MacBookAir10,1", "MacBook Air (M1, 2020)"},
		{"Macmini9,1", "Mac mini (M1, 2020)"},
		{"iMac21,1", "iMac (24-inch, M1, 2021)"},
		{"MacPro7,1", "Mac Pro (2019)"},
		// Unknown model returns the identifier itself.
		{"Unknown99,1", "Unknown99,1"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := GetHardwareModelName(tt.model)
			if got != tt.expected {
				t.Errorf("GetHardwareModelName(%q) = %q, want %q", tt.model, got, tt.expected)
			}
		})
	}
}

func TestExtractMajorVersion(t *testing.T) {
	tests := []struct {
		version  string
		expected string
	}{
		{"14.2.1", "14"},
		{"14.0", "14"},
		{"14", "14"},
		{"10.15.7", "10"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := extractMajorVersion(tt.version)
			if got != tt.expected {
				t.Errorf("extractMajorVersion(%q) = %q, want %q", tt.version, got, tt.expected)
			}
		})
	}
}

func TestExtractMajorMinorVersion(t *testing.T) {
	tests := []struct {
		version  string
		expected string
	}{
		{"14.2.1", "14.2"},
		{"14.0", "14.0"},
		{"14", "14"},
		{"10.15.7", "10.15"},
		{"10.15", "10.15"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := extractMajorMinorVersion(tt.version)
			if got != tt.expected {
				t.Errorf("extractMajorMinorVersion(%q) = %q, want %q", tt.version, got, tt.expected)
			}
		})
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
