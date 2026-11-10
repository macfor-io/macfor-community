package orchestrator

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor/atime"
)

// readZipJSON decodes the named JSON file from the container at path into v.
func readZipJSON(t *testing.T, path, name string, v any) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer func() { _ = rc.Close() }()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := json.Unmarshal(data, v); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		return
	}
	t.Fatalf("%s not found in container", name)
}

// runCollection runs a full collection with cfg (Source and Container are
// filled in when unset) and returns the container path.
func runCollection(t *testing.T, cfg Config) (string, *Result) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	t.Cleanup(func() { _ = cont.Close() })
	if cfg.Source == nil {
		cfg.Source = &mockSource{}
	}
	cfg.Container = cont
	cfg.ToolVersion = "1.0.0"
	o, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path, result
}

func TestOrchestrator_RecordsConfiguredVolumes(t *testing.T) {
	defer registerMockPlugins()()

	vols := []atime.Volume{
		{MountPoint: "/", FSType: "apfs", Flags: []string{"read-only"}, Policy: atime.PolicyNone},
		{MountPoint: "/System/Volumes/Data", FSType: "apfs", Flags: []string{}, Policy: atime.PolicyLazy},
	}
	path, _ := runCollection(t, Config{Volumes: vols})

	var m container.Manifest
	readZipJSON(t, path, "manifest.json", &m)
	if len(m.Source.Volumes) != 2 || m.Source.Volumes[1].Policy != atime.PolicyLazy {
		t.Errorf("source.volumes = %+v, want the configured volumes", m.Source.Volumes)
	}
}
