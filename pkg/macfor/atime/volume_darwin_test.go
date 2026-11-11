//go:build darwin

package atime

import (
	"slices"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/macfor-io/macfor-community/sources/directory"
)

func TestPolicyFromFlags(t *testing.T) {
	tests := []struct {
		name  string
		flags uint32
		want  Policy
	}{
		{"default", 0, PolicyLazy},
		{"journaled local", unix.MNT_JOURNALED | unix.MNT_LOCAL, PolicyLazy},
		{"read-only", unix.MNT_RDONLY, PolicyNone},
		{"noatime", unix.MNT_NOATIME, PolicyNone},
		{"strictatime", unix.MNT_STRICTATIME, PolicyStrict},
		{"read-only wins over strictatime", unix.MNT_RDONLY | unix.MNT_STRICTATIME, PolicyNone},
		{"noatime wins over strictatime", unix.MNT_NOATIME | unix.MNT_STRICTATIME, PolicyNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PolicyFromFlags(tt.flags); got != tt.want {
				t.Errorf("PolicyFromFlags(%#x) = %q, want %q", tt.flags, got, tt.want)
			}
		})
	}
}

func TestFlagNames(t *testing.T) {
	got := flagNames(unix.MNT_RDONLY | unix.MNT_NOATIME | unix.MNT_STRICTATIME | unix.MNT_SNAPSHOT | unix.MNT_JOURNALED)
	want := []string{"read-only", "noatime", "strictatime", "snapshot"}
	if !slices.Equal(got, want) {
		t.Errorf("flagNames = %v, want %v", got, want)
	}
	if got := flagNames(0); got == nil || len(got) != 0 {
		t.Errorf("flagNames(0) = %#v, want empty non-nil slice", got)
	}
}

func TestVolumeForSealedSystemVolume(t *testing.T) {
	v, err := VolumeFor("/")
	if err != nil {
		t.Fatalf("VolumeFor(/): %v", err)
	}
	if v.MountPoint != "/" {
		t.Errorf("MountPoint = %q, want /", v.MountPoint)
	}
	if v.FSType == "" || v.Device == "" {
		t.Errorf("FSType %q / Device %q should be set", v.FSType, v.Device)
	}
	// The macOS 11+ System volume is sealed and mounted read-only.
	if v.Policy != PolicyNone {
		t.Errorf("Policy = %q, want %q (sealed read-only System volume)", v.Policy, PolicyNone)
	}
}

func TestVolumeForDeduplicates(t *testing.T) {
	dir := t.TempDir()
	a, err := VolumeFor(dir)
	if err != nil {
		t.Fatalf("VolumeFor: %v", err)
	}
	b, err := VolumeFor(dir + "/../")
	if err != nil {
		t.Fatalf("VolumeFor: %v", err)
	}
	if a.fsid != b.fsid || a.MountPoint != b.MountPoint {
		t.Errorf("same volume probed differently: %+v vs %+v", a, b)
	}
	vols := probePaths([]string{dir, dir, "/nonexistent-macfor-path"})
	if len(vols) != 1 {
		t.Errorf("probePaths = %d volumes, want 1 (deduplicated, unprobeable skipped)", len(vols))
	}
}

func TestProbeSourceRooted(t *testing.T) {
	src, err := directory.New(t.TempDir())
	if err != nil {
		t.Fatalf("directory.New: %v", err)
	}
	vols := ProbeSource(src)
	if len(vols) != 1 {
		t.Fatalf("ProbeSource = %d volumes, want 1", len(vols))
	}
	if vols[0].Policy == PolicyUnknown {
		t.Errorf("Policy = unknown, want a probed policy")
	}
}
