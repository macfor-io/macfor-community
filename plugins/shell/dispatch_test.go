package shell

import (
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// TestCollectionOrderShellsHaveHandlers guards that every shell the plugin
// collects maps to a manifest artifact whose declared parser has a registered
// handler. This catches a CollectionOrder entry without a manifest artifact, a
// manifest `parser:` name with no implementation, or a stray extract block on
// a format with no declarative backend.
func TestCollectionOrderShellsHaveHandlers(t *testing.T) {
	for _, shellType := range CollectionOrder {
		artifactID, ok := shellArtifactIDs[shellType]
		if !ok {
			t.Errorf("shell %q is in CollectionOrder but has no artifact ID mapping", shellType)
			continue
		}

		def := artifacts.GetArtifactByID(manifest, artifactID)
		if def == nil {
			t.Errorf("artifact %q (shell %s) not defined in the manifest", artifactID, shellType)
			continue
		}

		if def.Extract != nil {
			t.Errorf("artifact %q declares extract, but shell history has no declarative backend", artifactID)
		}
		if def.Parser == "" {
			t.Errorf("artifact %q declares no parser in the manifest", artifactID)
			continue
		}
		if _, ok := nativeParsers[def.Parser]; !ok {
			t.Errorf("artifact %q references parser %q with no registered handler", artifactID, def.Parser)
		}
	}

	// Every artifact-ID mapping must be reachable from CollectionOrder.
	covered := make(map[Type]bool, len(CollectionOrder))
	for _, s := range CollectionOrder {
		covered[s] = true
	}
	for shellType := range shellArtifactIDs {
		if !covered[shellType] {
			t.Errorf("shell %q has an artifact mapping but is not in CollectionOrder", shellType)
		}
	}
}
