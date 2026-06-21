package safari

import (
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// TestCollectionOrderArtifactsHaveHandlers guards that every artifact the plugin
// collects declares a parse path in the manifest (extract or parser) and that
// the path resolves to a registered handler. This catches a CollectionOrder
// entry added without wiring its parser, or a manifest `parser:` name that does
// not match the native-parser registry.
func TestCollectionOrderArtifactsHaveHandlers(t *testing.T) {
	for _, at := range CollectionOrder {
		def := artifacts.GetArtifactByID(manifest, string(at))
		if def == nil {
			t.Errorf("artifact %q is in CollectionOrder but not defined in the manifest", at)
			continue
		}

		switch {
		case def.Extract != nil:
			if _, ok := declarativeHandlers[at]; !ok {
				t.Errorf("artifact %q declares extract but has no declarative handler", at)
			}
		case def.Parser != "":
			if _, ok := nativeParsers[def.Parser]; !ok {
				t.Errorf("artifact %q references parser %q with no registered handler", at, def.Parser)
			}
		default:
			t.Errorf("artifact %q has neither an extract block nor a parser in the manifest", at)
		}
	}
}

// TestLocatorEnforcesAppVersion verifies that app_min/app_max constraints in
// the manifest are enforced during path resolution using the Safari version
// detected from the source — not just displayed in the catalog. Tab Groups
// (SafariTabs.db) is gated to Safari 15+.
func TestLocatorEnforcesAppVersion(t *testing.T) {
	user := macfor.User{Username: "admin", HomeDir: "/Users/admin"}

	resolve := func(setup func(*mockSource)) []artifacts.ResolvedPath {
		t.Helper()
		src := newMockSource()
		if setup != nil {
			setup(src)
		}
		paths, err := locator.Paths(src, user, "tab_groups")
		if err != nil {
			t.Fatalf("Paths() error = %v", err)
		}
		return paths
	}

	// Safari 14: below app_min 15.0 — filtered out.
	if paths := resolve(func(src *mockSource) { src.setSafariVersion("14.1.2") }); len(paths) != 0 {
		t.Errorf("expected no tab_groups paths on Safari 14, got %v", paths)
	}

	// Safari 17: resolves.
	paths := resolve(func(src *mockSource) { src.setSafariVersion("17.2") })
	if len(paths) != 1 || paths[0].Path != "/Users/admin/Library/Safari/SafariTabs.db" {
		t.Errorf("expected SafariTabs.db path on Safari 17, got %v", paths)
	}

	// Safari version undetectable: constraint not applied — path included.
	if paths := resolve(nil); len(paths) != 1 {
		t.Errorf("expected 1 tab_groups path when Safari version is unknown, got %v", paths)
	}
}

// TestManifestDeriversAreRegistered guards that every deriver name referenced
// from an extract block in artifacts.yaml has an implementation in
// pluginDerivers, so a manifest/deriver mismatch fails at test time rather
// than during collection.
func TestManifestDeriversAreRegistered(t *testing.T) {
	for i := range manifest.Artifacts {
		a := &manifest.Artifacts[i]
		if a.Extract == nil {
			continue
		}
		for _, f := range a.Extract.Fields {
			if f.Derived == "" {
				continue
			}
			if _, ok := pluginDerivers[f.Derived]; !ok {
				t.Errorf("artifact %q field %q references deriver %q with no implementation in pluginDerivers",
					a.ID, f.Name, f.Derived)
			}
		}
	}
}
