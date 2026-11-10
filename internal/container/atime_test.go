package container

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor/atime"
)

func TestSetVolumesWritesSourceVolumes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.zip")
	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0", SourceType: "live"})
	c.SetVolumes([]atime.Volume{
		{MountPoint: "/", FSType: "apfs", Device: "/dev/disk3s1s1", Flags: []string{"read-only"}, Policy: atime.PolicyNone},
		{MountPoint: "/System/Volumes/Data", FSType: "apfs", Device: "/dev/disk3s5", Flags: []string{}, Policy: atime.PolicyLazy},
	})
	if err := c.Finalize(); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	raw := readFromZip(t, path, "manifest.json")
	if !strings.Contains(raw, `"atime_policy": "lazy"`) {
		t.Errorf("manifest.json missing atime_policy field:\n%s", raw)
	}
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.Version != "1.5" {
		t.Errorf("version = %q, want 1.5", m.Version)
	}
	if len(m.Source.Volumes) != 2 {
		t.Fatalf("source.volumes has %d entries, want 2", len(m.Source.Volumes))
	}
	data := m.Source.Volumes[1]
	if data.MountPoint != "/System/Volumes/Data" || data.Policy != atime.PolicyLazy || data.Device != "/dev/disk3s5" {
		t.Errorf("source.volumes[1] = %+v", data)
	}
}

func TestNoVolumesOmitsSourceVolumes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.zip")
	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0", SourceType: "directory"})
	if err := c.Finalize(); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if raw := readFromZip(t, path, "manifest.json"); strings.Contains(raw, `"volumes"`) {
		t.Errorf("manifest.json should omit source.volumes when none were set:\n%s", raw)
	}
}
