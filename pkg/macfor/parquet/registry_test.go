package parquet_test

import (
	"reflect"
	"sync"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor/parquet"
)

type testRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func newTestRegistry() *parquet.SchemaRegistry {
	// Use a fresh registry for each test (not the global one).
	return parquet.NewSchemaRegistry()
}

func TestSchemaRegistry_RegisterAndLookup(t *testing.T) {
	r := newTestRegistry()

	entry := parquet.SchemaEntry{
		PluginID:     "browser.safari",
		ArtifactType: "history",
		Version:      parquet.SchemaVersion{Major: 1, Minor: 0},
		Checksum:     "sha256:abc123",
		GoType:       reflect.TypeOf(testRecord{}),
	}

	r.Register(entry)

	got := r.Lookup("browser.safari", "history")
	if got == nil {
		t.Fatal("Lookup returned nil for registered entry")
	}
	if got.PluginID != "browser.safari" {
		t.Errorf("PluginID = %q, want %q", got.PluginID, "browser.safari")
	}
	if got.Version.String() != "1.0" {
		t.Errorf("Version = %q, want %q", got.Version.String(), "1.0")
	}
	if got.Checksum != "sha256:abc123" {
		t.Errorf("Checksum = %q, want %q", got.Checksum, "sha256:abc123")
	}
}

func TestSchemaRegistry_LookupMissing(t *testing.T) {
	r := newTestRegistry()

	got := r.Lookup("nonexistent", "plugin")
	if got != nil {
		t.Errorf("Lookup returned %v for missing entry, want nil", got)
	}
}

func TestSchemaRegistry_RegisterOverwrites(t *testing.T) {
	r := newTestRegistry()

	r.Register(parquet.SchemaEntry{
		PluginID:     "browser.safari",
		ArtifactType: "history",
		Version:      parquet.SchemaVersion{Major: 1, Minor: 0},
		Checksum:     "sha256:old",
	})

	r.Register(parquet.SchemaEntry{
		PluginID:     "browser.safari",
		ArtifactType: "history",
		Version:      parquet.SchemaVersion{Major: 2, Minor: 0},
		Checksum:     "sha256:new",
	})

	got := r.Lookup("browser.safari", "history")
	if got == nil {
		t.Fatal("Lookup returned nil")
	}
	if got.Version.Major != 2 {
		t.Errorf("Version.Major = %d, want 2", got.Version.Major)
	}
	if got.Checksum != "sha256:new" {
		t.Errorf("Checksum = %q, want %q", got.Checksum, "sha256:new")
	}
}

func TestSchemaRegistry_Entries(t *testing.T) {
	r := newTestRegistry()

	r.Register(parquet.SchemaEntry{
		PluginID:     "shell.history",
		ArtifactType: "history",
		Version:      parquet.SchemaVersion{Major: 1, Minor: 0},
	})
	r.Register(parquet.SchemaEntry{
		PluginID:     "browser.safari",
		ArtifactType: "cookies",
		Version:      parquet.SchemaVersion{Major: 1, Minor: 1},
	})
	r.Register(parquet.SchemaEntry{
		PluginID:     "browser.safari",
		ArtifactType: "history",
		Version:      parquet.SchemaVersion{Major: 1, Minor: 0},
	})

	entries := r.Entries()
	if len(entries) != 3 {
		t.Fatalf("Entries() returned %d entries, want 3", len(entries))
	}

	// Entries should be sorted by key.
	if entries[0].PluginID != "browser.safari" || entries[0].ArtifactType != "cookies" {
		t.Errorf("first entry = %s:%s, want browser.safari:cookies", entries[0].PluginID, entries[0].ArtifactType)
	}
}

func TestSchemaRegistry_Len(t *testing.T) {
	r := newTestRegistry()

	if r.Len() != 0 {
		t.Errorf("Len() = %d, want 0", r.Len())
	}

	r.Register(parquet.SchemaEntry{PluginID: "a", ArtifactType: "b"})
	if r.Len() != 1 {
		t.Errorf("Len() = %d, want 1", r.Len())
	}
}

func TestSchemaRegistry_ConcurrentAccess(t *testing.T) {
	r := newTestRegistry()
	var wg sync.WaitGroup

	// Concurrent writes.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Register(parquet.SchemaEntry{
				PluginID:     "plugin",
				ArtifactType: "type",
				Version:      parquet.SchemaVersion{Major: i, Minor: 0},
			})
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Lookup("plugin", "type")
			_ = r.Entries()
		}()
	}

	wg.Wait()

	// Should have exactly one entry (all writes to same key).
	if r.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after concurrent access", r.Len())
	}
}

func TestSchemaVersion_String(t *testing.T) {
	tests := []struct {
		v    parquet.SchemaVersion
		want string
	}{
		{parquet.SchemaVersion{Major: 1, Minor: 0}, "1.0"},
		{parquet.SchemaVersion{Major: 2, Minor: 3}, "2.3"},
		{parquet.SchemaVersion{Major: 0, Minor: 1}, "0.1"},
	}

	for _, tt := range tests {
		got := tt.v.String()
		if got != tt.want {
			t.Errorf("SchemaVersion{%d,%d}.String() = %q, want %q", tt.v.Major, tt.v.Minor, got, tt.want)
		}
	}
}

func TestGlobalRegistry(t *testing.T) {
	r := parquet.GlobalRegistry()
	if r == nil {
		t.Fatal("GlobalRegistry() returned nil")
	}
	// Same instance each call.
	r2 := parquet.GlobalRegistry()
	if r != r2 {
		t.Error("GlobalRegistry() should return the same instance")
	}
}
