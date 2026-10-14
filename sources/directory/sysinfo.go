package directory

import (
	"os"
	"regexp"
	"strings"
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

// globalPreferencesPlist represents the time zone fields of the system-wide
// Library/Preferences/.GlobalPreferences.plist. The selected-city dictionary is
// written by the Date & Time preference pane when a zone is chosen manually.
type globalPreferencesPlist struct {
	SelectedCity struct {
		TimeZoneName string `plist:"TimeZoneName"`
	} `plist:"com.apple.preferences.timezone.selected_city"`
}

// localtimePath is the source path of the system time zone symlink. It is
// read under private/etc rather than etc so the /etc -> private/etc symlink in
// the tree is never traversed.
const localtimePath = "private/etc/localtime"

// globalPreferencesPath is the source path of the system-wide global preferences.
const globalPreferencesPath = "Library/Preferences/.GlobalPreferences.plist"

// zoneinfoMarker separates the zoneinfo database directory from the zone name
// in a localtime symlink target.
const zoneinfoMarker = "zoneinfo/"

// ianaZoneName matches a plausible IANA time zone name such as "UTC",
// "Australia/Sydney", "Etc/GMT+10", or "America/Argentina/Buenos_Aires".
var ianaZoneName = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)*$`)

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
		// The collection fields describe the host running macfor, never the
		// collected tree, so they are set the same way as for a live source.
		CollectionUser: os.Getenv("USER"),
		CollectionUID:  os.Getuid(),
		IsRoot:         os.Getuid() == 0,
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
			info.HardwareModelName = platform.HardwareModel
			if name, ok := macfor.LookupHardwareModelName(platform.HardwareModel); ok {
				info.HardwareModelName = name
			}
		}
	}
	if info.HardwareModel == "" {
		info.HardwareModel = unknownValue
	} else if arch := macfor.HardwareArchitecture(info.HardwareModel); arch != "" {
		// Architecture is derived from the hardware model, never from
		// System/Library/Kernels/kernel: on Apple silicon that file is an x86_64
		// Mach-O, so it does not identify the machine's architecture.
		info.Architecture = arch
	}

	// The time zone name is read from the tree. TimezoneOffset is deliberately
	// left empty: the offset depends on the date (DST) and could only be computed
	// with the collecting host's tzdata, which would mix host state into the
	// evidence description.
	info.Timezone = s.timezone()

	// BootTime, UptimeSeconds, SIPEnabled, FileVaultEnabled, and
	// GatekeeperEnabled are runtime state of a booted system and cannot be
	// derived from a directory tree, so they stay at their zero values.

	return info, nil
}

// timezone returns the IANA time zone name of the tree: first from the
// private/etc/localtime symlink target, then from the selected city in the
// global preferences. It returns unknownValue if neither yields a plausible name.
func (s *Source) timezone() string {
	if name := s.timezoneFromLocaltime(); name != "" {
		return name
	}
	var prefs globalPreferencesPlist
	if err := s.ReadPlist(globalPreferencesPath, &prefs); err == nil {
		if name := prefs.SelectedCity.TimeZoneName; isZoneName(name) {
			return name
		}
	}
	return unknownValue
}

// timezoneFromLocaltime reads the localtime symlink target without following
// it. On macOS the target is absolute (for example
// /var/db/timezone/zoneinfo/Australia/Sydney), so following it would read the
// collecting host's file rather than the tree's. It returns "" when the link is
// absent, not a symlink, or does not name a plausible zone.
func (s *Source) timezoneFromLocaltime() string {
	target, err := s.Readlink(localtimePath)
	if err != nil {
		return ""
	}
	return zoneFromLinkTarget(target)
}

// zoneFromLinkTarget extracts the zone name following the last "zoneinfo/"
// segment of a localtime symlink target, accepting absolute and relative
// targets alike. It returns "" if the result is not a plausible IANA name.
func zoneFromLinkTarget(target string) string {
	i := strings.LastIndex(target, zoneinfoMarker)
	if i < 0 {
		return ""
	}
	if i > 0 && target[i-1] != '/' {
		return ""
	}
	name := target[i+len(zoneinfoMarker):]
	if !isZoneName(name) {
		return ""
	}
	return name
}

// isZoneName reports whether name is a plausible IANA time zone name.
func isZoneName(name string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}
	return ianaZoneName.MatchString(name)
}
