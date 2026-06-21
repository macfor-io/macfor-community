package schemas

import (
	"testing"

	macforparquet "github.com/macfor-io/macfor-community/pkg/macfor/parquet"
)

func TestSchemaRegistration(t *testing.T) {
	r := macforparquet.GlobalRegistry()

	// Shell history schemas.
	shellArtifacts := []string{"bash_history", "zsh_history", "fish_history", "sh_history"}
	for _, at := range shellArtifacts {
		entry := r.Lookup("shell.history", at)
		if entry == nil {
			t.Errorf("shell.history/%s not registered", at)
			continue
		}
		if entry.Version.String() != "1.0" {
			t.Errorf("shell.history/%s version = %s, want 1.0", at, entry.Version.String())
		}
		if entry.Checksum == "" {
			t.Errorf("shell.history/%s checksum is empty", at)
		}
		if entry.GoType == nil {
			t.Errorf("shell.history/%s GoType is nil", at)
		}
	}

	// Safari browser schemas.
	safariArtifacts := []string{
		"history", "downloads", "bookmarks", "reading_list",
		"cookies", "extensions", "local_storage",
		"session_windows", "session_tabs",
	}
	for _, at := range safariArtifacts {
		entry := r.Lookup("browser.safari", at)
		if entry == nil {
			t.Errorf("browser.safari/%s not registered", at)
			continue
		}
		if entry.Version.String() != "1.0" {
			t.Errorf("browser.safari/%s version = %s, want 1.0", at, entry.Version.String())
		}
		if entry.Checksum == "" {
			t.Errorf("browser.safari/%s checksum is empty", at)
		}
	}

	// Total count: 4 shell + 9 safari = 13.
	if r.Len() < 13 {
		t.Errorf("registry has %d entries, want at least 13", r.Len())
	}
}
