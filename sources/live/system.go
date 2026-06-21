package live

import (
	"fmt"
	"os"
	"sync"

	"howett.net/plist"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// systemVersionPlistPath is the path to the macOS system version plist.
const systemVersionPlistPath = "/System/Library/CoreServices/SystemVersion.plist"

// osVersionCache stores the cached OS version and any error from reading it.
// The cache is populated on first call to OSVersion() and reused thereafter.
type osVersionCache struct {
	once    sync.Once
	version string
	err     error
}

var osVersionCached osVersionCache

// systemVersionPlist represents the structure of SystemVersion.plist.
// We only parse the fields we need.
type systemVersionPlist struct {
	ProductName         string `plist:"ProductName"`
	ProductVersion      string `plist:"ProductVersion"`
	ProductBuildVersion string `plist:"ProductBuildVersion"`
}

// OSVersion returns the macOS version of the live system.
// The version is read from /System/Library/CoreServices/SystemVersion.plist
// and cached for subsequent calls.
//
// Returns the version string (e.g., "14.2.1" for Sonoma).
func (s *Source) OSVersion() (string, error) {
	osVersionCached.once.Do(func() {
		osVersionCached.version, osVersionCached.err = s.readOSVersion()
	})
	return osVersionCached.version, osVersionCached.err
}

// readOSVersion reads the OS version from the system plist file.
func (s *Source) readOSVersion() (string, error) {
	var sv systemVersionPlist
	if err := s.ReadPlist(systemVersionPlistPath, &sv); err != nil {
		return "", fmt.Errorf("reading system version: %w", err)
	}

	if sv.ProductVersion == "" {
		return "", fmt.Errorf("ProductVersion not found in %s", systemVersionPlistPath)
	}

	return sv.ProductVersion, nil
}

// Hostname returns the hostname of the live system.
func (s *Source) Hostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", macfor.WrapIOError("hostname", "system", err)
	}
	return hostname, nil
}

// ReadPlist reads and parses a plist file into the provided value.
// The value should be a pointer to a struct or map that matches
// the plist structure.
//
// This method uses howett.net/plist for parsing and supports
// both XML and binary plist formats.
func (s *Source) ReadPlist(path string, v any) error {
	resolved := s.ResolvePath(path)

	f, err := os.Open(resolved) //nolint:gosec // G304: Intentional file access for forensic collection
	if err != nil {
		return wrapOSError("open", resolved, err)
	}
	defer func() { _ = f.Close() }()

	decoder := plist.NewDecoder(f)
	if err := decoder.Decode(v); err != nil {
		return macfor.WrapParseError(resolved, 0, 0, "plist decode failed", err)
	}

	return nil
}
