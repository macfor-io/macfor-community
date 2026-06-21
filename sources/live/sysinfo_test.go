package live

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// Note: darwinOS constant is defined in integration_test.go.

// archARM64 is the GOARCH value for Apple Silicon.
const archARM64 = "arm64"

// TestApplySystemVersion tests the applySystemVersion function with sample data.
// This tests the OS version parsing logic independent of file reading.
func TestApplySystemVersion(t *testing.T) {
	tests := []struct {
		name            string
		sv              systemVersionPlist
		wantVersion     string
		wantBuild       string
		wantCodename    string
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "macOS Sonoma 14.2.1",
			sv: systemVersionPlist{
				ProductName:         "macOS",
				ProductVersion:      "14.2.1",
				ProductBuildVersion: "23C71",
			},
			wantVersion:  "14.2.1",
			wantBuild:    "23C71",
			wantCodename: "Sonoma",
		},
		{
			name: "macOS Ventura 13.6",
			sv: systemVersionPlist{
				ProductName:         "macOS",
				ProductVersion:      "13.6",
				ProductBuildVersion: "22G120",
			},
			wantVersion:  "13.6",
			wantBuild:    "22G120",
			wantCodename: "Ventura",
		},
		{
			name: "macOS Sequoia 15.0",
			sv: systemVersionPlist{
				ProductName:         "macOS",
				ProductVersion:      "15.0",
				ProductBuildVersion: "24A335",
			},
			wantVersion:  "15.0",
			wantBuild:    "24A335",
			wantCodename: "Sequoia",
		},
		{
			name: "macOS Catalina 10.15.7",
			sv: systemVersionPlist{
				ProductName:         "Mac OS X",
				ProductVersion:      "10.15.7",
				ProductBuildVersion: "19H2",
			},
			wantVersion:  "10.15.7",
			wantBuild:    "19H2",
			wantCodename: "Catalina",
		},
		{
			name: "macOS Mojave 10.14.6",
			sv: systemVersionPlist{
				ProductName:         "Mac OS X",
				ProductVersion:      "10.14.6",
				ProductBuildVersion: "18G103",
			},
			wantVersion:  "10.14.6",
			wantBuild:    "18G103",
			wantCodename: "Mojave",
		},
		{
			name: "missing ProductVersion",
			sv: systemVersionPlist{
				ProductName:         "macOS",
				ProductBuildVersion: "23C71",
			},
			wantErr:         true,
			wantErrContains: "ProductVersion not found",
		},
		{
			name: "unknown version (no codename)",
			sv: systemVersionPlist{
				ProductName:         "macOS",
				ProductVersion:      "99.0",
				ProductBuildVersion: "99A999",
			},
			wantVersion:  "99.0",
			wantBuild:    "99A999",
			wantCodename: "", // Unknown version.
		},
		{
			name: "empty build version",
			sv: systemVersionPlist{
				ProductName:    "macOS",
				ProductVersion: "14.0",
			},
			wantVersion:  "14.0",
			wantBuild:    "", // Empty build is allowed.
			wantCodename: "Sonoma",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &macfor.SystemInfo{}
			err := applySystemVersion(info, tt.sv)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if info.OSVersion != tt.wantVersion {
				t.Errorf("OSVersion = %q, want %q", info.OSVersion, tt.wantVersion)
			}
			if info.OSBuild != tt.wantBuild {
				t.Errorf("OSBuild = %q, want %q", info.OSBuild, tt.wantBuild)
			}
			if info.OSCodename != tt.wantCodename {
				t.Errorf("OSCodename = %q, want %q", info.OSCodename, tt.wantCodename)
			}
		})
	}
}

// TestDeriveAppleSiliconCPUType tests CPU type derivation from model identifiers.
func TestDeriveAppleSiliconCPUType(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		// M4 models.
		{"Mac16,1", "Apple M4"},
		{"Mac16,5", "Apple M4"},

		// M3 models.
		{"Mac15,3", "Apple M3"},
		{"Mac15,12", "Apple M3"},

		// M2 models.
		{"Mac14,2", "Apple M2"},
		{"Mac14,7", "Apple M2"},

		// M1 Ultra (Mac Studio).
		{"Mac13,1", "Apple M1 Ultra"},
		{"Mac13,2", "Apple M1 Ultra"},

		// M1 models.
		{"MacBookAir10,1", "Apple M1"},
		{"MacBookPro17,1", "Apple M1"},
		{"MacBookPro18,1", "Apple M1"},
		{"Macmini9,1", "Apple M1"},
		{"iMac21,1", "Apple M1"},

		// Intel models (should return empty).
		{"MacBookPro16,1", ""},
		{"iMac20,1", ""},
		{"Macmini8,1", ""},

		// Unknown model (should return empty).
		{"Unknown,1", ""},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := deriveAppleSiliconCPUType(tt.model)
			if got != tt.want {
				t.Errorf("deriveAppleSiliconCPUType(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

// TestSysctlFunctions tests the sysctl helper functions on a live system.
func TestSysctlFunctions(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping sysctl test on non-macOS")
	}

	// Test sysctlString with a known sysctl.
	model, err := sysctlString("hw.model")
	if err != nil {
		t.Fatalf("sysctlString(hw.model) error: %v", err)
	}
	if model == "" {
		t.Error("sysctlString(hw.model) returned empty string")
	}
	t.Logf("hw.model: %s", model)

	// Test sysctlInt.
	cores, err := sysctlInt("hw.physicalcpu")
	if err != nil {
		t.Fatalf("sysctlInt(hw.physicalcpu) error: %v", err)
	}
	if cores <= 0 {
		t.Errorf("sysctlInt(hw.physicalcpu) = %d, want > 0", cores)
	}
	t.Logf("hw.physicalcpu: %d", cores)

	// Test sysctlInt64.
	mem, err := sysctlInt64("hw.memsize")
	if err != nil {
		t.Fatalf("sysctlInt64(hw.memsize) error: %v", err)
	}
	if mem <= 0 {
		t.Errorf("sysctlInt64(hw.memsize) = %d, want > 0", mem)
	}
	t.Logf("hw.memsize: %d bytes (%.1f GB)", mem, float64(mem)/(1024*1024*1024))
}

// TestGetSerialNumber tests serial number retrieval on a live system.
func TestGetSerialNumber(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping serial number test on non-macOS")
	}

	serial, err := getSerialNumber()
	// Serial number retrieval may fail without privileges, which is OK.
	if err != nil {
		t.Logf("getSerialNumber() returned error (may be expected): %v", err)
		return
	}

	if serial == "" {
		t.Error("getSerialNumber() returned empty string")
	}

	// Serial numbers are typically 10-12 characters.
	if len(serial) < 8 || len(serial) > 14 {
		t.Logf("warning: serial number %q has unexpected length %d", serial, len(serial))
	}

	t.Logf("Serial: %s", serial)
}

// TestCollectHardwareInfo tests hardware info collection on a live system.
func TestCollectHardwareInfo(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping hardware info test on non-macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	info := &macfor.SystemInfo{}
	src.collectHardwareInfo(info)

	// Hardware model should always be available.
	if info.HardwareModel == "" {
		t.Error("HardwareModel is empty")
	} else {
		t.Logf("HardwareModel: %s", info.HardwareModel)
	}

	// HardwareModelName should be set (either from database or model ID fallback).
	if info.HardwareModelName == "" {
		t.Error("HardwareModelName is empty")
	} else {
		t.Logf("HardwareModelName: %s", info.HardwareModelName)
	}

	// CPUType should be set on most systems.
	if info.CPUType != "" {
		t.Logf("CPUType: %s", info.CPUType)
	}

	// CPUCores should be > 0.
	if info.CPUCores <= 0 {
		t.Errorf("CPUCores = %d, want > 0", info.CPUCores)
	} else {
		t.Logf("CPUCores: %d", info.CPUCores)
	}

	// MemoryGB should be > 0.
	if info.MemoryGB <= 0 {
		t.Errorf("MemoryGB = %d, want > 0", info.MemoryGB)
	} else {
		t.Logf("MemoryGB: %d", info.MemoryGB)
	}

	// Serial number may or may not be available.
	if info.SerialNumber != "" {
		t.Logf("SerialNumber: %s", info.SerialNumber)
	} else {
		t.Log("SerialNumber: (not available)")
	}
}

// TestParseSIPStatus tests parsing of csrutil status output.
func TestParseSIPStatus(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "enabled simple",
			output: "System Integrity Protection status: enabled.",
			want:   true,
		},
		{
			name:   "disabled simple",
			output: "System Integrity Protection status: disabled.",
			want:   false,
		},
		{
			name:   "enabled with custom config",
			output: "System Integrity Protection status: enabled (Custom Configuration).",
			want:   true,
		},
		{
			name:   "enabled multiline",
			output: "System Integrity Protection status: enabled.\n\nConfiguration:\n\tApple Internal: disabled",
			want:   true,
		},
		{
			name:   "disabled with details",
			output: "System Integrity Protection status: disabled.\n\nConfiguration:\n\tApple Internal: disabled\n\tKext Signing: disabled",
			want:   false,
		},
		{
			name:   "mixed case enabled",
			output: "System Integrity Protection Status: ENABLED.",
			want:   true,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSIPStatus(tt.output)
			if got != tt.want {
				t.Errorf("parseSIPStatus(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

// TestParseFileVaultStatus tests parsing of fdesetup status output.
func TestParseFileVaultStatus(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "on simple",
			output: "FileVault is On.",
			want:   true,
		},
		{
			name:   "off simple",
			output: "FileVault is Off.",
			want:   false,
		},
		{
			name:   "on decrypting",
			output: "FileVault is On, but is currently Decrypting.",
			want:   true,
		},
		{
			name:   "on encrypting",
			output: "FileVault is On, but is currently Encrypting.",
			want:   true,
		},
		{
			name:   "mixed case on",
			output: "FILEVAULT IS ON.",
			want:   true,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFileVaultStatus(tt.output)
			if got != tt.want {
				t.Errorf("parseFileVaultStatus(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

// TestParseGatekeeperStatus tests parsing of spctl --status output.
func TestParseGatekeeperStatus(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "enabled simple",
			output: "assessments enabled",
			want:   true,
		},
		{
			name:   "disabled simple",
			output: "assessments disabled",
			want:   false,
		},
		{
			name:   "mixed case enabled",
			output: "ASSESSMENTS ENABLED",
			want:   true,
		},
		{
			name:   "with newline",
			output: "assessments enabled\n",
			want:   true,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGatekeeperStatus(tt.output)
			if got != tt.want {
				t.Errorf("parseGatekeeperStatus(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

// TestParseSecureBootLevel tests parsing of bputil -d output.
func TestParseSecureBootLevel(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "full security",
			output: "Current security mode is Full Security.",
			want:   "full",
		},
		{
			name:   "reduced security",
			output: "Current security mode is Reduced Security.",
			want:   "reduced",
		},
		{
			name:   "permissive security",
			output: "Current security mode is Permissive Security.",
			want:   "permissive",
		},
		{
			name:   "full security multiline",
			output: "Utility version: 2.0\nCurrent security mode is Full Security.\nAdditional configuration details...",
			want:   "full",
		},
		{
			name:   "mixed case",
			output: "Current Security Mode Is FULL SECURITY.",
			want:   "full",
		},
		{
			name:   "alternative format secureboot on",
			output: "SecureBoot: on\nKernel Security: full",
			want:   "full",
		},
		{
			name:   "unknown format",
			output: "Security configuration unknown",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSecureBootLevel(tt.output)
			if got != tt.want {
				t.Errorf("parseSecureBootLevel(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

// TestCollectSecurityState tests security state collection on a live system.
func TestCollectSecurityState(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping security state test on non-macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	info := &macfor.SystemInfo{
		Architecture: runtime.GOARCH,
	}
	src.collectSecurityState(info)

	// Log the security state (can't assert specific values as they vary by system).
	t.Logf("SIP Enabled: %v", info.SIPEnabled)
	t.Logf("FileVault Enabled: %v", info.FileVaultEnabled)
	t.Logf("Gatekeeper Enabled: %v", info.GatekeeperEnabled)
	if info.Architecture == archARM64 {
		t.Logf("Secure Boot Level: %s", info.SecureBootLevel)
	}

	// On a typical macOS system, at least SIP and Gatekeeper should be queryable.
	// We don't assert specific values as they depend on system configuration.
}

// TestGetSIPStatus tests the getSIPStatus function on a live system.
func TestGetSIPStatus(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping SIP status test on non-macOS")
	}

	enabled, err := getSIPStatus()
	if err != nil {
		t.Logf("getSIPStatus() returned error (may be expected in some environments): %v", err)
		return
	}

	t.Logf("SIP status: enabled=%v", enabled)
}

// TestGetFileVaultStatus tests the getFileVaultStatus function on a live system.
func TestGetFileVaultStatus(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping FileVault status test on non-macOS")
	}

	enabled, err := getFileVaultStatus()
	if err != nil {
		t.Logf("getFileVaultStatus() returned error (may be expected in some environments): %v", err)
		return
	}

	t.Logf("FileVault status: enabled=%v", enabled)
}

// TestGetGatekeeperStatus tests the getGatekeeperStatus function on a live system.
func TestGetGatekeeperStatus(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping Gatekeeper status test on non-macOS")
	}

	enabled, err := getGatekeeperStatus()
	if err != nil {
		t.Logf("getGatekeeperStatus() returned error (may be expected in some environments): %v", err)
		return
	}

	t.Logf("Gatekeeper status: enabled=%v", enabled)
}

// TestCollectNetworkIdentity tests network identity collection on a live system.
func TestCollectNetworkIdentity(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping network identity test on non-macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	info := &macfor.SystemInfo{}
	src.collectNetworkIdentity(info)

	// Hostname should always be available.
	if info.Hostname == "" {
		t.Error("Hostname is empty")
	} else {
		t.Logf("Hostname: %s", info.Hostname)
	}

	// LocalHostname may or may not be set.
	if info.LocalHostname != "" {
		t.Logf("LocalHostname: %s", info.LocalHostname)
	} else {
		t.Log("LocalHostname: (not set)")
	}

	// ComputerName may or may not be set.
	if info.ComputerName != "" {
		t.Logf("ComputerName: %s", info.ComputerName)
	} else {
		t.Log("ComputerName: (not set)")
	}
}

// TestGetLocalHostname tests the getLocalHostname function on a live system.
func TestGetLocalHostname(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping local hostname test on non-macOS")
	}

	hostname, err := getLocalHostname()
	if err != nil {
		t.Logf("getLocalHostname() returned error (may be expected if not set): %v", err)
		return
	}

	if hostname == "" {
		t.Log("getLocalHostname() returned empty string (hostname may not be set)")
	} else {
		t.Logf("LocalHostname: %s", hostname)
	}
}

// TestGetComputerName tests the getComputerName function on a live system.
func TestGetComputerName(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping computer name test on non-macOS")
	}

	name, err := getComputerName()
	if err != nil {
		t.Logf("getComputerName() returned error (may be expected if not set): %v", err)
		return
	}

	if name == "" {
		t.Log("getComputerName() returned empty string (name may not be set)")
	} else {
		t.Logf("ComputerName: %s", name)
	}
}

// TestGetKernelVersion tests the getKernelVersion function.
func TestGetKernelVersion(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping kernel version test on non-macOS")
	}

	version, err := getKernelVersion()
	if err != nil {
		t.Fatalf("getKernelVersion() error: %v", err)
	}

	if version == "" {
		t.Error("getKernelVersion() returned empty string")
	}

	// Kernel version should be in format like "23.2.0".
	// Check it starts with a digit.
	if len(version) == 0 || version[0] < '0' || version[0] > '9' {
		t.Errorf("getKernelVersion() = %q, expected version starting with digit", version)
	}

	t.Logf("Kernel version: %s", version)
}

// TestParseBootTime tests parsing of kern.boottime sysctl output.
func TestParseBootTime(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantSec int64
		wantErr bool
	}{
		{
			name:    "standard format",
			raw:     "{ sec = 1706428530, usec = 0 } Thu Jan 28 09:15:30 2026",
			wantSec: 1706428530,
		},
		{
			name:    "different timestamp",
			raw:     "{ sec = 1700000000, usec = 123456 } Tue Nov 14 22:13:20 2023",
			wantSec: 1700000000,
		},
		{
			name:    "braces only",
			raw:     "{ sec = 1706428530, usec = 0 }",
			wantSec: 1706428530,
		},
		{
			name:    "missing sec",
			raw:     "{ usec = 0 }",
			wantErr: true,
		},
		{
			name:    "empty string",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "invalid sec value",
			raw:     "{ sec = abc, usec = 0 }",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBootTime(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Unix() != tt.wantSec {
				t.Errorf("parseBootTime() unix = %d, want %d", got.Unix(), tt.wantSec)
			}
		})
	}
}

// TestGetBootTime tests boot time retrieval on a live system.
func TestGetBootTime(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping boot time test on non-macOS")
	}

	bootTime, err := getBootTime()
	if err != nil {
		t.Fatalf("getBootTime() error: %v", err)
	}

	// Boot time should be in the past.
	if bootTime.After(time.Now()) {
		t.Errorf("boot time %v is in the future", bootTime)
	}

	// Boot time should be reasonable (within last year).
	if time.Since(bootTime) > 365*24*time.Hour {
		t.Errorf("boot time %v is more than a year ago", bootTime)
	}

	t.Logf("Boot time: %s (uptime: %s)", bootTime, time.Since(bootTime).Round(time.Second))
}

// TestCollectBootVolumeInfo tests boot volume info collection on a live system.
func TestCollectBootVolumeInfo(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping boot volume info test on non-macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	info := &macfor.SystemInfo{}
	src.collectBootVolumeInfo(info)

	// Boot volume name should typically be set.
	if info.BootVolumeName == "" {
		t.Log("BootVolumeName is empty (may be expected in some environments)")
	} else {
		t.Logf("BootVolumeName: %s", info.BootVolumeName)
	}

	// Volume UUID should typically be set.
	if info.BootVolumeUUID == "" {
		t.Log("BootVolumeUUID is empty (may be expected in some environments)")
	} else {
		t.Logf("BootVolumeUUID: %s", info.BootVolumeUUID)
	}

	// Filesystem type should typically be "apfs" on modern macOS.
	if info.BootVolumeFS == "" {
		t.Log("BootVolumeFS is empty (may be expected in some environments)")
	} else {
		t.Logf("BootVolumeFS: %s", info.BootVolumeFS)
	}
}

// TestFormatTimezoneOffset tests the formatTimezoneOffset function.
func TestFormatTimezoneOffset(t *testing.T) {
	tests := []struct {
		offsetSeconds int
		want          string
	}{
		{0, "+00:00"},
		{3600, "+01:00"},
		{-3600, "-01:00"},
		{7200, "+02:00"},
		{-7200, "-02:00"},
		{5400, "+01:30"},   // 1.5 hours
		{-5400, "-01:30"},  // -1.5 hours
		{36000, "+10:00"},  // Sydney/Melbourne
		{-28800, "-08:00"}, // Pacific Time
		{19800, "+05:30"},  // India
		{-12600, "-03:30"}, // Newfoundland
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatTimezoneOffset(tt.offsetSeconds)
			if got != tt.want {
				t.Errorf("formatTimezoneOffset(%d) = %q, want %q", tt.offsetSeconds, got, tt.want)
			}
		})
	}
}

// TestPadZero tests the padZero function.
func TestPadZero(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "00"},
		{1, "01"},
		{9, "09"},
		{10, "10"},
		{23, "23"},
		{59, "59"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := padZero(tt.n)
			if got != tt.want {
				t.Errorf("padZero(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

// TestSystemInfoIntegration tests the full SystemInfo method on a live system.
func TestSystemInfoIntegration(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("skipping integration test on non-macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo() error: %v", err)
	}

	// Validate required fields.
	if info.OSName != "macOS" {
		t.Errorf("OSName = %q, want %q", info.OSName, "macOS")
	}

	if info.OSVersion == "" {
		t.Error("OSVersion is empty")
	}

	if info.Architecture == "" {
		t.Error("Architecture is empty")
	}

	// Architecture should be arm64 or amd64.
	if info.Architecture != archARM64 && info.Architecture != "amd64" {
		t.Errorf("Architecture = %q, want %s or amd64", info.Architecture, archARM64)
	}

	// Check collection time is reasonable (within last minute).
	if time.Since(info.CollectionTime) > time.Minute {
		t.Errorf("CollectionTime %v is too old", info.CollectionTime)
	}

	// CollectionUser should not be empty.
	if info.CollectionUser == "" {
		t.Error("CollectionUser is empty")
	}

	// CollectionUID should be non-negative.
	if info.CollectionUID < 0 {
		t.Errorf("CollectionUID = %d, want >= 0", info.CollectionUID)
	}

	// SourceType should be live.
	if info.SourceType != "live" {
		t.Errorf("SourceType = %q, want %q", info.SourceType, "live")
	}

	// Hardware info should be populated (MACFOR-SYS-3).
	if info.HardwareModel == "" {
		t.Error("HardwareModel is empty")
	}
	if info.HardwareModelName == "" {
		t.Error("HardwareModelName is empty")
	}
	if info.CPUCores <= 0 {
		t.Errorf("CPUCores = %d, want > 0", info.CPUCores)
	}
	if info.MemoryGB <= 0 {
		t.Errorf("MemoryGB = %d, want > 0", info.MemoryGB)
	}

	// Boot time should be set (MACFOR-SYS-6).
	if info.BootTime.IsZero() {
		t.Error("BootTime is zero")
	} else if info.BootTime.After(time.Now()) {
		t.Error("BootTime is in the future")
	}
	if info.UptimeSeconds <= 0 {
		t.Errorf("UptimeSeconds = %d, want > 0", info.UptimeSeconds)
	}

	// Boot volume should be populated (MACFOR-SYS-7).
	if info.BootVolumeName == "" {
		t.Log("BootVolumeName is empty (may be expected in some environments)")
	}
	if info.BootVolumeFS == "" {
		t.Log("BootVolumeFS is empty (may be expected in some environments)")
	}

	// Log all collected info.
	t.Logf("OS: %s %s (%s) - %s", info.OSName, info.OSVersion, info.OSBuild, info.OSCodename)
	t.Logf("Kernel: %s", info.KernelVersion)
	t.Logf("Architecture: %s", info.Architecture)
	t.Logf("Hardware: %s (%s)", info.HardwareModel, info.HardwareModelName)
	t.Logf("CPU: %s (%d cores)", info.CPUType, info.CPUCores)
	t.Logf("Memory: %d GB", info.MemoryGB)
	if info.SerialNumber != "" {
		t.Logf("Serial: %s", info.SerialNumber)
	}
	t.Logf("Security: SIP=%v, FileVault=%v, Gatekeeper=%v", info.SIPEnabled, info.FileVaultEnabled, info.GatekeeperEnabled)
	if info.SecureBootLevel != "" {
		t.Logf("Secure Boot: %s", info.SecureBootLevel)
	}
	t.Logf("Network: Hostname=%s, LocalHostname=%s, ComputerName=%s", info.Hostname, info.LocalHostname, info.ComputerName)
	t.Logf("User: %s (UID %d, root=%v)", info.CollectionUser, info.CollectionUID, info.IsRoot)
	t.Logf("Timezone: %s (%s)", info.Timezone, info.TimezoneOffset)
	t.Logf("Boot Time: %s (Uptime: %ds)", info.BootTime, info.UptimeSeconds)
	t.Logf("Boot Volume: %s (%s, %s)", info.BootVolumeName, info.BootVolumeFS, info.BootVolumeUUID)
}
