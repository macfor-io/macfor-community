package users

import (
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// TestCollectionOrderArtifactsHaveHandlers guards that every collected artifact
// resolves in the manifest and declares a parser with a registered handler.
func TestCollectionOrderArtifactsHaveHandlers(t *testing.T) {
	for _, artifactType := range CollectionOrder {
		def := artifacts.GetArtifactByID(manifest, string(artifactType))
		if def == nil {
			t.Errorf("artifact %q is in CollectionOrder but not defined in the manifest", artifactType)
			continue
		}
		if def.Extract != nil {
			t.Errorf("artifact %q declares extract, but account plists have no declarative backend", artifactType)
		}
		if _, ok := nativeParsers[def.Parser]; !ok {
			t.Errorf("artifact %q references parser %q with no registered handler", artifactType, def.Parser)
		}
	}
}

// TestNoManifestDrift guards that every declared artifact is either collected
// (present in CollectionOrder) or explicitly marked planned, never both.
func TestNoManifestDrift(t *testing.T) {
	collected := make(map[string]bool, len(CollectionOrder))
	for _, e := range CollectionOrder {
		collected[string(e)] = true
	}

	for i := range manifest.Artifacts {
		a := &manifest.Artifacts[i]
		if collected[a.ID] && a.Planned {
			t.Errorf("artifact %q is both in CollectionOrder and marked planned", a.ID)
		}
		if !collected[a.ID] && !a.Planned {
			t.Errorf("artifact %q is declared but neither collected nor marked planned", a.ID)
		}
	}
}
