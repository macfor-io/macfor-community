package live

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"
)

// tccDBPath is the system TCC database. Its file mode is world-readable,
// but macOS denies opening it to any process, root included, that has not
// been granted Full Disk Access, which makes it a reliable probe.
const tccDBPath = "/Library/Application Support/com.apple.TCC/TCC.db"

// FullDiskAccess reports whether this process has Full Disk Access. The
// second result is false when access could not be determined.
func FullDiskAccess() (granted, known bool) {
	return probeFullDiskAccess(tccDBPath)
}

// probeFullDiskAccess attempts to open a TCC-protected file read-only.
func probeFullDiskAccess(path string) (granted, known bool) {
	f, err := os.Open(path) //nolint:gosec // fixed system path, opened read-only.
	if err == nil {
		_ = f.Close()
		return true, true
	}
	if errors.Is(err, fs.ErrPermission) {
		return false, true
	}
	return false, false
}

// localtimePath is the symlink macOS points at the system time zone.
const localtimePath = "/etc/localtime"

// systemTimezone returns the system's IANA time zone name (for example
// "Australia/Sydney"). It reads the /etc/localtime symlink rather than the
// process environment, since the collection records the system's zone. It
// falls back to Go's local zone name when the link cannot be resolved.
func systemTimezone() string {
	if name := timezoneFromLink(localtimePath); name != "" {
		return name
	}
	return time.Local.String()
}

// timezoneFromLink extracts the zone name from a zoneinfo symlink target
// such as /var/db/timezone/zoneinfo/Australia/Sydney.
func timezoneFromLink(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	const marker = "zoneinfo/"
	i := strings.LastIndex(target, marker)
	if i < 0 {
		return ""
	}
	return target[i+len(marker):]
}
