package safari

import (
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// TestNoManifestDrift guards the manifest against documentation drift: every
// declared artifact must be either collected (present in CollectionOrder) or
// explicitly marked planned, and every collected id must resolve in the
// manifest. A planned artifact must never also be collected.
func TestNoManifestDrift(t *testing.T) {
	collected := make(map[string]bool, len(CollectionOrder))
	for _, e := range CollectionOrder {
		collected[string(e)] = true
	}

	// Every declared artifact is either collected or planned.
	for i := range manifest.Artifacts {
		a := &manifest.Artifacts[i]
		if collected[a.ID] && a.Planned {
			t.Errorf("artifact %q is both in CollectionOrder and marked planned: a planned artifact must not be collected", a.ID)
		}
		if !collected[a.ID] && !a.Planned {
			t.Errorf("artifact %q is declared but neither collected (in CollectionOrder) nor marked planned: undocumented drift", a.ID)
		}
	}

	// Every collected id resolves in the manifest.
	for _, e := range CollectionOrder {
		id := string(e)
		if artifacts.GetArtifactByID(manifest, id) == nil {
			t.Errorf("collected id %q does not resolve to an artifact in the manifest", id)
		}
	}
}
