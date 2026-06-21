package macfor_test

import (
	"context"
	"sync"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// mockPlugin is a test implementation of the Plugin interface.
type mockPlugin struct {
	info macfor.PluginInfo
}

func (m *mockPlugin) Info() macfor.PluginInfo {
	return m.info
}

func (m *mockPlugin) Detect(_ context.Context, _ macfor.Source) (macfor.DetectionResult, error) {
	return macfor.DetectionResult{Applicable: true}, nil
}

func (m *mockPlugin) Collect(_ context.Context, _ macfor.Source, _ macfor.Collector) error {
	return nil
}

func newMockPlugin(id, name string, categories ...string) *mockPlugin {
	return &mockPlugin{
		info: macfor.PluginInfo{
			ID:         id,
			Name:       name,
			Categories: categories,
		},
	}
}

func TestRegisterAndByID(t *testing.T) {
	// Clear registry before test
	macfor.ClearRegistry()

	plugin := newMockPlugin("test.plugin", "Test Plugin")
	macfor.Register(plugin)

	got, ok := macfor.ByID("test.plugin")
	if !ok {
		t.Fatal("ByID returned false for registered plugin")
	}
	if got.Info().ID != "test.plugin" {
		t.Errorf("ByID returned plugin with ID %q, want %q", got.Info().ID, "test.plugin")
	}
}

func TestByIDNotFound(t *testing.T) {
	macfor.ClearRegistry()

	_, ok := macfor.ByID("nonexistent.plugin")
	if ok {
		t.Error("ByID returned true for non-existent plugin")
	}
}

func TestAll(t *testing.T) {
	macfor.ClearRegistry()

	plugin1 := newMockPlugin("plugin.one", "Plugin One")
	plugin2 := newMockPlugin("plugin.two", "Plugin Two")
	plugin3 := newMockPlugin("plugin.three", "Plugin Three")

	macfor.Register(plugin1)
	macfor.Register(plugin2)
	macfor.Register(plugin3)

	all := macfor.All()
	if len(all) != 3 {
		t.Errorf("All() returned %d plugins, want 3", len(all))
	}

	// Verify all plugins are present (order not guaranteed)
	ids := make(map[string]bool)
	for _, p := range all {
		ids[p.Info().ID] = true
	}
	for _, id := range []string{"plugin.one", "plugin.two", "plugin.three"} {
		if !ids[id] {
			t.Errorf("All() missing plugin with ID %q", id)
		}
	}
}

func TestAllEmpty(t *testing.T) {
	macfor.ClearRegistry()

	all := macfor.All()
	if len(all) != 0 {
		t.Errorf("All() returned %d plugins for empty registry, want 0", len(all))
	}
}

func TestByCategory(t *testing.T) {
	macfor.ClearRegistry()

	shellPlugin := newMockPlugin("shell.history", "Shell History", "user", "command", "shell")
	browserPlugin := newMockPlugin("browser.safari", "Safari", "user", "browser")
	systemPlugin := newMockPlugin("system.logs", "System Logs", "system")

	macfor.Register(shellPlugin)
	macfor.Register(browserPlugin)
	macfor.Register(systemPlugin)

	tests := []struct {
		category string
		wantIDs  []string
	}{
		{"user", []string{"shell.history", "browser.safari"}},
		{"shell", []string{"shell.history"}},
		{"browser", []string{"browser.safari"}},
		{"system", []string{"system.logs"}},
		{"command", []string{"shell.history"}},
		{"nonexistent", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := macfor.ByCategory(tt.category)
			if len(got) != len(tt.wantIDs) {
				t.Errorf("ByCategory(%q) returned %d plugins, want %d", tt.category, len(got), len(tt.wantIDs))
				return
			}

			gotIDs := make(map[string]bool)
			for _, p := range got {
				gotIDs[p.Info().ID] = true
			}
			for _, id := range tt.wantIDs {
				if !gotIDs[id] {
					t.Errorf("ByCategory(%q) missing plugin with ID %q", tt.category, id)
				}
			}
		})
	}
}

func TestRegisterOverwrite(t *testing.T) {
	macfor.ClearRegistry()

	plugin1 := newMockPlugin("same.id", "First Plugin")
	plugin2 := newMockPlugin("same.id", "Second Plugin")

	macfor.Register(plugin1)
	macfor.Register(plugin2)

	got, ok := macfor.ByID("same.id")
	if !ok {
		t.Fatal("ByID returned false after overwrite")
	}
	if got.Info().Name != "Second Plugin" {
		t.Errorf("ByID returned plugin named %q, want %q (overwritten)", got.Info().Name, "Second Plugin")
	}

	// Verify only one plugin exists
	if macfor.RegisteredCount() != 1 {
		t.Errorf("RegisteredCount() = %d, want 1 after overwrite", macfor.RegisteredCount())
	}
}

func TestConcurrentRegistration(t *testing.T) {
	macfor.ClearRegistry()

	const numGoroutines = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			plugin := newMockPlugin(
				"concurrent.plugin",
				"Concurrent Plugin",
			)
			macfor.Register(plugin)
		}()
	}

	wg.Wait()

	// Should have exactly one plugin (all registrations use the same ID)
	if macfor.RegisteredCount() != 1 {
		t.Errorf("RegisteredCount() = %d after concurrent registration, want 1", macfor.RegisteredCount())
	}
}

func TestConcurrentReadWrite(_ *testing.T) {
	macfor.ClearRegistry()

	// Pre-register some plugins
	for i := 0; i < 10; i++ {
		plugin := newMockPlugin("plugin."+string(rune('a'+i)), "Plugin")
		macfor.Register(plugin)
	}

	const numReaders = 50
	const numWriters = 10

	var wg sync.WaitGroup
	wg.Add(numReaders + numWriters)

	// Concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			_ = macfor.All()
			_, _ = macfor.ByID("plugin.a")
			_ = macfor.ByCategory("user")
		}()
	}

	// Concurrent writers
	for i := 0; i < numWriters; i++ {
		go func(idx int) {
			defer wg.Done()
			plugin := newMockPlugin("new.plugin."+string(rune('a'+idx)), "New Plugin", "user")
			macfor.Register(plugin)
		}(i)
	}

	wg.Wait()

	// No panic means thread-safety works
}

func TestRegisteredCount(t *testing.T) {
	macfor.ClearRegistry()

	if macfor.RegisteredCount() != 0 {
		t.Errorf("RegisteredCount() = %d for empty registry, want 0", macfor.RegisteredCount())
	}

	macfor.Register(newMockPlugin("one", "One"))
	if macfor.RegisteredCount() != 1 {
		t.Errorf("RegisteredCount() = %d, want 1", macfor.RegisteredCount())
	}

	macfor.Register(newMockPlugin("two", "Two"))
	if macfor.RegisteredCount() != 2 {
		t.Errorf("RegisteredCount() = %d, want 2", macfor.RegisteredCount())
	}
}
