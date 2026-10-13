package live

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeFullDiskAccess(t *testing.T) {
	dir := t.TempDir()

	readable := filepath.Join(dir, "readable.db")
	if err := os.WriteFile(readable, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if granted, known := probeFullDiskAccess(readable); !granted || !known {
		t.Errorf("readable file: got (%v, %v), want (true, true)", granted, known)
	}

	if granted, known := probeFullDiskAccess(filepath.Join(dir, "missing.db")); granted || known {
		t.Errorf("missing file: got (%v, %v), want (false, false)", granted, known)
	}

	if os.Getuid() != 0 {
		denied := filepath.Join(dir, "denied.db")
		if err := os.WriteFile(denied, []byte("x"), 0o000); err != nil {
			t.Fatal(err)
		}
		if granted, known := probeFullDiskAccess(denied); granted || !known {
			t.Errorf("denied file: got (%v, %v), want (false, true)", granted, known)
		}
	}
}

func TestTimezoneFromLink(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{"macOS zoneinfo", "/var/db/timezone/zoneinfo/Australia/Sydney", "Australia/Sydney"},
		{"three-part zone", "/usr/share/zoneinfo/America/Argentina/Buenos_Aires", "America/Argentina/Buenos_Aires"},
		{"UTC", "/var/db/timezone/zoneinfo/UTC", "UTC"},
		{"not a zoneinfo path", "/tmp/somewhere", ""},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := filepath.Join(dir, "localtime"+string(rune('a'+i)))
			if err := os.Symlink(tt.target, link); err != nil {
				t.Fatal(err)
			}
			if got := timezoneFromLink(link); got != tt.want {
				t.Errorf("timezoneFromLink(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}

	if got := timezoneFromLink(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing link = %q, want empty", got)
	}
}
