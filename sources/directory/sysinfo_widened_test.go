package directory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/testdata/macostree"
)

// writeLocaltimeLink creates root/private/etc/localtime as a symlink to target.
// The target is never created, so a test passes only if it is not followed.
func writeLocaltimeLink(t *testing.T, root, target string) {
	t.Helper()
	dir := filepath.Join(root, "private", "etc")
	mustMkdir(t, dir)
	if err := os.Symlink(target, filepath.Join(dir, "localtime")); err != nil {
		t.Fatalf("symlink localtime: %v", err)
	}
}

// writeGlobalPreferences creates Library/Preferences/.GlobalPreferences.plist
// with the given selected-city time zone name.
func writeGlobalPreferences(t *testing.T, root, zone string) {
	t.Helper()
	content := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>com.apple.preferences.timezone.selected_city</key>
	<dict>
		<key>Name</key>
		<string>Some City</string>
		<key>TimeZoneName</key>
		<string>` + zone + `</string>
	</dict>
</dict>
</plist>`
	mustWrite(t, filepath.Join(root, "Library", "Preferences", ".GlobalPreferences.plist"), content)
}

// systemInfo builds a Source over roots and returns its SystemInfo.
func systemInfo(t *testing.T, roots ...string) *macfor.SystemInfo {
	t.Helper()
	src, err := NewLayered(roots...)
	if err != nil {
		t.Fatalf("NewLayered(%v) error = %v", roots, err)
	}
	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info == nil {
		t.Fatal("SystemInfo() returned nil")
	}
	return info
}

func TestSystemInfo_Timezone(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		want  string
	}{
		{
			name: "absolute symlink not followed",
			setup: func(t *testing.T, root string) {
				// The target does not exist on the host; reading it would fail.
				writeLocaltimeLink(t, root, "/var/db/timezone/zoneinfo/Pacific/Chatham")
			},
			want: "Pacific/Chatham",
		},
		{
			name: "relative symlink",
			setup: func(t *testing.T, root string) {
				writeLocaltimeLink(t, root, "../usr/share/zoneinfo/Europe/London")
			},
			want: "Europe/London",
		},
		{
			name: "three-component zone",
			setup: func(t *testing.T, root string) {
				writeLocaltimeLink(t, root, "/usr/share/zoneinfo/America/Argentina/Buenos_Aires")
			},
			want: "America/Argentina/Buenos_Aires",
		},
		{
			name: "symlink takes precedence over global preferences",
			setup: func(t *testing.T, root string) {
				writeLocaltimeLink(t, root, "/var/db/timezone/zoneinfo/Asia/Tokyo")
				writeGlobalPreferences(t, root, "Europe/Paris")
			},
			want: "Asia/Tokyo",
		},
		{
			name: "global preferences fallback",
			setup: func(t *testing.T, root string) {
				writeGlobalPreferences(t, root, "America/New_York")
			},
			want: "America/New_York",
		},
		{
			name: "implausible link target falls back to global preferences",
			setup: func(t *testing.T, root string) {
				writeLocaltimeLink(t, root, "/var/db/timezone/zoneinfo/../../../etc/passwd")
				writeGlobalPreferences(t, root, "Europe/Berlin")
			},
			want: "Europe/Berlin",
		},
		{
			name: "link without zoneinfo segment",
			setup: func(t *testing.T, root string) {
				writeLocaltimeLink(t, root, "/somewhere/else/Australia/Sydney")
			},
			want: unknownValue,
		},
		{
			name: "regular file is not a symlink",
			setup: func(t *testing.T, root string) {
				mustWrite(t, filepath.Join(root, "private", "etc", "localtime"), "TZif2")
			},
			want: unknownValue,
		},
		{
			name: "implausible global preferences name",
			setup: func(t *testing.T, root string) {
				writeGlobalPreferences(t, root, "../../etc/passwd")
			},
			want: unknownValue,
		},
		{
			name:  "neither source present",
			setup: func(*testing.T, string) {},
			want:  unknownValue,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := macostree.MustBuild(t, macostree.Defaults())
			tt.setup(t, root)

			info := systemInfo(t, root)
			if info.Timezone != tt.want {
				t.Errorf("Timezone = %q, want %q", info.Timezone, tt.want)
			}
			if info.TimezoneOffset != "" {
				t.Errorf("TimezoneOffset = %q, want empty (not derivable without host tzdata)", info.TimezoneOffset)
			}
		})
	}
}

func TestSystemInfo_Timezone_Layered(t *testing.T) {
	tree := buildLayeredTree(t)
	// The System root has private/etc but no localtime; the link lives on Data.
	mustMkdir(t, filepath.Join(tree.system, "private", "etc"))
	writeLocaltimeLink(t, tree.data, "/var/db/timezone/zoneinfo/Pacific/Chatham")

	info := systemInfo(t, tree.system, tree.data)
	if info.Timezone != "Pacific/Chatham" {
		t.Errorf("Timezone = %q, want %q", info.Timezone, "Pacific/Chatham")
	}
}

func TestSystemInfo_Timezone_LayeredGlobalPreferences(t *testing.T) {
	tree := buildLayeredTree(t)
	writeGlobalPreferences(t, tree.data, "Australia/Perth")

	info := systemInfo(t, tree.system, tree.data)
	if info.Timezone != "Australia/Perth" {
		t.Errorf("Timezone = %q, want %q", info.Timezone, "Australia/Perth")
	}
}

func TestSystemInfo_Architecture(t *testing.T) {
	tests := []struct {
		name  string
		model string // Empty means no platform plist.
		want  string
	}{
		{"Apple silicon M2", "Mac14,2", "arm64"},
		{"Apple silicon M1 Max", "MacBookPro18,2", "arm64"},
		{"Apple silicon M4 Pro", "Mac16,7", "arm64"},
		{"Intel", "MacBookPro16,1", "amd64"},
		{"Intel 2012", "MacBookPro9,2", "amd64"},
		{"unknown model", "MacFuture99,1", unknownValue},
		{"absent platform plist", "", unknownValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := macostree.MustBuild(t, macostree.Defaults())
			if tt.model != "" {
				writePlatformPlist(t, root, tt.model)
			}
			info := systemInfo(t, root)
			if info.Architecture != tt.want {
				t.Errorf("Architecture = %q, want %q", info.Architecture, tt.want)
			}
		})
	}
}

// TestSystemInfo_HostStateNotDerived checks that runtime host state is never
// populated for a directory tree, even when every derivable field is present.
func TestSystemInfo_HostStateNotDerived(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Defaults())
	writePlatformPlist(t, root, "Mac14,2")
	writeLocaltimeLink(t, root, "/var/db/timezone/zoneinfo/Pacific/Chatham")

	info := systemInfo(t, root)
	if !info.BootTime.IsZero() {
		t.Errorf("BootTime = %v, want zero", info.BootTime)
	}
	if info.UptimeSeconds != 0 {
		t.Errorf("UptimeSeconds = %d, want 0", info.UptimeSeconds)
	}
	if info.SIPEnabled || info.FileVaultEnabled || info.GatekeeperEnabled {
		t.Errorf("security flags = SIP %v, FileVault %v, Gatekeeper %v; want all false",
			info.SIPEnabled, info.FileVaultEnabled, info.GatekeeperEnabled)
	}
}

func TestZoneFromLinkTarget(t *testing.T) {
	tests := map[string]string{
		"/var/db/timezone/zoneinfo/Australia/Sydney": "Australia/Sydney",
		"/usr/share/zoneinfo/UTC":                    "UTC",
		"/usr/share/zoneinfo/Etc/GMT+10":             "Etc/GMT+10",
		"zoneinfo/Europe/London":                     "Europe/London",
		"/usr/share/zoneinfo/":                       "",
		"/usr/share/zoneinfo/Europe/../London":       "",
		"/usr/share/notzoneinfo/Europe/London":       "",
		"/usr/share/zoneinfo/Europe//London":         "",
		"/etc/localtime":                             "",
		"":                                           "",
	}
	for target, want := range tests {
		if got := zoneFromLinkTarget(target); got != want {
			t.Errorf("zoneFromLinkTarget(%q) = %q, want %q", target, got, want)
		}
	}
}

// TestSystemInfoCollectionHostFields checks that the collection fields describe
// the host running macfor rather than anything in the collected tree.
func TestSystemInfoCollectionHostFields(t *testing.T) {
	src, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := src.SystemInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.CollectionUser != os.Getenv("USER") {
		t.Errorf("CollectionUser = %q, want %q", info.CollectionUser, os.Getenv("USER"))
	}
	if info.CollectionUID != os.Getuid() || info.IsRoot != (os.Getuid() == 0) {
		t.Errorf("CollectionUID/IsRoot = %d/%v, want %d/%v", info.CollectionUID, info.IsRoot, os.Getuid(), os.Getuid() == 0)
	}
}
