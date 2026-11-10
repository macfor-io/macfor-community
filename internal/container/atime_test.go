package container

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestAccessTimeChangeAndSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.zip")
	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0", SourceType: "live"})

	t0 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	before := atime.Stat{Dev: 16777232, Ino: 42, Mode: "-rw-r--r--", Size: 8, Atime: t0, Mtime: t0, Ctime: t0}
	after := before
	after.Atime = t1
	c.AddAccessTimeChange(atime.Event{
		ObservedAt:   t1,
		PluginID:     "browser.safari",
		Phase:        atime.PhaseCollect,
		Operation:    atime.OperationRead,
		Path:         "~/Library/Safari/History.db",
		ResolvedPath: "/Users/alice/Library/Safari/History.db",
		Attribution:  atime.AttributionLikelyCollector,
		Before:       before,
		After:        after,
	})
	c.SetAccessTimes(&atime.Summary{
		ObservedReads: 10, Modified: 1, LikelyCollector: 1,
		Inodes:     []atime.InodeChange{{Dev: 16777232, Ino: 42, Path: "~/Library/Safari/History.db", Changes: 1, FirstBefore: before, LastAfter: after}},
		Unobserved: []string{"external commands"},
	})
	if err := c.Finalize(); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var custody ChainOfCustodyLog
	if err := json.Unmarshal([]byte(readFromZip(t, path, "chain-of-custody.json")), &custody); err != nil {
		t.Fatalf("unmarshal custody: %v", err)
	}
	var found *ChainOfCustodyEntry
	for i := range custody.Entries {
		if custody.Entries[i].Action == CustodyActionAccessTimeModified {
			found = &custody.Entries[i]
		}
	}
	if found == nil {
		t.Fatal("no access_time_modified custody entry")
	}
	if found.Target != "~/Library/Safari/History.db" || !found.Timestamp.Equal(t1) {
		t.Errorf("entry target/time = %q / %v", found.Target, found.Timestamp)
	}
	for _, k := range []string{"plugin", "phase", "operation", "attribution", "before", "after", "resolved_path"} {
		if _, ok := found.Details[k]; !ok {
			t.Errorf("custody details missing %q: %v", k, found.Details)
		}
	}
	b, _ := found.Details["before"].(map[string]any)
	if b["atime"] != "2020-01-01T00:00:00Z" || b["ino"] != float64(42) {
		t.Errorf("before stat = %v", b)
	}

	var m Manifest
	if err := json.Unmarshal([]byte(readFromZip(t, path, "manifest.json")), &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.AccessTimes == nil || m.AccessTimes.Modified != 1 || m.AccessTimes.ObservedReads != 10 || len(m.AccessTimes.Inodes) != 1 {
		t.Errorf("manifest access_times = %+v", m.AccessTimes)
	}
}
