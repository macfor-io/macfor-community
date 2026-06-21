package cli

import (
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

func TestFormatVersionRange(t *testing.T) {
	cases := []struct {
		min, max, want string
	}{
		{"", "", versionRangeAll},
		{"10.14", "", "10.14+"},
		{"", "10.15", "≤10.15"},
		{"11.0", "12.0", "11.0–12.0"},
		{"15.0", "", "15.0+"},
	}
	for _, c := range cases {
		if got := formatVersionRange(c.min, c.max); got != c.want {
			t.Errorf("formatVersionRange(%q,%q) = %q, want %q", c.min, c.max, got, c.want)
		}
	}
}

func TestParsedByLabel(t *testing.T) {
	declarative := &artifacts.Artifact{Extract: &artifacts.ExtractSpec{From: "T"}}
	if got := parsedByLabel(declarative); got != parsedByExtract {
		t.Errorf("extract artifact: got %q", got)
	}

	named := &artifacts.Artifact{Parser: "safari.cookies"}
	if got := parsedByLabel(named); got != "native: safari.cookies" {
		t.Errorf("named parser: got %q", got)
	}

	// No extract and no parser: a collected artifact whose location is
	// manifest-driven but whose parsing is hand-written native code.
	bare := &artifacts.Artifact{}
	if got := parsedByLabel(bare); got != parsedByNative {
		t.Errorf("bare artifact: got %q, want %q", got, parsedByNative)
	}

	// Planned artifacts are reported as planned regardless of extract/parser.
	planned := &artifacts.Artifact{Planned: true}
	if got := parsedByLabel(planned); got != parsedByPlanned {
		t.Errorf("planned artifact: got %q, want %q", got, parsedByPlanned)
	}
}

func TestBuildCatalog(t *testing.T) {
	m := &artifacts.Manifest{
		Plugin: artifacts.Plugin{ID: "browser.test", Name: "Test"},
		Artifacts: []artifacts.Artifact{
			{
				ID:      "history",
				Name:    "History",
				Format:  "sqlite",
				Extract: &artifacts.ExtractSpec{From: "items"},
				Paths: []artifacts.PathSpec{
					{Pattern: "~/h.db", Type: "file"},
				},
			},
			{
				ID:       "tabs",
				Name:     "Tabs",
				Format:   "sqlite",
				Optional: true,
				Paths: []artifacts.PathSpec{
					{Pattern: "~/SafariTabs.db", Type: "file", AppMin: "15.0"},
				},
			},
			{
				ID:     "ls",
				Name:   "LocalStorage",
				Format: "sqlite",
				Paths: []artifacts.PathSpec{
					{Pattern: "~/old/*.ls", Type: "glob", OSMax: "10.15"},
					{Pattern: "~/new/*.ls", Type: "glob", OSMin: "11.0"},
				},
			},
		},
	}

	cat := buildCatalog(m)
	if cat.PluginID != "browser.test" {
		t.Errorf("plugin id = %q", cat.PluginID)
	}
	// 1 (history) + 1 (tabs) + 2 (ls) = 4 rows.
	if len(cat.Rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(cat.Rows))
	}

	if cat.Rows[0].ParsedBy != parsedByExtract {
		t.Errorf("history parsed_by = %q", cat.Rows[0].ParsedBy)
	}
	if cat.Rows[1].AppRange != "15.0+" || !cat.Rows[1].Optional {
		t.Errorf("tabs row = %+v", cat.Rows[1])
	}
	if cat.Rows[2].OSRange != "≤10.15" || cat.Rows[3].OSRange != "11.0+" {
		t.Errorf("ls os ranges = %q, %q", cat.Rows[2].OSRange, cat.Rows[3].OSRange)
	}
}
