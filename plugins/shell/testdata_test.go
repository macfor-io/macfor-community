package shell

import (
	"strings"
	"testing"
)

func TestLoadFixture(t *testing.T) {
	content, err := loadFixture(FixtureBashBasic)
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}

	if !strings.Contains(content, "ls -la") {
		t.Error("expected fixture to contain 'ls -la'")
	}
}

func TestLoadFixture_NotFound(t *testing.T) {
	_, err := loadFixture("nonexistent_fixture")
	if err == nil {
		t.Error("expected error for nonexistent fixture")
	}
}

func TestMustLoadFixture(t *testing.T) {
	// Should not panic for existing fixture
	content := mustLoadFixture(FixtureBashBasic)
	if content == "" {
		t.Error("expected non-empty content")
	}
}

func TestMustLoadFixture_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nonexistent fixture")
		}
	}()
	mustLoadFixture("nonexistent_fixture")
}

func TestFixtureExists(t *testing.T) {
	if !fixtureExists(FixtureBashBasic) {
		t.Error("expected bash_history_basic to exist")
	}

	if fixtureExists("nonexistent_fixture") {
		t.Error("expected nonexistent_fixture to not exist")
	}
}

func TestListFixtures(t *testing.T) {
	fixtures, err := listFixtures()
	if err != nil {
		t.Fatalf("failed to list fixtures: %v", err)
	}

	if len(fixtures) == 0 {
		t.Error("expected at least one fixture")
	}

	// Check for some expected fixtures
	expectedFixtures := []string{
		FixtureBashBasic,
		FixtureZshExtended,
		FixtureFish,
		FixtureShBasic,
		FixtureEmpty,
		FixtureBinaryGarbage,
	}

	fixtureSet := make(map[string]bool)
	for _, f := range fixtures {
		fixtureSet[f] = true
	}

	for _, expected := range expectedFixtures {
		if !fixtureSet[expected] {
			t.Errorf("expected fixture %q to be listed", expected)
		}
	}
}

func TestTestdataDir(t *testing.T) {
	dir := testdataDir()
	if dir == "" {
		t.Error("expected non-empty testdata directory path")
	}

	if !strings.Contains(dir, "testdata") {
		t.Errorf("expected path to contain 'testdata', got %q", dir)
	}
}

// TestAllFixturesLoadable verifies that all declared fixture constants
// correspond to actual files.
func TestAllFixturesLoadable(t *testing.T) {
	fixtures := []string{
		FixtureBashBasic,
		FixtureBashTimestamped,
		FixtureBashMixed,
		FixtureBashUnicode,
		FixtureBashSpecial,
		FixtureBashEmpty,
		FixtureBashLongCmd,
		FixtureZshExtended,
		FixtureZshBasic,
		FixtureZshMultiline,
		FixtureZshUnicode,
		FixtureZshSpecial,
		FixtureZshEmpty,
		FixtureZshMixed,
		FixtureZshLongCmd,
		FixtureFish,
		FixtureFishLongCmd,
		FixtureShBasic,
		FixtureShUnicode,
		FixtureShSpecial,
		FixtureEmpty,
		FixtureBinaryGarbage,
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			if !fixtureExists(name) {
				t.Errorf("fixture %q does not exist", name)
			}
		})
	}
}
