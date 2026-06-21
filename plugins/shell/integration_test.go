package shell

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/sources/live"
)

const darwinOS = "darwin"

// Test constants.
const testUser = "testuser"

// Integration tests that verify the shell plugin works with real components.
// These tests use the live source and test fixtures.

func TestIntegration_PluginInfo(t *testing.T) {
	p := &Plugin{}

	info := p.Info()

	if info.ID != PluginID {
		t.Errorf("expected ID %q, got %q", PluginID, info.ID)
	}

	if info.Name != "Shell History" {
		t.Errorf("expected name 'Shell History', got %q", info.Name)
	}

	if info.Version == "" {
		t.Error("version should not be empty")
	}

	// Check categories
	expectedCategories := []string{"user", "command", "shell"}
	for _, expected := range expectedCategories {
		found := false
		for _, cat := range info.Categories {
			if cat == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing category: %s", expected)
		}
	}

	// Check platform spec
	if len(info.Platforms) == 0 {
		t.Error("expected at least one platform spec")
	}
	if info.Platforms[0].OS != "darwin" {
		t.Errorf("expected platform darwin, got %s", info.Platforms[0].OS)
	}
}

func TestIntegration_PluginRegistration(t *testing.T) {
	// Plugin should be auto-registered via init()
	p, ok := macfor.ByID(PluginID)
	if !ok {
		t.Fatal("shell.history plugin not registered")
	}

	if p.Info().ID != PluginID {
		t.Errorf("registered plugin ID mismatch: got %s", p.Info().ID)
	}
}

func TestIntegration_DetectWithLiveSource(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	// Log detection result
	t.Logf("Detection result: applicable=%v, confidence=%.2f, reason=%s",
		result.Applicable, result.Confidence, result.Reason)

	// OS version should be set
	if result.OSVersion == "" {
		t.Error("OSVersion should be set")
	}

	// Metadata should be populated
	if result.Metadata == nil {
		t.Error("Metadata should not be nil")
	}

	if result.Applicable {
		// If applicable, should have shells detected
		shells, ok := result.Metadata["shells_detected"]
		if !ok {
			t.Error("missing shells_detected in metadata")
		} else {
			t.Logf("Detected shells: %v", shells)
		}

		users, ok := result.Metadata["users_with_history"]
		if !ok {
			t.Error("missing users_with_history in metadata")
		} else {
			t.Logf("Users with history: %v", users)
		}

		if result.Confidence != 1.0 {
			t.Errorf("expected confidence 1.0, got %.2f", result.Confidence)
		}
	} else {
		t.Log("Plugin not applicable on this system (no shell history files found)")
	}
}

func TestIntegration_CollectWithLiveSource(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	p := &Plugin{}

	// First check if applicable
	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	if !result.Applicable {
		t.Skip("shell.history not applicable on this system")
	}

	// Create container for collection
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "evidence.zip")

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	cont.SetCollectionMetadata(container.CollectionMeta{
		ToolVersion: "1.0.0-test",
		SourceType:  "live",
	})

	// Create collector
	coll := container.NewCollector(cont, PluginID)

	// Run collection
	err = p.Collect(context.Background(), src, coll)

	// Collection may return errors for inaccessible files, but shouldn't fail completely
	if err != nil {
		t.Logf("Collection returned non-fatal errors: %v", err)
	}

	// Finalize collector
	summary, err := coll.Finalize()
	if err != nil {
		t.Fatalf("failed to finalize collector: %v", err)
	}

	cont.AddPluginSummary(summary)

	if err := cont.Finalize(); err != nil {
		t.Fatalf("failed to finalize container: %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Log collection statistics
	t.Logf("Collection summary: files=%d, records=%d, errors=%d",
		summary.FilesCollected, summary.RecordsCreated, summary.ErrorsEncountered)

	// Verify container exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("container was not created")
	}
}

func TestIntegration_CollectionCancellation(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	p := &Plugin{}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "evidence.zip")

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	coll := container.NewCollector(cont, PluginID)

	// Collection should return context cancelled error
	err = p.Collect(ctx, src, coll)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}

	// Cleanup
	cont.MarkError()
	_ = cont.Close()
}

func TestIntegration_ParseBashFixtures(t *testing.T) {
	fixtures := []struct {
		name          string
		expectedCmds  int
		hasTimestamps bool
	}{
		{FixtureBashBasic, 5, false},
		{FixtureBashTimestamped, 5, true},
		{FixtureBashMixed, 5, false}, // Mixed has some timestamps
		{FixtureBashUnicode, 5, false},
		{FixtureBashSpecial, 10, false},
		{FixtureBashEmpty, 0, false},
	}

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			content, err := loadFixture(tt.name)
			if err != nil {
				t.Fatalf("failed to load fixture: %v", err)
			}

			records, warnings := parseBashHistory(content, testUser, tt.name)

			t.Logf("%s: parsed %d records, %d warnings", tt.name, len(records), len(warnings))

			if tt.expectedCmds > 0 && len(records) == 0 {
				t.Error("expected at least some records")
			}

			// Check record structure
			for _, r := range records {
				if r.Type != testTypeShellCommand {
					t.Errorf("expected type '%s', got '%s'", testTypeShellCommand, r.Type)
				}
				if r.Shell != string(TypeBash) {
					t.Errorf("expected shell '%s', got '%s'", TypeBash, r.Shell)
				}
				if r.User != testUser {
					t.Errorf("expected user '%s', got '%s'", testUser, r.User)
				}
				if r.Command == "" {
					t.Error("command should not be empty")
				}
			}
		})
	}
}

func TestIntegration_ParseZshFixtures(t *testing.T) {
	fixtures := []struct {
		name          string
		hasTimestamps bool
	}{
		{FixtureZshBasic, false},
		{FixtureZshExtended, true},
		{FixtureZshMultiline, true},
		{FixtureZshUnicode, false},
		{FixtureZshSpecial, false},
		{FixtureZshEmpty, false},
		{FixtureZshMixed, false}, // Some with timestamps
	}

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			content, err := loadFixture(tt.name)
			if err != nil {
				t.Fatalf("failed to load fixture: %v", err)
			}

			records, warnings := parseZshHistory(content, testUser, tt.name)

			t.Logf("%s: parsed %d records, %d warnings", tt.name, len(records), len(warnings))

			// Empty fixture should have no records
			if tt.name == FixtureZshEmpty && len(records) != 0 {
				t.Errorf("expected 0 records for empty fixture, got %d", len(records))
			}

			// Check record structure
			for _, r := range records {
				if r.Type != testTypeShellCommand {
					t.Errorf("expected type '%s', got '%s'", testTypeShellCommand, r.Type)
				}
				if r.Shell != string(TypeZsh) {
					t.Errorf("expected shell '%s', got '%s'", TypeZsh, r.Shell)
				}
			}
		})
	}
}

func TestIntegration_ParseFishFixtures(t *testing.T) {
	t.Run("fish_history", func(t *testing.T) {
		content, err := loadFixture(FixtureFish)
		if err != nil {
			t.Fatalf("failed to load fixture: %v", err)
		}

		records, warnings := parseFishHistory(content, testUser, FixtureFish)

		t.Logf("fish_history: parsed %d records, %d warnings", len(records), len(warnings))

		if len(records) == 0 {
			t.Error("expected at least some records")
		}

		// Check record structure
		for _, r := range records {
			if r.Type != testTypeShellCommand {
				t.Errorf("expected type '%s', got '%s'", testTypeShellCommand, r.Type)
			}
			if r.Shell != string(TypeFish) {
				t.Errorf("expected shell '%s', got '%s'", TypeFish, r.Shell)
			}
		}
	})
}

func TestIntegration_ParseShFixtures(t *testing.T) {
	fixtures := []string{
		FixtureShBasic,
		FixtureShUnicode,
		FixtureShSpecial,
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			content, err := loadFixture(name)
			if err != nil {
				t.Fatalf("failed to load fixture: %v", err)
			}

			records, warnings := parseShHistory(content, testUser, name)

			t.Logf("%s: parsed %d records, %d warnings", name, len(records), len(warnings))

			if len(records) == 0 {
				t.Error("expected at least some records")
			}

			// Check record structure
			for _, r := range records {
				if r.Type != testTypeShellCommand {
					t.Errorf("expected type '%s', got '%s'", testTypeShellCommand, r.Type)
				}
				if r.Shell != string(TypeSh) {
					t.Errorf("expected shell '%s', got '%s'", TypeSh, r.Shell)
				}
				// sh history never has timestamps
				if r.Timestamp != nil {
					t.Error("sh history should not have timestamps")
				}
			}
		})
	}
}

func TestIntegration_RecordSerialization(t *testing.T) {
	t.Run("bash record JSON", func(t *testing.T) {
		content := mustLoadFixture(FixtureBashTimestamped)
		records, _ := parseBashHistory(content, testUser, FixtureBashTimestamped)

		if len(records) == 0 {
			t.Fatal("expected at least one record")
		}

		// Serialize and deserialize
		data, err := json.Marshal(records[0])
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled BashRecord
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.Type != records[0].Type {
			t.Error("Type mismatch after serialization")
		}
		if unmarshaled.Command != records[0].Command {
			t.Error("Command mismatch after serialization")
		}
	})

	t.Run("zsh record JSON", func(t *testing.T) {
		content := mustLoadFixture(FixtureZshExtended)
		records, _ := parseZshHistory(content, testUser, FixtureZshExtended)

		if len(records) == 0 {
			t.Fatal("expected at least one record")
		}

		data, err := json.Marshal(records[0])
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled ZshRecord
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.Shell != string(TypeZsh) {
			t.Errorf("expected shell '%s', got '%s'", TypeZsh, unmarshaled.Shell)
		}
	})

	t.Run("fish record JSON", func(t *testing.T) {
		content := mustLoadFixture(FixtureFish)
		records, _ := parseFishHistory(content, testUser, FixtureFish)

		if len(records) == 0 {
			t.Fatal("expected at least one record")
		}

		data, err := json.Marshal(records[0])
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled FishRecord
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.Shell != string(TypeFish) {
			t.Errorf("expected shell '%s', got '%s'", TypeFish, unmarshaled.Shell)
		}
	})
}

func TestIntegration_EdgeCases(t *testing.T) {
	t.Run("empty file", func(t *testing.T) {
		content, err := loadFixture(FixtureEmpty)
		if err != nil {
			t.Fatalf("failed to load fixture: %v", err)
		}

		// All parsers should handle empty files gracefully
		bashRecords, _ := parseBashHistory(content, testUser, FixtureEmpty)
		zshRecords, _ := parseZshHistory(content, testUser, FixtureEmpty)
		fishRecords, _ := parseFishHistory(content, testUser, FixtureEmpty)
		shRecords, _ := parseShHistory(content, testUser, FixtureEmpty)

		if len(bashRecords) != 0 || len(zshRecords) != 0 || len(fishRecords) != 0 || len(shRecords) != 0 {
			t.Error("empty file should produce no records")
		}
	})

	t.Run("binary garbage", func(t *testing.T) {
		content, err := loadFixture(FixtureBinaryGarbage)
		if err != nil {
			t.Fatalf("failed to load fixture: %v", err)
		}

		// Parsers should not crash on binary data
		parseBashHistory(content, testUser, FixtureBinaryGarbage)
		parseZshHistory(content, testUser, FixtureBinaryGarbage)
		parseFishHistory(content, testUser, FixtureBinaryGarbage)
		parseShHistory(content, testUser, FixtureBinaryGarbage)

		// Test passes if no panic occurred
	})

	t.Run("long commands", func(t *testing.T) {
		if !fixtureExists(FixtureBashLongCmd) {
			t.Skip("long command fixture not available")
		}

		content, err := loadFixture(FixtureBashLongCmd)
		if err != nil {
			t.Fatalf("failed to load fixture: %v", err)
		}

		records, warnings := parseBashHistory(content, testUser, FixtureBashLongCmd)

		t.Logf("Long command fixture: %d records, %d warnings", len(records), len(warnings))

		// Should handle long lines (even if some are skipped with warnings)
		if len(warnings) == 0 && len(records) == 0 {
			t.Log("No records or warnings - fixture may need long lines >64KB to test scanner limits")
		}
	})
}

func TestIntegration_VersionComparison(t *testing.T) {
	testCases := []struct {
		current  string
		minimum  string
		expected bool
	}{
		{"14.2.1", "10.12", true},
		{"14.2", "10.12", true},
		{"10.12", "10.12", true},
		{"10.11", "10.12", false},
		{"10.12.1", "10.12", true},
		{"10.12", "10.12.1", false},
		{"15.0", "14.0", true},
		{"13.0", "14.0", false},
	}

	for _, tc := range testCases {
		t.Run(tc.current+"_vs_"+tc.minimum, func(t *testing.T) {
			result := artifacts.VersionAtLeast(tc.current, tc.minimum)
			if result != tc.expected {
				t.Errorf("artifacts.VersionAtLeast(%q, %q) = %v, expected %v",
					tc.current, tc.minimum, result, tc.expected)
			}
		})
	}
}

func TestIntegration_ShellTypes(t *testing.T) {
	// Verify shell type constants
	shells := []Type{TypeBash, TypeZsh, TypeFish, TypeSh}

	for _, s := range shells {
		if s == "" {
			t.Errorf("shell type should not be empty")
		}

		// Each shell should have a history path
		if _, ok := shellArtifactIDs[s]; !ok {
			t.Errorf("shell %s missing history path", s)
		}
	}

	// Verify collection order includes all shells
	for _, s := range shells {
		found := false
		for _, co := range CollectionOrder {
			if co == s {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("shell %s not in CollectionOrder", s)
		}
	}
}

func TestIntegration_AllFixturesLoadable(t *testing.T) {
	fixtures, err := listFixtures()
	if err != nil {
		t.Fatalf("failed to list fixtures: %v", err)
	}

	if len(fixtures) == 0 {
		t.Fatal("no fixtures found")
	}

	t.Logf("Found %d fixtures", len(fixtures))

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			content, err := loadFixture(name)
			if err != nil {
				t.Errorf("failed to load fixture %s: %v", name, err)
			}
			if !strings.HasSuffix(name, "_empty") && name != "empty_history" && len(content) == 0 {
				t.Logf("warning: fixture %s is empty", name)
			}
		})
	}
}

func TestIntegration_CollectWithMockCollector(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	p := &Plugin{}

	// Check if applicable first
	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	if !result.Applicable {
		t.Skip("shell.history not applicable on this system")
	}

	// Use mock collector (from bash_test.go)
	coll := newMockCollector()

	_ = p.Collect(context.Background(), src, coll)

	// Log results
	t.Logf("Collection results: %d files, %d records, %d errors, %d log messages",
		len(coll.files), len(coll.records), len(coll.errors), len(coll.logs))

	// Should have collected something (if applicable)
	if len(coll.files) == 0 && len(coll.records) == 0 && len(coll.errors) == 0 {
		t.Log("No files, records, or errors collected (user may have no shell history)")
	}

	// Check file artifacts
	for path := range coll.files {
		if !strings.HasPrefix(path, "artifacts/shell/") {
			t.Errorf("expected artifact path to start with 'artifacts/shell/', got '%s'", path)
		}
	}
}
