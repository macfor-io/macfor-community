package directory

import (
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// unknownValue is the placeholder returned when a system information field
// cannot be determined from the directory tree.
const unknownValue = "unknown"

// osNameMacOS is the operating system name for macOS sources.
const osNameMacOS = "macOS"

// systemVersionPlist represents the structure of SystemVersion.plist.
type systemVersionPlist struct {
	ProductName         string `plist:"ProductName"`
	ProductVersion      string `plist:"ProductVersion"`
	ProductBuildVersion string `plist:"ProductBuildVersion"`
}

// preferencesPlist represents the relevant fields of the SystemConfiguration preferences.plist.
type preferencesPlist struct {
	ComputerName  string `plist:"ComputerName"`
	HostName      string `plist:"HostName"`
	LocalHostName string `plist:"LocalHostName"`
}

// platformPlist represents the com.apple.platform.plist structure for hardware model.
type platformPlist struct {
	HardwareModel string `plist:"hardware-model"`
}

// OSVersion returns the macOS version from SystemVersion.plist in the directory tree.
// Returns unknownValue (not an error) if the plist is absent or cannot be parsed.
func (s *Source) OSVersion() (string, error) {
	var sv systemVersionPlist
	if err := s.ReadPlist("System/Library/CoreServices/SystemVersion.plist", &sv); err != nil {
		return unknownValue, nil //nolint:nilerr // Intentional: missing plist is not an error for directory sources.
	}
	if sv.ProductVersion == "" {
		return unknownValue, nil
	}
	return sv.ProductVersion, nil
}

// Hostname returns the computer name from the SystemConfiguration preferences.plist.
// Returns unknownValue (not an error) if the plist is absent or cannot be parsed.
func (s *Source) Hostname() (string, error) {
	var prefs preferencesPlist
	if err := s.ReadPlist("Library/Preferences/SystemConfiguration/preferences.plist", &prefs); err != nil {
		return unknownValue, nil //nolint:nilerr // Intentional: missing plist is not an error for directory sources.
	}
	if prefs.ComputerName != "" {
		return prefs.ComputerName, nil
	}
	if prefs.HostName != "" {
		return prefs.HostName, nil
	}
	return unknownValue, nil
}

// SystemInfo returns system information derived from plist files in the directory tree.
// Fields that cannot be determined from files are set to unknownValue or zero values.
// This method never returns an error -- it always returns a populated struct.
func (s *Source) SystemInfo() (*macfor.SystemInfo, error) {
	info := &macfor.SystemInfo{
		OSName:         osNameMacOS,
		Architecture:   unknownValue,
		SourceType:     macfor.SourceTypeDirectory,
		ImagePath:      s.root,
		CollectionTime: time.Now().UTC(),
	}

	// Read OS version.
	var sv systemVersionPlist
	if err := s.ReadPlist("System/Library/CoreServices/SystemVersion.plist", &sv); err == nil {
		info.OSVersion = sv.ProductVersion
		info.OSBuild = sv.ProductBuildVersion
		if sv.ProductVersion != "" {
			info.OSCodename = macfor.GetOSCodename(sv.ProductVersion)
		}
	}
	if info.OSVersion == "" {
		info.OSVersion = unknownValue
	}

	// Read hostname and computer name.
	var prefs preferencesPlist
	if err := s.ReadPlist("Library/Preferences/SystemConfiguration/preferences.plist", &prefs); err == nil {
		info.Hostname = prefs.HostName
		info.ComputerName = prefs.ComputerName
		info.LocalHostname = prefs.LocalHostName
	}
	if info.Hostname == "" {
		info.Hostname = unknownValue
	}

	// Read hardware model.
	var platform platformPlist
	if err := s.ReadPlist("private/var/db/SystemConfiguration/com.apple.platform.plist", &platform); err == nil {
		info.HardwareModel = platform.HardwareModel
		if platform.HardwareModel != "" {
			info.HardwareModelName = macfor.GetHardwareModelName(platform.HardwareModel)
		}
	}
	if info.HardwareModel == "" {
		info.HardwareModel = unknownValue
	}

	return info, nil
}
