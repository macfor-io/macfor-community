package macfor

import "time"

// SystemInfo contains comprehensive system information collected at runtime.
// This data is included in the evidence manifest and made available to plugins
// for version-aware collection and forensic context.
type SystemInfo struct {
	// Operating System
	OSName        string `json:"os_name"`        // Operating system name (always "macOS").
	OSVersion     string `json:"os_version"`     // Major.minor.patch version (e.g., "14.2.1").
	OSBuild       string `json:"os_build"`       // Build identifier (e.g., "23C71").
	OSCodename    string `json:"os_codename"`    // Marketing name (e.g., "Sonoma").
	KernelVersion string `json:"kernel_version"` // Darwin kernel version (e.g., "23.2.0").

	// Hardware
	HardwareModel     string `json:"hardware_model"`                // Model identifier (e.g., "Mac14,2").
	HardwareModelName string `json:"hardware_model_name,omitempty"` // Human-readable model name.
	SerialNumber      string `json:"serial_number,omitempty"`       // System serial number (may be unavailable).
	Architecture      string `json:"architecture"`                  // CPU architecture (e.g., "arm64", "amd64").
	CPUType           string `json:"cpu_type,omitempty"`            // CPU identifier (e.g., "Apple M2").
	CPUCores          int    `json:"cpu_cores,omitempty"`           // Physical core count.
	MemoryGB          int    `json:"memory_gb,omitempty"`           // Total RAM in GB.

	// Storage
	BootVolumeName      string `json:"boot_volume_name,omitempty"`      // Boot volume name (e.g., "Macintosh HD").
	BootVolumeUUID      string `json:"boot_volume_uuid,omitempty"`      // Boot volume UUID.
	BootVolumeFS        string `json:"boot_volume_fs,omitempty"`        // Filesystem type (e.g., "apfs").
	BootVolumeEncrypted bool   `json:"boot_volume_encrypted,omitempty"` // FileVault encryption status.

	// Security State
	SIPEnabled        bool   `json:"sip_enabled"`                  // System Integrity Protection status.
	FileVaultEnabled  bool   `json:"filevault_enabled"`            // FileVault encryption status.
	SecureBootLevel   string `json:"secure_boot_level,omitempty"`  // Secure boot setting (Apple Silicon).
	GatekeeperEnabled bool   `json:"gatekeeper_enabled,omitempty"` // Gatekeeper status.

	// Network Identity
	Hostname      string `json:"hostname"`                 // Primary hostname.
	LocalHostname string `json:"local_hostname,omitempty"` // Bonjour name.
	ComputerName  string `json:"computer_name,omitempty"`  // Sharing/display name.

	// Temporal Context
	Timezone       string    `json:"timezone"`                  // System timezone (e.g., "America/Los_Angeles").
	TimezoneOffset string    `json:"timezone_offset,omitempty"` // UTC offset (e.g., "-08:00").
	BootTime       time.Time `json:"boot_time,omitempty"`       // Last boot timestamp.
	UptimeSeconds  int64     `json:"uptime_seconds,omitempty"`  // System uptime in seconds.
	CollectionTime time.Time `json:"collection_time"`           // When collection was performed.

	// Collection Environment
	CollectionUser string     `json:"collection_user"`            // User running macfor.
	CollectionUID  int        `json:"collection_uid"`             // UID of collection user.
	IsRoot         bool       `json:"is_root"`                    // Whether running as root.
	FullDiskAccess *bool      `json:"full_disk_access,omitempty"` // Whether macfor had Full Disk Access (live only; nil if unknown).
	SourceType     SourceType `json:"source_type"`                // Source type (live or image).
	ImagePath      string     `json:"image_path,omitempty"`       // Path to disk image (if applicable).
	ImageFormat    string     `json:"image_format,omitempty"`     // Image format (dmg, e01, raw).
}

// OSCodenames maps macOS version prefixes to marketing names.
var OSCodenames = map[string]string{
	"15":    "Sequoia",
	"14":    "Sonoma",
	"13":    "Ventura",
	"12":    "Monterey",
	"11":    "Big Sur",
	"10.15": "Catalina",
	"10.14": "Mojave",
	"10.13": "High Sierra",
	"10.12": "Sierra",
	"10.11": "El Capitan",
	"10.10": "Yosemite",
}

// HardwareModels maps model identifiers to human-readable names.
// This is a subset of common models; the full mapping is in internal/sysinfo/models.go.
var HardwareModels = map[string]string{
	// Apple Silicon MacBook Air
	"Mac14,2":  "MacBook Air (M2, 2022)",
	"Mac14,15": "MacBook Air (15-inch, M2, 2023)",
	"Mac15,12": "MacBook Air (13-inch, M3, 2024)",
	"Mac15,13": "MacBook Air (15-inch, M3, 2024)",

	// Apple Silicon MacBook Pro
	"Mac14,7":  "MacBook Pro (13-inch, M2, 2022)",
	"Mac14,5":  "MacBook Pro (14-inch, M2 Pro, 2023)",
	"Mac14,6":  "MacBook Pro (16-inch, M2 Pro, 2023)",
	"Mac14,9":  "MacBook Pro (14-inch, M2 Max, 2023)",
	"Mac14,10": "MacBook Pro (16-inch, M2 Max, 2023)",
	"Mac15,3":  "MacBook Pro (14-inch, M3, 2023)",
	"Mac15,6":  "MacBook Pro (14-inch, M3 Pro, 2023)",
	"Mac15,8":  "MacBook Pro (14-inch, M3 Max, 2023)",
	"Mac15,7":  "MacBook Pro (16-inch, M3 Pro, 2023)",
	"Mac15,9":  "MacBook Pro (16-inch, M3 Max, 2023)",
	"Mac15,10": "MacBook Pro (14-inch, M3 Pro, Nov 2023)",
	"Mac15,11": "MacBook Pro (16-inch, M3 Max, Nov 2023)",

	// M1 MacBooks
	"MacBookAir10,1": "MacBook Air (M1, 2020)",
	"MacBookPro17,1": "MacBook Pro (13-inch, M1, 2020)",
	"MacBookPro18,1": "MacBook Pro (16-inch, M1 Pro, 2021)",
	"MacBookPro18,2": "MacBook Pro (16-inch, M1 Max, 2021)",
	"MacBookPro18,3": "MacBook Pro (14-inch, M1 Pro, 2021)",
	"MacBookPro18,4": "MacBook Pro (14-inch, M1 Max, 2021)",

	// Apple Silicon Mac mini
	"Mac14,3":    "Mac mini (M2, 2023)",
	"Mac14,12":   "Mac mini (M2 Pro, 2023)",
	"Macmini9,1": "Mac mini (M1, 2020)",

	// Apple Silicon Mac Studio
	"Mac13,1":  "Mac Studio (M1 Max, 2022)",
	"Mac13,2":  "Mac Studio (M1 Ultra, 2022)",
	"Mac14,13": "Mac Studio (M2 Max, 2023)",
	"Mac14,14": "Mac Studio (M2 Ultra, 2023)",

	// Apple Silicon Mac Pro
	"Mac14,8": "Mac Pro (M2 Ultra, 2023)",

	// Apple Silicon iMac
	"iMac21,1": "iMac (24-inch, M1, 2021)",
	"iMac21,2": "iMac (24-inch, M1, 2021)",
	"Mac15,4":  "iMac (24-inch, M3, 2023)",
	"Mac15,5":  "iMac (24-inch, M3, 2023)",

	// Intel MacBook Pro (recent models)
	"MacBookPro16,1": "MacBook Pro (16-inch, 2019)",
	"MacBookPro16,2": "MacBook Pro (13-inch, 2020, Four Thunderbolt 3)",
	"MacBookPro16,3": "MacBook Pro (13-inch, 2020, Two Thunderbolt 3)",
	"MacBookPro16,4": "MacBook Pro (16-inch, 2019)",
	"MacBookPro15,1": "MacBook Pro (15-inch, 2018/2019)",
	"MacBookPro15,2": "MacBook Pro (13-inch, 2018/2019)",
	"MacBookPro15,3": "MacBook Pro (15-inch, 2019)",
	"MacBookPro15,4": "MacBook Pro (13-inch, 2019)",

	// Intel MacBook Air (recent models)
	"MacBookAir9,1": "MacBook Air (Retina, 13-inch, 2020)",
	"MacBookAir8,1": "MacBook Air (Retina, 13-inch, 2018)",
	"MacBookAir8,2": "MacBook Air (Retina, 13-inch, 2019)",

	// Intel iMac (recent models)
	"iMac20,1": "iMac (Retina 5K, 27-inch, 2020)",
	"iMac20,2": "iMac (Retina 5K, 27-inch, 2020)",
	"iMac19,1": "iMac (Retina 5K, 27-inch, 2019)",
	"iMac19,2": "iMac (Retina 4K, 21.5-inch, 2019)",

	// Intel Mac mini (recent models)
	"Macmini8,1": "Mac mini (2018)",

	// Intel Mac Pro
	"MacPro7,1": "Mac Pro (2019)",
}

// GetOSCodename returns the marketing codename for a macOS version string.
// It checks both major version (for macOS 11+) and major.minor (for 10.x).
// Returns empty string if no match is found.
func GetOSCodename(version string) string {
	if version == "" {
		return ""
	}

	// Try major version first (11, 12, 13, 14, 15)
	major := extractMajorVersion(version)
	if name, ok := OSCodenames[major]; ok {
		return name
	}

	// Try major.minor for 10.x versions
	majorMinor := extractMajorMinorVersion(version)
	if name, ok := OSCodenames[majorMinor]; ok {
		return name
	}

	return ""
}

// GetHardwareModelName returns the human-readable name for a hardware model identifier.
// Returns the identifier itself if no mapping is found.
func GetHardwareModelName(model string) string {
	if name, ok := HardwareModels[model]; ok {
		return name
	}
	return model
}

// extractMajorVersion extracts the major version from a version string.
// For example, "14.2.1" returns "14".
func extractMajorVersion(version string) string {
	for i, c := range version {
		if c == '.' {
			return version[:i]
		}
	}
	return version
}

// extractMajorMinorVersion extracts major.minor from a version string.
// For example, "10.15.7" returns "10.15".
func extractMajorMinorVersion(version string) string {
	dotCount := 0
	for i, c := range version {
		if c == '.' {
			dotCount++
			if dotCount == 2 {
				return version[:i]
			}
		}
	}
	return version
}
