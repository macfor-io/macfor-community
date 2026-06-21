package plistmapper

import (
	"context"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

type plRec struct {
	Kind  string    `json:"kind"`
	User  string    `json:"user"`
	Name  string    `json:"name,omitempty"`
	Base  string    `json:"base,omitempty"`
	Size  int64     `json:"size"`
	When  time.Time `json:"when"`
	OK    bool      `json:"ok"`
	State string    `json:"state"`
}

func recSpec() artifacts.ExtractSpec {
	return artifacts.ExtractSpec{
		Records: "Items",
		Fields: []artifacts.FieldSpec{
			{Name: "kind", Const: "item"},
			{Name: "user", Context: "username"},
			{Name: "name", Key: "Name"},
			{Name: "base", Key: "Path", Transform: "basename"},
			{Name: "size", Key: "Size"},
			{Name: "when", Key: "Date"},
			{Name: "ok", Key: "Enabled"},
			{Name: "state", Derived: "test.state"},
		},
	}
}

const dictRootPlist = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>Items</key>
  <array>
    <dict>
      <key>Name</key><string>alpha</string>
      <key>Path</key><string>/a/b/file.txt</string>
      <key>Size</key><integer>250</integer>
      <key>Date</key><date>2024-01-02T03:04:05Z</date>
      <key>Enabled</key><true/>
    </dict>
    <dict>
      <key>Path</key><string>/x/small.bin</string>
      <key>Size</key><integer>10</integer>
      <key>Date</key><date>2020-06-07T00:00:00Z</date>
      <key>Enabled</key><false/>
    </dict>
  </array>
</dict>
</plist>`

func mapAll(t *testing.T, data string, spec artifacts.ExtractSpec) []*plRec {
	t.Helper()
	var recs []*plRec
	_, err := Each(context.Background(), []byte(data), spec, Options{
		NewRecord: func() any { return &plRec{} },
		Context:   map[string]string{"username": "alice"},
		Derivers: map[string]Deriver{
			"test.state": func(el map[string]any) (any, error) {
				if n, ok := toInt64(el["Size"]); ok && n > 100 {
					return "big", nil
				}
				return "small", nil
			},
		},
	}, func(rec any) error {
		recs = append(recs, rec.(*plRec))
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	return recs
}

func TestEach_DictRootAllFieldKinds(t *testing.T) {
	recs := mapAll(t, dictRootPlist, recSpec())
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}

	r0 := recs[0]
	if r0.Kind != "item" || r0.User != "alice" {
		t.Errorf("const/context wrong: %+v", r0)
	}
	if r0.Name != "alpha" || r0.Base != "file.txt" || r0.Size != 250 || !r0.OK {
		t.Errorf("key/transform fields wrong: %+v", r0)
	}
	if r0.When.Year() != 2024 {
		t.Errorf("date mapping wrong: %v", r0.When)
	}
	if r0.State != "big" {
		t.Errorf("derived state wrong: %q", r0.State)
	}

	r1 := recs[1]
	if r1.Name != "" { // absent key -> empty (omitempty).
		t.Errorf("expected empty name, got %q", r1.Name)
	}
	if r1.Base != "small.bin" || r1.OK || r1.State != "small" {
		t.Errorf("second record wrong: %+v", r1)
	}
}

func TestEach_ArrayRootFallback(t *testing.T) {
	const arrayRoot = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<array>
  <dict><key>Name</key><string>solo</string><key>Size</key><integer>5</integer></dict>
</array>
</plist>`
	// Records key set but root is an array -> use the array.
	recs := mapAll(t, arrayRoot, recSpec())
	if len(recs) != 1 || recs[0].Name != "solo" || recs[0].State != "small" {
		t.Fatalf("array-root fallback failed: %+v", recs)
	}
}

func TestEach_MissingRecordsKeyIsEmpty(t *testing.T) {
	const noKey = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>Other</key><string>x</string></dict></plist>`
	recs := mapAll(t, noKey, recSpec())
	if len(recs) != 0 {
		t.Errorf("expected 0 records for absent key, got %d", len(recs))
	}
}

func TestEach_UnknownDeriverFailsFast(t *testing.T) {
	// An unresolved deriver reference is a config error: it must fail before any
	// element is processed, not silently skip every record.
	spec := recSpec()
	emitted := 0
	_, err := Each(context.Background(), []byte(dictRootPlist), spec, Options{
		NewRecord: func() any { return &plRec{} },
		Context:   map[string]string{"username": "alice"},
		// No derivers registered -> "test.state" unknown.
	}, func(any) error { emitted++; return nil })
	if err == nil {
		t.Fatal("expected a config error for an unknown deriver")
	}
	if emitted != 0 {
		t.Errorf("expected no records emitted, got %d", emitted)
	}
}

func TestEach_UnknownTransformFailsFast(t *testing.T) {
	spec := recSpec()
	spec.Fields[3].Transform = "does_not_exist" // base field.
	_, err := Each(context.Background(), []byte(dictRootPlist), spec, Options{
		NewRecord: func() any { return &plRec{} },
		Context:   map[string]string{"username": "alice"},
		Derivers: map[string]Deriver{
			"test.state": func(map[string]any) (any, error) { return "small", nil },
		},
	}, func(any) error { return nil })
	if err == nil {
		t.Fatal("expected a config error for an unknown transform")
	}
}

func TestEach_InvalidPlistErrors(t *testing.T) {
	_, err := Each(context.Background(), []byte("not a plist"), recSpec(), Options{
		NewRecord: func() any { return &plRec{} },
	}, func(any) error { return nil })
	if err == nil {
		t.Error("expected error for invalid plist")
	}
}

// TestTransformsSyncWithDeclaredVocabulary guards that the validator's declared
// plist transform vocabulary and this mapper's resolver cannot drift.
func TestTransformsSyncWithDeclaredVocabulary(t *testing.T) {
	declared := artifacts.TransformsForFormat(artifacts.FormatBinaryPlist)
	if len(declared) == 0 {
		t.Fatal("no transforms declared for binary_plist")
	}
	for _, name := range declared {
		if _, err := resolveTransform(name); err != nil {
			t.Errorf("declared transform %q does not resolve in the plist mapper: %v", name, err)
		}
	}
	if !artifacts.IsValidTransform(artifacts.FormatBinaryPlist, "truncate:10") {
		t.Error("validator rejects truncate:10")
	}
	if _, err := resolveTransform("truncate:10"); err != nil {
		t.Errorf("mapper rejects truncate:10: %v", err)
	}
}

// TestEach_ColumnSourceFailsFast guards that a SQL column reference on a plist
// extract is a config error, not a silently empty field.
func TestEach_ColumnSourceFailsFast(t *testing.T) {
	spec := artifacts.ExtractSpec{
		Records: "Items",
		Fields:  []artifacts.FieldSpec{{Name: "url", Column: "items.url"}},
	}
	_, err := Each(context.Background(), []byte(dictRootPlist), spec, Options{
		NewRecord: func() any { return &plRec{} },
	}, func(any) error { return nil })
	if err == nil {
		t.Fatal("expected a config error for a column field on a plist extract")
	}
}
