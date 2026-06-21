package main

import (
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// TestAllPluginsExposeManifests guards that every plugin registered by this
// binary implements macfor.ManifestProvider with a manifest whose plugin ID
// matches the plugin's own ID. This keeps the manifest tooling
// (matrix/paths/docs) complete as plugins are added: a new plugin that forgets
// the one-line Manifest() method fails here instead of silently vanishing from
// the artifact location catalog.
func TestAllPluginsExposeManifests(t *testing.T) {
	plugins := macfor.All()
	if len(plugins) == 0 {
		t.Fatal("no plugins registered")
	}

	for _, p := range plugins {
		id := p.Info().ID

		mp, ok := p.(macfor.ManifestProvider)
		if !ok {
			t.Errorf("plugin %s does not implement macfor.ManifestProvider", id)
			continue
		}

		m := mp.Manifest()
		if m == nil {
			t.Errorf("plugin %s returned a nil manifest", id)
			continue
		}
		if m.Plugin.ID != id {
			t.Errorf("plugin %s manifest declares mismatched ID %q", id, m.Plugin.ID)
		}
	}
}
