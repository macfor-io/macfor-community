package artifacts

import (
	"errors"
	"strings"
	"testing"
)

// TestCompareVersions tests version comparison.
func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Equal versions.
		{"10.15", "10.15", 0},
		{"14.2.1", "14.2.1", 0},
		{"1", "1", 0},

		// Less than.
		{"10.14", "10.15", -1},
		{"10.15", "11.0", -1},
		{"14.1", "14.2", -1},
		{"14.2", "14.2.1", -1},
		{"1", "2", -1},

		// Greater than.
		{"10.15", "10.14", 1},
		{"11.0", "10.15", 1},
		{"14.2.1", "14.2", 1},
		{"14.3", "14.2.1", 1},
		{"2", "1", 1},

		// Versions with different component counts.
		{"10", "10.0", 0},
		{"10", "10.0.0", 0},
		{"10.0", "10.0.0", 0},

		// Beta versions (suffix stripped).
		{"14.2-beta", "14.2", 0},
		{"14.2-rc1", "14.1", 1},

		// Empty versions.
		{"", "", 0},
		{"10.15", "", 1},
		{"", "10.15", -1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := CompareVersions(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestVersionAtLeast tests the VersionAtLeast function.
func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		version, minimum string
		want             bool
	}{
		{"14.2", "14.0", true},
		{"14.2", "14.2", true},
		{"14.2", "14.3", false},
		{"11.0", "10.15", true},
		{"10.14", "10.15", false},
	}

	for _, tt := range tests {
		t.Run(tt.version+">="+tt.minimum, func(t *testing.T) {
			got := VersionAtLeast(tt.version, tt.minimum)
			if got != tt.want {
				t.Errorf("VersionAtLeast(%q, %q) = %v, want %v", tt.version, tt.minimum, got, tt.want)
			}
		})
	}
}

// TestVersionAtMost tests the VersionAtMost function.
func TestVersionAtMost(t *testing.T) {
	tests := []struct {
		version, maximum string
		want             bool
	}{
		{"14.0", "14.2", true},
		{"14.2", "14.2", true},
		{"14.3", "14.2", false},
		{"10.15", "11.0", true},
		{"10.15", "10.14", false},
	}

	for _, tt := range tests {
		t.Run(tt.version+"<="+tt.maximum, func(t *testing.T) {
			got := VersionAtMost(tt.version, tt.maximum)
			if got != tt.want {
				t.Errorf("VersionAtMost(%q, %q) = %v, want %v", tt.version, tt.maximum, got, tt.want)
			}
		})
	}
}

// TestVersionInRange tests the VersionInRange function.
func TestVersionInRange(t *testing.T) {
	tests := []struct {
		version, min, max string
		want              bool
	}{
		// In range.
		{"14.2", "14.0", "15.0", true},
		{"14.0", "14.0", "15.0", true}, // Equal to min.
		{"15.0", "14.0", "15.0", true}, // Equal to max.

		// Out of range.
		{"13.0", "14.0", "15.0", false},
		{"16.0", "14.0", "15.0", false},

		// Unbounded min.
		{"10.0", "", "15.0", true},
		{"16.0", "", "15.0", false},

		// Unbounded max.
		{"20.0", "14.0", "", true},
		{"13.0", "14.0", "", false},

		// Both unbounded.
		{"100.0", "", "", true},
	}

	for _, tt := range tests {
		name := tt.version + "_in_" + tt.min + "_" + tt.max
		t.Run(name, func(t *testing.T) {
			got := VersionInRange(tt.version, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("VersionInRange(%q, %q, %q) = %v, want %v", tt.version, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

// TestResolver_ExpandPath tests path expansion.
func TestResolver_ExpandPath(t *testing.T) {
	user := SimpleUser{
		Username: "admin",
		HomeDir:  "/Users/admin",
	}

	tests := []struct {
		pattern string
		want    string
	}{
		{"~/.zsh_history", "/Users/admin/.zsh_history"},
		{"~/Library/Safari/History.db", "/Users/admin/Library/Safari/History.db"},
		{"~", "/Users/admin"},
		{"/etc/passwd", "/etc/passwd"},
		{"/var/log/{username}.log", "/var/log/admin.log"},
		{"~/Library/{username}/data", "/Users/admin/Library/admin/data"},
	}

	resolver := NewResolver("14.2")
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := resolver.expandPath(tt.pattern, user)
			if got != tt.want {
				t.Errorf("expandPath(%q) = %q, want %q", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestResolver_WithRootPath tests disk image path remapping.
func TestResolver_WithRootPath(t *testing.T) {
	user := SimpleUser{
		Username: "admin",
		HomeDir:  "/Users/admin",
	}

	resolver := NewResolver("14.2").WithRootPath("/mnt/image")

	tests := []struct {
		pattern string
		want    string
	}{
		{"~/.zsh_history", "/mnt/image/Users/admin/.zsh_history"},
		{"/etc/passwd", "/mnt/image/etc/passwd"},
		{"~/Library/Safari/History.db", "/mnt/image/Users/admin/Library/Safari/History.db"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := resolver.expandPath(tt.pattern, user)
			if got != tt.want {
				t.Errorf("expandPath(%q) = %q, want %q", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestResolver_PathMatchesVersion tests version-aware path matching.
func TestResolver_PathMatchesVersion(t *testing.T) {
	tests := []struct {
		name      string
		osVersion string
		pathSpec  PathSpec
		want      bool
	}{
		{
			name:      "no constraints",
			osVersion: "14.2",
			pathSpec:  PathSpec{Pattern: "~/file"},
			want:      true,
		},
		{
			name:      "within os range",
			osVersion: "14.2",
			pathSpec:  PathSpec{Pattern: "~/file", OSMin: "11.0", OSMax: "15.0"},
			want:      true,
		},
		{
			name:      "below os min",
			osVersion: "10.15",
			pathSpec:  PathSpec{Pattern: "~/file", OSMin: "11.0"},
			want:      false,
		},
		{
			name:      "above os max",
			osVersion: "16.0",
			pathSpec:  PathSpec{Pattern: "~/file", OSMax: "15.0"},
			want:      false,
		},
		{
			name:      "equal to os min",
			osVersion: "11.0",
			pathSpec:  PathSpec{Pattern: "~/file", OSMin: "11.0"},
			want:      true,
		},
		{
			name:      "equal to os max",
			osVersion: "15.0",
			pathSpec:  PathSpec{Pattern: "~/file", OSMax: "15.0"},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewResolver(tt.osVersion)
			got := resolver.pathMatchesVersion(tt.pathSpec)
			if got != tt.want {
				t.Errorf("pathMatchesVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResolver_ResolveArtifact tests artifact path resolution.
func TestResolver_ResolveArtifact(t *testing.T) {
	user := SimpleUser{
		Username: "admin",
		HomeDir:  "/Users/admin",
	}

	t.Run("single path", func(t *testing.T) {
		artifact := Artifact{
			ID:     "history",
			Name:   "History",
			Paths:  []PathSpec{{Pattern: "~/Library/Safari/History.db"}},
			Format: FormatSQLite,
		}

		resolver := NewResolver("14.2")
		paths, err := resolver.ResolveArtifact(artifact, user)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(paths) != 1 {
			t.Fatalf("got %d paths, want 1", len(paths))
		}
		if paths[0].Path != "/Users/admin/Library/Safari/History.db" {
			t.Errorf("path = %q, want /Users/admin/Library/Safari/History.db", paths[0].Path)
		}
		if paths[0].Type != PathTypeFile {
			t.Errorf("type = %q, want %q", paths[0].Type, PathTypeFile)
		}
	})

	t.Run("version-specific paths", func(t *testing.T) {
		artifact := Artifact{
			ID:   "local_storage",
			Name: "Local Storage",
			Paths: []PathSpec{
				{Pattern: "~/Library/Safari/LocalStorage/*.localstorage", Type: PathTypeGlob, OSMax: "10.15"},
				{Pattern: "~/Library/WebKit/WebsiteData/LocalStorage/*.localstorage", Type: PathTypeGlob, OSMin: "11.0"},
			},
			Format: FormatSQLite,
		}

		// Test on macOS 10.15 - should get old path.
		resolver := NewResolver("10.15")
		paths, err := resolver.ResolveArtifact(artifact, user)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(paths) != 1 {
			t.Fatalf("got %d paths, want 1", len(paths))
		}
		if !strings.Contains(paths[0].Path, "Safari/LocalStorage") {
			t.Errorf("path = %q, want Safari/LocalStorage path", paths[0].Path)
		}

		// Test on macOS 14.2 - should get new path.
		resolver = NewResolver("14.2")
		paths, err = resolver.ResolveArtifact(artifact, user)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(paths) != 1 {
			t.Fatalf("got %d paths, want 1", len(paths))
		}
		if !strings.Contains(paths[0].Path, "WebKit/WebsiteData/LocalStorage") {
			t.Errorf("path = %q, want WebKit/WebsiteData/LocalStorage path", paths[0].Path)
		}
	})

	t.Run("no matching path - required", func(t *testing.T) {
		artifact := Artifact{
			ID:       "future_feature",
			Name:     "Future Feature",
			Paths:    []PathSpec{{Pattern: "~/new-feature", OSMin: "20.0"}},
			Format:   FormatText,
			Optional: false,
		}

		resolver := NewResolver("14.2")
		_, err := resolver.ResolveArtifact(artifact, user)
		if err == nil {
			t.Error("expected error for no matching path")
		}
		if !errors.Is(err, ErrNoMatchingPath) {
			t.Errorf("got error %v, want ErrNoMatchingPath", err)
		}
	})

	t.Run("no matching path - optional", func(t *testing.T) {
		artifact := Artifact{
			ID:       "future_feature",
			Name:     "Future Feature",
			Paths:    []PathSpec{{Pattern: "~/new-feature", OSMin: "20.0"}},
			Format:   FormatText,
			Optional: true,
		}

		resolver := NewResolver("14.2")
		paths, err := resolver.ResolveArtifact(artifact, user)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(paths) != 0 {
			t.Errorf("got %d paths, want 0", len(paths))
		}
	})
}

// TestResolver_ResolveRelated tests related file resolution.
func TestResolver_ResolveRelated(t *testing.T) {
	user := SimpleUser{
		Username: "admin",
		HomeDir:  "/Users/admin",
	}

	artifact := Artifact{
		ID:   "history",
		Name: "History",
		Paths: []PathSpec{
			{Pattern: "~/Library/Safari/History.db"},
		},
		Related: []string{
			"~/Library/Safari/History.db-wal",
			"~/Library/Safari/History.db-shm",
		},
		Format: FormatSQLite,
	}

	resolver := NewResolver("14.2")
	related := resolver.ResolveRelated(artifact, user)

	if len(related) != 2 {
		t.Fatalf("got %d related paths, want 2", len(related))
	}
	if related[0] != "/Users/admin/Library/Safari/History.db-wal" {
		t.Errorf("related[0] = %q, want /Users/admin/Library/Safari/History.db-wal", related[0])
	}
	if related[1] != "/Users/admin/Library/Safari/History.db-shm" {
		t.Errorf("related[1] = %q, want /Users/admin/Library/Safari/History.db-shm", related[1])
	}
}

// TestResolver_GetSchema tests schema retrieval for database artifacts.
func TestResolver_GetSchema(t *testing.T) {
	artifact := Artifact{
		ID:     "history",
		Name:   "History",
		Format: FormatSQLite,
		Schema: []SchemaSpec{
			{OSRange: [2]string{"10.12", "10.15"}, Tables: []string{"history_items", "history_visits"}, VersionID: "10"},
			{OSRange: [2]string{"11.0", ""}, Tables: []string{"history_items", "history_visits", "history_tombstones"}, VersionID: "15"},
		},
	}

	t.Run("macOS 10.15", func(t *testing.T) {
		resolver := NewResolver("10.15")
		schema := resolver.GetSchema(artifact)
		if schema == nil {
			t.Fatal("expected schema, got nil")
		}
		if schema.VersionID != "10" {
			t.Errorf("version_id = %q, want 10", schema.VersionID)
		}
		if len(schema.Tables) != 2 {
			t.Errorf("tables = %d, want 2", len(schema.Tables))
		}
	})

	t.Run("macOS 14.2", func(t *testing.T) {
		resolver := NewResolver("14.2")
		schema := resolver.GetSchema(artifact)
		if schema == nil {
			t.Fatal("expected schema, got nil")
		}
		if schema.VersionID != "15" {
			t.Errorf("version_id = %q, want 15", schema.VersionID)
		}
		if len(schema.Tables) != 3 {
			t.Errorf("tables = %d, want 3", len(schema.Tables))
		}
	})

	t.Run("macOS 10.11 (no match)", func(t *testing.T) {
		resolver := NewResolver("10.11")
		schema := resolver.GetSchema(artifact)
		if schema != nil {
			t.Errorf("expected nil schema, got %+v", schema)
		}
	})
}

// TestValidate tests manifest validation.
func TestValidate(t *testing.T) {
	t.Run("valid manifest", func(t *testing.T) {
		m := &Manifest{
			SchemaVersion: "1.0",
			Plugin: Plugin{
				ID:          "test.plugin",
				Name:        "Test Plugin",
				Description: "A test plugin",
				Version:     "1.0.0",
				Categories:  []string{"test"},
				Platforms:   []Platform{{OS: "darwin"}},
			},
			Artifacts: []Artifact{
				{
					ID:       "artifact1",
					Name:     "Artifact 1",
					Category: CategoryHistory,
					Format:   FormatText,
					Paths:    []PathSpec{{Pattern: "~/test"}},
				},
			},
		}

		err := Validate(m)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing schema version", func(t *testing.T) {
		m := &Manifest{
			Plugin: Plugin{
				ID:          "test.plugin",
				Name:        "Test",
				Description: "Test",
				Version:     "1.0.0",
				Categories:  []string{"test"},
				Platforms:   []Platform{{OS: "darwin"}},
			},
			Artifacts: []Artifact{
				{ID: "a", Name: "A", Category: CategoryHistory, Format: FormatText, Paths: []PathSpec{{Pattern: "~"}}},
			},
		}

		err := Validate(m)
		if err == nil {
			t.Error("expected error for missing schema_version")
		}
	})

	t.Run("missing plugin id", func(t *testing.T) {
		m := &Manifest{
			SchemaVersion: "1.0",
			Plugin: Plugin{
				Name:        "Test",
				Description: "Test",
				Version:     "1.0.0",
				Categories:  []string{"test"},
				Platforms:   []Platform{{OS: "darwin"}},
			},
			Artifacts: []Artifact{
				{ID: "a", Name: "A", Category: CategoryHistory, Format: FormatText, Paths: []PathSpec{{Pattern: "~"}}},
			},
		}

		err := Validate(m)
		if err == nil {
			t.Error("expected error for missing plugin.id")
		}
	})

	t.Run("duplicate artifact id", func(t *testing.T) {
		m := &Manifest{
			SchemaVersion: "1.0",
			Plugin: Plugin{
				ID:          "test.plugin",
				Name:        "Test",
				Description: "Test",
				Version:     "1.0.0",
				Categories:  []string{"test"},
				Platforms:   []Platform{{OS: "darwin"}},
			},
			Artifacts: []Artifact{
				{ID: "dup", Name: "A", Category: CategoryHistory, Format: FormatText, Paths: []PathSpec{{Pattern: "~"}}},
				{ID: "dup", Name: "B", Category: CategoryHistory, Format: FormatText, Paths: []PathSpec{{Pattern: "~"}}},
			},
		}

		err := Validate(m)
		if err == nil {
			t.Error("expected error for duplicate artifact id")
		}
		if !strings.Contains(err.Error(), "duplicate ID") {
			t.Errorf("error should mention duplicate ID: %v", err)
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		m := &Manifest{
			SchemaVersion: "1.0",
			Plugin: Plugin{
				ID:          "test.plugin",
				Name:        "Test",
				Description: "Test",
				Version:     "1.0.0",
				Categories:  []string{"test"},
				Platforms:   []Platform{{OS: "darwin"}},
			},
			Artifacts: []Artifact{
				{ID: "a", Name: "A", Category: CategoryHistory, Format: "invalid_format", Paths: []PathSpec{{Pattern: "~"}}},
			},
		}

		err := Validate(m)
		if err == nil {
			t.Error("expected error for invalid format")
		}
		if !strings.Contains(err.Error(), "invalid format") {
			t.Errorf("error should mention invalid format: %v", err)
		}
	})

	t.Run("invalid category", func(t *testing.T) {
		m := &Manifest{
			SchemaVersion: "1.0",
			Plugin: Plugin{
				ID:          "test.plugin",
				Name:        "Test",
				Description: "Test",
				Version:     "1.0.0",
				Categories:  []string{"test"},
				Platforms:   []Platform{{OS: "darwin"}},
			},
			Artifacts: []Artifact{
				{ID: "a", Name: "A", Category: "invalid_category", Format: FormatText, Paths: []PathSpec{{Pattern: "~"}}},
			},
		}

		err := Validate(m)
		if err == nil {
			t.Error("expected error for invalid category")
		}
		if !strings.Contains(err.Error(), "invalid category") {
			t.Errorf("error should mention invalid category: %v", err)
		}
	})

	t.Run("invalid path type", func(t *testing.T) {
		m := &Manifest{
			SchemaVersion: "1.0",
			Plugin: Plugin{
				ID:          "test.plugin",
				Name:        "Test",
				Description: "Test",
				Version:     "1.0.0",
				Categories:  []string{"test"},
				Platforms:   []Platform{{OS: "darwin"}},
			},
			Artifacts: []Artifact{
				{ID: "a", Name: "A", Category: CategoryHistory, Format: FormatText, Paths: []PathSpec{{Pattern: "~", Type: "invalid_type"}}},
			},
		}

		err := Validate(m)
		if err == nil {
			t.Error("expected error for invalid path type")
		}
		if !strings.Contains(err.Error(), "invalid type") {
			t.Errorf("error should mention invalid type: %v", err)
		}
	})

	t.Run("no artifacts", func(t *testing.T) {
		m := &Manifest{
			SchemaVersion: "1.0",
			Plugin: Plugin{
				ID:          "test.plugin",
				Name:        "Test",
				Description: "Test",
				Version:     "1.0.0",
				Categories:  []string{"test"},
				Platforms:   []Platform{{OS: "darwin"}},
			},
			Artifacts: []Artifact{},
		}

		err := Validate(m)
		if err == nil {
			t.Error("expected error for no artifacts")
		}
	})

	t.Run("no paths in artifact", func(t *testing.T) {
		m := &Manifest{
			SchemaVersion: "1.0",
			Plugin: Plugin{
				ID:          "test.plugin",
				Name:        "Test",
				Description: "Test",
				Version:     "1.0.0",
				Categories:  []string{"test"},
				Platforms:   []Platform{{OS: "darwin"}},
			},
			Artifacts: []Artifact{
				{ID: "a", Name: "A", Category: CategoryHistory, Format: FormatText, Paths: []PathSpec{}},
			},
		}

		err := Validate(m)
		if err == nil {
			t.Error("expected error for no paths")
		}
	})
}

// TestParse tests YAML parsing.
func TestParse(t *testing.T) {
	yamlData := `
schema_version: "1.0"
plugin:
  id: shell.history
  name: Shell History
  description: Collects shell command history
  version: "1.0.0"
  categories:
    - user
    - command
  platforms:
    - os: darwin
      min_version: "10.12"
artifacts:
  - id: zsh_history
    name: Zsh History
    category: command_history
    format: text
    optional: true
    paths:
      - pattern: "~/.zsh_history"
        type: file
    metadata:
      timestamp_format: extended
`

	m, err := Parse([]byte(yamlData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.SchemaVersion != "1.0" {
		t.Errorf("schema_version = %q, want 1.0", m.SchemaVersion)
	}
	if m.Plugin.ID != "shell.history" {
		t.Errorf("plugin.id = %q, want shell.history", m.Plugin.ID)
	}
	if len(m.Plugin.Categories) != 2 {
		t.Errorf("plugin.categories = %d, want 2", len(m.Plugin.Categories))
	}
	if len(m.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(m.Artifacts))
	}

	a := m.Artifacts[0]
	if a.ID != "zsh_history" {
		t.Errorf("artifact.id = %q, want zsh_history", a.ID)
	}
	if !a.Optional {
		t.Error("artifact.optional = false, want true")
	}
	if a.Paths[0].Type != PathTypeFile {
		t.Errorf("artifact.paths[0].type = %q, want file", a.Paths[0].Type)
	}
	if a.Metadata["timestamp_format"] != "extended" {
		t.Errorf("metadata[timestamp_format] = %q, want extended", a.Metadata["timestamp_format"])
	}
}

// TestParse_Invalid tests parsing of invalid YAML.
func TestParse_Invalid(t *testing.T) {
	t.Run("invalid yaml", func(t *testing.T) {
		_, err := Parse([]byte("not: valid: yaml: syntax"))
		if err == nil {
			t.Error("expected error for invalid yaml")
		}
	})

	t.Run("valid yaml but invalid manifest", func(t *testing.T) {
		_, err := Parse([]byte("schema_version: '1.0'"))
		if err == nil {
			t.Error("expected error for invalid manifest")
		}
	})
}

// TestMustParse tests the panic behavior.
func TestMustParse(t *testing.T) {
	yamlData := `
schema_version: "1.0"
plugin:
  id: test.plugin
  name: Test
  description: Test
  version: "1.0.0"
  categories: [test]
  platforms: [{os: darwin}]
artifacts:
  - id: a
    name: A
    category: command_history
    format: text
    paths: [{pattern: "~"}]
`

	// Should not panic.
	m := MustParse([]byte(yamlData))
	if m.Plugin.ID != "test.plugin" {
		t.Errorf("plugin.id = %q, want test.plugin", m.Plugin.ID)
	}
}

// TestMustParse_Panic tests that MustParse panics on invalid input.
func TestMustParse_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid yaml")
		}
	}()

	MustParse([]byte("invalid yaml: {"))
}

// TestGetArtifactByID tests artifact lookup by ID.
func TestGetArtifactByID(t *testing.T) {
	m := &Manifest{
		Artifacts: []Artifact{
			{ID: "first", Name: "First"},
			{ID: "second", Name: "Second"},
			{ID: "third", Name: "Third"},
		},
	}

	t.Run("found", func(t *testing.T) {
		a := GetArtifactByID(m, "second")
		if a == nil {
			t.Fatal("expected artifact, got nil")
		}
		if a.Name != "Second" {
			t.Errorf("name = %q, want Second", a.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		a := GetArtifactByID(m, "nonexistent")
		if a != nil {
			t.Errorf("expected nil, got %+v", a)
		}
	})
}

// TestGetArtifactsByCategory tests artifact filtering by category.
func TestGetArtifactsByCategory(t *testing.T) {
	m := &Manifest{
		Artifacts: []Artifact{
			{ID: "a", Category: CategoryHistory},
			{ID: "b", Category: CategoryHistory},
			{ID: "c", Category: CategoryBookmarks},
		},
	}

	history := GetArtifactsByCategory(m, CategoryHistory)
	if len(history) != 2 {
		t.Errorf("got %d history artifacts, want 2", len(history))
	}

	bookmarks := GetArtifactsByCategory(m, CategoryBookmarks)
	if len(bookmarks) != 1 {
		t.Errorf("got %d bookmarks artifacts, want 1", len(bookmarks))
	}

	cookies := GetArtifactsByCategory(m, CategoryCookies)
	if len(cookies) != 0 {
		t.Errorf("got %d cookies artifacts, want 0", len(cookies))
	}
}

// TestGetArtifactsByFormat tests artifact filtering by format.
func TestGetArtifactsByFormat(t *testing.T) {
	m := &Manifest{
		Artifacts: []Artifact{
			{ID: "a", Format: FormatSQLite},
			{ID: "b", Format: FormatSQLite},
			{ID: "c", Format: FormatBinaryPlist},
		},
	}

	sqlite := GetArtifactsByFormat(m, FormatSQLite)
	if len(sqlite) != 2 {
		t.Errorf("got %d sqlite artifacts, want 2", len(sqlite))
	}

	plist := GetArtifactsByFormat(m, FormatBinaryPlist)
	if len(plist) != 1 {
		t.Errorf("got %d plist artifacts, want 1", len(plist))
	}
}

// TestGetOptionalArtifacts tests filtering optional artifacts.
func TestGetOptionalArtifacts(t *testing.T) {
	m := &Manifest{
		Artifacts: []Artifact{
			{ID: "a", Optional: true},
			{ID: "b", Optional: false},
			{ID: "c", Optional: true},
		},
	}

	optional := GetOptionalArtifacts(m)
	if len(optional) != 2 {
		t.Errorf("got %d optional artifacts, want 2", len(optional))
	}
}

// TestGetRequiredArtifacts tests filtering required artifacts.
func TestGetRequiredArtifacts(t *testing.T) {
	m := &Manifest{
		Artifacts: []Artifact{
			{ID: "a", Optional: true},
			{ID: "b", Optional: false},
			{ID: "c", Optional: false},
		},
	}

	required := GetRequiredArtifacts(m)
	if len(required) != 2 {
		t.Errorf("got %d required artifacts, want 2", len(required))
	}
}

// TestGetRootRequiredArtifacts tests filtering root-required artifacts.
func TestGetRootRequiredArtifacts(t *testing.T) {
	m := &Manifest{
		Artifacts: []Artifact{
			{ID: "a", RequiresRoot: true},
			{ID: "b", RequiresRoot: false},
			{ID: "c", RequiresRoot: true},
		},
	}

	root := GetRootRequiredArtifacts(m)
	if len(root) != 2 {
		t.Errorf("got %d root-required artifacts, want 2", len(root))
	}
}

// TestValidFormats tests the ValidFormats function.
func TestValidFormats(t *testing.T) {
	formats := ValidFormats()
	if len(formats) != 12 {
		t.Errorf("got %d formats, want 12", len(formats))
	}

	// Check that all constants are included.
	expected := map[string]bool{
		FormatSQLite:        true,
		FormatBinaryPlist:   true,
		FormatXMLPlist:      true,
		FormatText:          true,
		FormatYAML:          true,
		FormatJSON:          true,
		FormatBinary:        true,
		FormatBinaryCookies: true,
		FormatDirectory:     true,
		FormatLevelDB:       true,
		FormatSNSS:          true,
		FormatJSONL:         true,
	}

	for _, f := range formats {
		if !expected[f] {
			t.Errorf("unexpected format: %s", f)
		}
	}
}

// TestValidCategories tests the ValidCategories function.
func TestValidCategories(t *testing.T) {
	categories := ValidCategories()
	if len(categories) != 20 {
		t.Errorf("got %d categories, want 20", len(categories))
	}
}

// TestValidPathTypes tests the ValidPathTypes function.
func TestValidPathTypes(t *testing.T) {
	types := ValidPathTypes()
	if len(types) != 3 {
		t.Errorf("got %d path types, want 3", len(types))
	}
}
