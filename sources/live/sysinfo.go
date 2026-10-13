package live

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"howett.net/plist"

	"github.com/macfor-io/macfor-community/internal/sysinfo"
	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// System-detection value constants.
const (
	osNameMacOS = "macOS"

	// sipFull is the "full" System Integrity Protection level.
	sipFull = "full"

	// Apple Silicon CPU type identifiers.
	cpuAppleM1      = "Apple M1"
	cpuAppleM1Ultra = "Apple M1 Ultra"
	cpuAppleM2      = "Apple M2"
	cpuAppleM3      = "Apple M3"
	cpuAppleM4      = "Apple M4"
)

// SystemInfo returns comprehensive system information for the live system.
// It collects OS version, hardware info, security state, and other forensically
// relevant data. Collection continues even if individual components fail,
// with errors logged but not returned (graceful degradation).
func (s *Source) SystemInfo() (*macfor.SystemInfo, error) {
	info := &macfor.SystemInfo{
		OSName:         osNameMacOS,
		Architecture:   runtime.GOARCH,
		SourceType:     macfor.SourceTypeLive,
		CollectionTime: time.Now().UTC(),
		CollectionUser: os.Getenv("USER"),
		CollectionUID:  os.Getuid(),
		IsRoot:         os.Getuid() == 0,
	}
	if granted, known := FullDiskAccess(); known {
		info.FullDiskAccess = &granted
	}

	// Collect OS version information (MACFOR-SYS-2).
	if err := s.collectOSVersion(info); err != nil {
		// Log but continue - OS version is critical but we shouldn't fail entirely.
		// The error is still returned if OSVersion is empty.
		if info.OSVersion == "" {
			return nil, fmt.Errorf("collecting OS version: %w", err)
		}
	}

	// Collect hardware information (MACFOR-SYS-3).
	s.collectHardwareInfo(info)

	// Collect security state (MACFOR-SYS-4).
	s.collectSecurityState(info)

	// Collect network identity (MACFOR-SYS-5).
	s.collectNetworkIdentity(info)

	// Collect boot volume information (MACFOR-SYS-7).
	s.collectBootVolumeInfo(info)

	// Collect temporal context (MACFOR-SYS-6).
	info.Timezone = systemTimezone()
	_, offset := time.Now().Zone()
	info.TimezoneOffset = formatTimezoneOffset(offset)

	// Boot time and uptime.
	if bootTime, err := getBootTime(); err == nil {
		info.BootTime = bootTime
		info.UptimeSeconds = int64(time.Since(bootTime).Seconds())
	}

	return info, nil
}

// collectOSVersion collects OS version information from SystemVersion.plist
// and the kernel version from sysctl.
//
// This implements MACFOR-SYS-2: OS Version Collection.
func (s *Source) collectOSVersion(info *macfor.SystemInfo) error {
	// Read SystemVersion.plist for version and build.
	var sv systemVersionPlist
	if err := s.ReadPlist(systemVersionPlistPath, &sv); err != nil {
		return fmt.Errorf("reading SystemVersion.plist: %w", err)
	}

	// Apply the parsed version info.
	if err := applySystemVersion(info, sv); err != nil {
		return err
	}

	// Get kernel version from sysctl.
	if kernelVersion, err := getKernelVersion(); err == nil {
		info.KernelVersion = kernelVersion
	}
	// Kernel version failure is non-fatal.

	return nil
}

// applySystemVersion populates OS version fields from parsed SystemVersion.plist data.
// This function is separated from collectOSVersion to enable unit testing with sample data.
func applySystemVersion(info *macfor.SystemInfo, sv systemVersionPlist) error {
	if sv.ProductVersion == "" {
		return fmt.Errorf("ProductVersion not found in SystemVersion.plist")
	}

	info.OSVersion = sv.ProductVersion
	info.OSBuild = sv.ProductBuildVersion
	info.OSCodename = macfor.GetOSCodename(sv.ProductVersion)

	return nil
}

// collectHardwareInfo collects hardware information via sysctl and IOKit.
// All errors are non-fatal; fields are left empty if collection fails.
//
// This implements MACFOR-SYS-3: Hardware Information Collection.
func (s *Source) collectHardwareInfo(info *macfor.SystemInfo) {
	// Hardware model from hw.model (e.g., "Mac14,2").
	if model, err := sysctlString("hw.model"); err == nil {
		info.HardwareModel = model
		// Get human-readable name from comprehensive database.
		info.HardwareModelName = sysinfo.GetModelName(model)
	}

	// CPU type from machdep.cpu.brand_string (e.g., "Apple M2").
	// Note: This sysctl may not be available on Apple Silicon.
	if cpuType, err := sysctlString("machdep.cpu.brand_string"); err == nil {
		info.CPUType = cpuType
	} else {
		// Fallback for Apple Silicon: derive from model identifier.
		info.CPUType = deriveAppleSiliconCPUType(info.HardwareModel)
	}

	// Physical CPU cores from hw.physicalcpu.
	if cores, err := sysctlInt("hw.physicalcpu"); err == nil {
		info.CPUCores = cores
	}

	// Total memory from hw.memsize (bytes).
	if memBytes, err := sysctlInt64("hw.memsize"); err == nil {
		info.MemoryGB = int(memBytes / (1024 * 1024 * 1024))
	}

	// Serial number from IOKit (best effort, may require elevated privileges).
	if serial, err := getSerialNumber(); err == nil {
		info.SerialNumber = serial
	}
}

// sysctlString retrieves a string value from sysctl.
func sysctlString(name string) (string, error) {
	ctx := context.Background()
	out, err := exec.CommandContext(ctx, "sysctl", "-n", name).Output()
	if err != nil {
		return "", fmt.Errorf("sysctl %s: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// sysctlInt retrieves an integer value from sysctl.
func sysctlInt(name string) (int, error) {
	s, err := sysctlString(name)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// sysctlInt64 retrieves an int64 value from sysctl.
func sysctlInt64(name string) (int64, error) {
	s, err := sysctlString(name)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}

// deriveAppleSiliconCPUType derives the CPU type from an Apple Silicon model identifier.
// Returns empty string for Intel models or unknown identifiers.
func deriveAppleSiliconCPUType(model string) string {
	gen := sysinfo.GetModelGeneration(model)
	switch gen {
	case "M4":
		return cpuAppleM4
	case "M3":
		return cpuAppleM3
	case "M2":
		return cpuAppleM2
	case "M1 Ultra":
		return cpuAppleM1Ultra
	case "M1":
		return cpuAppleM1
	default:
		return ""
	}
}

// getSerialNumber retrieves the system serial number from IOKit.
// This may fail without elevated privileges on some systems.
func getSerialNumber() (string, error) {
	ctx := context.Background()
	// Use ioreg to query the IOPlatformSerialNumber property.
	out, err := exec.CommandContext(ctx, "ioreg", "-c", "IOPlatformExpertDevice", "-d", "2").Output()
	if err != nil {
		return "", fmt.Errorf("ioreg: %w", err)
	}

	// Parse output to find IOPlatformSerialNumber.
	// Format: "IOPlatformSerialNumber" = "XXXXXXXX"
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "IOPlatformSerialNumber") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				serial := strings.TrimSpace(parts[1])
				serial = strings.Trim(serial, `"`)
				if serial != "" {
					return serial, nil
				}
			}
		}
	}

	return "", fmt.Errorf("serial number not found in ioreg output")
}

// getKernelVersion returns the Darwin kernel version using sysctl.
// Example output: "23.2.0" for macOS 14.2.
func getKernelVersion() (string, error) {
	// Use a background context since sysctl is a fast local operation.
	ctx := context.Background()
	out, err := exec.CommandContext(ctx, "sysctl", "-n", "kern.osrelease").Output()
	if err != nil {
		return "", fmt.Errorf("sysctl kern.osrelease: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// collectSecurityState collects security configuration state.
// All errors are non-fatal; fields are left at default values if collection fails.
//
// This implements MACFOR-SYS-4: Security State Collection.
func (s *Source) collectSecurityState(info *macfor.SystemInfo) {
	// SIP (System Integrity Protection) status from csrutil.
	if sipEnabled, err := getSIPStatus(); err == nil {
		info.SIPEnabled = sipEnabled
	}

	// FileVault status from fdesetup.
	if fvEnabled, err := getFileVaultStatus(); err == nil {
		info.FileVaultEnabled = fvEnabled
		// Boot volume encryption follows FileVault status.
		info.BootVolumeEncrypted = fvEnabled
	}

	// Gatekeeper status from spctl.
	if gkEnabled, err := getGatekeeperStatus(); err == nil {
		info.GatekeeperEnabled = gkEnabled
	}

	// Secure boot level (Apple Silicon only).
	if info.Architecture == "arm64" {
		if level, err := getSecureBootLevel(); err == nil {
			info.SecureBootLevel = level
		}
	}
}

// getSIPStatus checks System Integrity Protection status using csrutil.
// Returns true if SIP is enabled, false otherwise.
func getSIPStatus() (bool, error) {
	ctx := context.Background()
	out, err := exec.CommandContext(ctx, "csrutil", "status").CombinedOutput()
	if err != nil {
		// csrutil may fail on older systems or in certain contexts.
		return false, fmt.Errorf("csrutil status: %w", err)
	}

	output := string(out)
	return parseSIPStatus(output), nil
}

// parseSIPStatus parses the output of csrutil status.
// Example outputs:
//   - "System Integrity Protection status: enabled."
//   - "System Integrity Protection status: disabled."
//   - "System Integrity Protection status: enabled (Custom Configuration)."
func parseSIPStatus(output string) bool {
	// SIP is enabled if output contains "enabled" (regardless of custom config).
	lower := strings.ToLower(output)
	return strings.Contains(lower, "status: enabled")
}

// getFileVaultStatus checks FileVault encryption status using fdesetup.
// Returns true if FileVault is enabled, false otherwise.
func getFileVaultStatus() (bool, error) {
	ctx := context.Background()
	out, err := exec.CommandContext(ctx, "fdesetup", "status").CombinedOutput()
	if err != nil {
		// fdesetup may require elevated privileges for full info.
		// But status command usually works without root.
		return false, fmt.Errorf("fdesetup status: %w", err)
	}

	output := string(out)
	return parseFileVaultStatus(output), nil
}

// parseFileVaultStatus parses the output of fdesetup status.
// Example outputs:
//   - "FileVault is On."
//   - "FileVault is Off."
//   - "FileVault is On, but is currently Decrypting."
func parseFileVaultStatus(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "filevault is on")
}

// getGatekeeperStatus checks Gatekeeper status using spctl.
// Returns true if Gatekeeper is enabled, false otherwise.
func getGatekeeperStatus() (bool, error) {
	ctx := context.Background()
	out, err := exec.CommandContext(ctx, "spctl", "--status").CombinedOutput()
	if err != nil {
		// spctl --status returns non-zero exit code when disabled.
		// Check output to determine actual status.
		output := string(out)
		if strings.Contains(strings.ToLower(output), "disabled") {
			return false, nil
		}
		return false, fmt.Errorf("spctl --status: %w", err)
	}

	output := string(out)
	return parseGatekeeperStatus(output), nil
}

// parseGatekeeperStatus parses the output of spctl --status.
// Example outputs:
//   - "assessments enabled"
//   - "assessments disabled"
func parseGatekeeperStatus(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "assessments enabled")
}

// getSecureBootLevel retrieves the Secure Boot level on Apple Silicon Macs.
// Returns the security level: "full", "reduced", or "permissive".
func getSecureBootLevel() (string, error) {
	ctx := context.Background()
	// bputil -d shows current security configuration.
	out, err := exec.CommandContext(ctx, "bputil", "-d").CombinedOutput()
	if err != nil {
		// bputil may not be available or may require elevated privileges.
		return "", fmt.Errorf("bputil -d: %w", err)
	}

	output := string(out)
	return parseSecureBootLevel(output), nil
}

// parseSecureBootLevel parses the output of bputil -d to determine secure boot level.
// Example outputs contain lines like:
//   - "Current security mode is Full Security"
//   - "Current security mode is Reduced Security"
//   - "Current security mode is Permissive Security"
func parseSecureBootLevel(output string) string {
	lower := strings.ToLower(output)

	switch {
	case strings.Contains(lower, "full security"):
		return sipFull
	case strings.Contains(lower, "reduced security"):
		return "reduced"
	case strings.Contains(lower, "permissive security"):
		return "permissive"
	default:
		// Try alternative patterns.
		if strings.Contains(lower, "secureboot: on") || strings.Contains(lower, "secure boot: on") {
			return sipFull
		}
		return ""
	}
}

// collectNetworkIdentity collects hostname variants.
// All errors are non-fatal; fields are left empty if collection fails.
//
// This implements MACFOR-SYS-5: Network Identity Collection.
func (s *Source) collectNetworkIdentity(info *macfor.SystemInfo) {
	// Primary hostname from os.Hostname().
	if hostname, err := s.Hostname(); err == nil {
		info.Hostname = hostname
	}

	// Local hostname (Bonjour name) from scutil.
	if localHostname, err := getLocalHostname(); err == nil {
		info.LocalHostname = localHostname
	}

	// Computer name (sharing/display name) from scutil.
	if computerName, err := getComputerName(); err == nil {
		info.ComputerName = computerName
	}
}

// getLocalHostname retrieves the local hostname (Bonjour name) using scutil.
// This is the name used for local network discovery (e.g., "my-mac" in "my-mac.local").
func getLocalHostname() (string, error) {
	ctx := context.Background()
	out, err := exec.CommandContext(ctx, "scutil", "--get", "LocalHostName").CombinedOutput()
	if err != nil {
		// LocalHostName may not be set on all systems.
		return "", fmt.Errorf("scutil --get LocalHostName: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// getComputerName retrieves the computer name (sharing/display name) using scutil.
// This is the user-friendly name shown in Finder and System Preferences.
func getComputerName() (string, error) {
	ctx := context.Background()
	out, err := exec.CommandContext(ctx, "scutil", "--get", "ComputerName").CombinedOutput()
	if err != nil {
		// ComputerName may not be set on all systems.
		return "", fmt.Errorf("scutil --get ComputerName: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// getBootTime retrieves the system boot time from sysctl kern.boottime.
// The output format is: "{ sec = 1706428530, usec = 0 } Thu Jan 28 09:15:30 2026".
func getBootTime() (time.Time, error) {
	raw, err := sysctlString("kern.boottime")
	if err != nil {
		return time.Time{}, err
	}
	return parseBootTime(raw)
}

// parseBootTime parses the kern.boottime sysctl output.
// Expected format: "{ sec = 1706428530, usec = 0 } Thu Jan 28 09:15:30 2026".
func parseBootTime(raw string) (time.Time, error) {
	// Find "sec = " that is NOT preceded by 'u' (to avoid matching "usec = ").
	secStart := -1
	for i := 0; i < len(raw); i++ {
		if strings.HasPrefix(raw[i:], "sec = ") {
			if i == 0 || raw[i-1] != 'u' {
				secStart = i + len("sec = ")
				break
			}
		}
	}
	if secStart < 0 {
		return time.Time{}, fmt.Errorf("parsing kern.boottime: sec not found in %q", raw)
	}

	secEnd := strings.Index(raw[secStart:], ",")
	if secEnd < 0 {
		// Try space as delimiter if comma not found.
		secEnd = strings.Index(raw[secStart:], " ")
		if secEnd < 0 {
			return time.Time{}, fmt.Errorf("parsing kern.boottime: sec delimiter not found in %q", raw)
		}
	}

	secStr := strings.TrimSpace(raw[secStart : secStart+secEnd])
	sec, err := strconv.ParseInt(secStr, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing kern.boottime sec value %q: %w", secStr, err)
	}

	return time.Unix(sec, 0).UTC(), nil
}

// collectBootVolumeInfo collects boot volume information via diskutil.
// All errors are non-fatal; fields are left empty if collection fails.
//
// This implements MACFOR-SYS-7: Boot Volume Information.
func (s *Source) collectBootVolumeInfo(info *macfor.SystemInfo) {
	ctx := context.Background()
	out, err := exec.CommandContext(ctx, "diskutil", "info", "-plist", "/").Output()
	if err != nil {
		return
	}

	var di diskutilInfo
	if _, err := plist.Unmarshal(out, &di); err != nil {
		return
	}

	if di.VolumeName != "" {
		info.BootVolumeName = di.VolumeName
	}
	if di.VolumeUUID != "" {
		info.BootVolumeUUID = di.VolumeUUID
	}
	if di.FilesystemType != "" {
		info.BootVolumeFS = strings.ToLower(di.FilesystemType)
	}
}

// diskutilInfo represents the plist output from diskutil info -plist.
type diskutilInfo struct {
	VolumeName     string `plist:"VolumeName"`
	VolumeUUID     string `plist:"VolumeUUID"`
	FilesystemType string `plist:"FilesystemType"`
}

// formatTimezoneOffset formats an offset in seconds as "+HH:MM" or "-HH:MM".
func formatTimezoneOffset(offsetSeconds int) string {
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return sign + padZero(hours) + ":" + padZero(minutes)
}

// padZero pads a number to two digits with a leading zero if needed.
func padZero(n int) string {
	return fmt.Sprintf("%02d", n)
}
