package tablemapper

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	_ "modernc.org/sqlite" // SQLite driver registration.
)

// rec is a sample record schema exercising const, context, column, transform,
// nullable-pointer, and optional-column field mappings.
type rec struct {
	Kind string `json:"kind"`
	User string `json:"user"`
	Name string `json:"name,omitempty"`
	OK   bool   `json:"ok"`
	Opt  *int64 `json:"opt,omitempty"`
	ID   int64  `json:"id"`
}

func openDB(t *testing.T, schema string, inserts ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	for _, ins := range inserts {
		if _, err := db.ExecContext(ctx, ins); err != nil {
			t.Fatalf("insert %q: %v", ins, err)
		}
	}
	return db
}

func baseSpec() artifacts.ExtractSpec {
	return artifacts.ExtractSpec{
		From:    "t",
		OrderBy: "id ASC",
		Fields: []artifacts.FieldSpec{
			{Name: "kind", Const: "row"},
			{Name: "user", Context: "username"},
			{Name: "name", Column: "name"},
			{Name: "ok", Column: "flag", Transform: "bool_from_int"},
			{Name: "opt", Column: "opt", Optional: true},
			{Name: "id", Column: "id"},
		},
	}
}

func TestEach_MapsAllFieldKinds(t *testing.T) {
	db := openDB(t,
		`CREATE TABLE t (id INTEGER, name TEXT, flag INTEGER, opt INTEGER)`,
		`INSERT INTO t VALUES (1, 'a', 1, NULL)`,
		`INSERT INTO t VALUES (2, NULL, 0, 42)`,
	)

	var got []*rec
	n, err := Each(context.Background(), db, baseSpec(), Options{
		NewRecord: func() any { return &rec{} },
		Context:   map[string]string{"username": "alice"},
	}, func(r any) error {
		got = append(got, r.(*rec))
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if n != 2 || len(got) != 2 {
		t.Fatalf("expected 2 records, got n=%d len=%d", n, len(got))
	}

	r0 := got[0]
	if r0.Kind != "row" || r0.User != "alice" || r0.Name != "a" || !r0.OK || r0.Opt != nil || r0.ID != 1 {
		t.Errorf("row0 mismatch: %+v", r0)
	}

	r1 := got[1]
	if r1.Name != "" { // NULL name → empty string.
		t.Errorf("row1 expected empty name, got %q", r1.Name)
	}
	if r1.OK { // flag 0 → false.
		t.Errorf("row1 expected ok=false")
	}
	if r1.Opt == nil || *r1.Opt != 42 { // non-NULL opt → pointer.
		t.Errorf("row1 expected opt=42, got %v", r1.Opt)
	}
}

func TestEach_OptionalColumnAbsentIsSkipped(t *testing.T) {
	// Table has no "opt" column; the optional field must be dropped, leaving the
	// pointer nil, with a warning — not an error.
	db := openDB(t,
		`CREATE TABLE t (id INTEGER, name TEXT, flag INTEGER)`,
		`INSERT INTO t VALUES (1, 'a', 1)`,
	)

	var warnings int
	var got []*rec
	_, err := Each(context.Background(), db, baseSpec(), Options{
		NewRecord: func() any { return &rec{} },
		Context:   map[string]string{"username": "alice"},
		Warnf:     func(string, ...any) { warnings++ },
	}, func(r any) error {
		got = append(got, r.(*rec))
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if len(got) != 1 || got[0].Opt != nil {
		t.Fatalf("expected 1 record with nil Opt, got %+v", got)
	}
	if warnings == 0 {
		t.Error("expected a drift warning for the absent optional column")
	}
}

// timeRec exercises the coredata_timestamp transform into a time.Time field.
type timeRec struct {
	When time.Time `json:"when"`
}

func TestEach_CoreDataTimestampTransform(t *testing.T) {
	db := openDB(t,
		`CREATE TABLE t (id INTEGER, ts REAL)`,
		`INSERT INTO t VALUES (1, 727005600.0)`, // ~2024.
	)

	var year int
	_, err := Each(context.Background(), db, artifacts.ExtractSpec{
		From: "t",
		Fields: []artifacts.FieldSpec{
			{Name: "when", Column: "ts", Transform: "coredata_timestamp"},
		},
	}, Options{
		NewRecord: func() any { return &timeRec{} },
	}, func(r any) error {
		year = r.(*timeRec).When.Year()
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if year != 2024 {
		t.Errorf("expected Core Data ts to convert to year 2024, got %d", year)
	}
}

func TestEach_UnknownTransformErrors(t *testing.T) {
	// An invalid transform reference is a config error and must fail fast, before
	// any rows are processed — not silently drop every row.
	db := openDB(t,
		`CREATE TABLE t (id INTEGER, name TEXT, flag INTEGER, opt INTEGER)`,
		`INSERT INTO t VALUES (1, 'a', 1, NULL)`,
	)
	spec := baseSpec()
	spec.Fields[3].Transform = "does_not_exist" // ok field.

	emitted := 0
	_, err := Each(context.Background(), db, spec, Options{
		NewRecord: func() any { return &rec{} },
		Context:   map[string]string{"username": "alice"},
	}, func(any) error { emitted++; return nil })
	if err == nil {
		t.Fatal("expected an error for an unknown transform")
	}
	if emitted != 0 {
		t.Errorf("expected no records emitted, got %d", emitted)
	}
}

func TestResolveTransform_Truncate(t *testing.T) {
	tf, err := resolveTransform("truncate:200")
	if err != nil {
		t.Fatalf("resolveTransform: %v", err)
	}
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'a'
	}
	exact := string(long[:200])

	cases := []struct {
		name string
		in   any
		want string
	}{
		{"short string", "hello", "hello"},
		{"empty", "", ""},
		{"exact limit", exact, exact},
		{"over limit", string(long[:201]), exact + "..."},
		{"much longer blob", long, exact + "..."},
		{"json untouched", `{"theme":"dark"}`, `{"theme":"dark"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := tf(c.in)
			if err != nil {
				t.Fatalf("transform: %v", err)
			}
			if got.(string) != c.want {
				t.Errorf("truncate(%T len=%d) = %q, want %q", c.in, length(c.in), got, c.want)
			}
		})
	}
}

func TestResolveTransform_TruncateInvalid(t *testing.T) {
	if _, err := resolveTransform("truncate:notanumber"); err == nil {
		t.Error("expected error for invalid truncate length")
	}
}

func TestByteLengthTransform(t *testing.T) {
	tf := transforms["byte_length"]
	for _, in := range []any{[]byte("hello"), "hello"} {
		got, err := tf(in)
		if err != nil {
			t.Fatalf("byte_length(%T): %v", in, err)
		}
		if got.(int64) != 5 {
			t.Errorf("byte_length(%T) = %v, want 5", in, got)
		}
	}
}

// length reports the byte length of a string or []byte for test messages.
func length(v any) int {
	switch s := v.(type) {
	case string:
		return len(s)
	case []byte:
		return len(s)
	default:
		return -1
	}
}

// TestTransformsSyncWithDeclaredVocabulary guards that the validator's declared
// transform vocabulary (artifacts.TransformsForFormat) and this mapper's
// registry cannot drift: every declared name must resolve here, and every
// registry entry must be accepted by the validator.
func TestTransformsSyncWithDeclaredVocabulary(t *testing.T) {
	declared := artifacts.TransformsForFormat(artifacts.FormatSQLite)
	if len(declared) == 0 {
		t.Fatal("no transforms declared for sqlite")
	}
	for _, name := range declared {
		if _, err := resolveTransform(name); err != nil {
			t.Errorf("declared transform %q does not resolve in the table mapper: %v", name, err)
		}
	}
	for name := range transforms {
		if !artifacts.IsValidTransform(artifacts.FormatSQLite, name) {
			t.Errorf("registry transform %q is not declared in artifacts.TransformsForFormat", name)
		}
	}
	// The parameterized truncate form is valid on both sides.
	if !artifacts.IsValidTransform(artifacts.FormatSQLite, "truncate:10") {
		t.Error("validator rejects truncate:10")
	}
	if _, err := resolveTransform("truncate:10"); err != nil {
		t.Errorf("mapper rejects truncate:10: %v", err)
	}
}

// TestEach_NonTableSourceFailsFast guards the holders[-1] panic path: a field
// sourced like a plist field (key/derived) on a table extract is a config
// error and must fail before any row is scanned.
func TestEach_NonTableSourceFailsFast(t *testing.T) {
	db := openDB(t,
		`CREATE TABLE t (id INTEGER)`,
		`INSERT INTO t VALUES (1)`,
	)
	spec := artifacts.ExtractSpec{
		From:   "t",
		Fields: []artifacts.FieldSpec{{Name: "state", Derived: "x.y"}},
	}
	_, err := Each(context.Background(), db, spec, Options{
		NewRecord: func() any { return &rec{} },
	}, func(any) error { return nil })
	if err == nil {
		t.Fatal("expected a config error for a derived field on a table extract")
	}
}

// TestEach_Deriver exercises the field-level deriver escape hatch: a computed
// field reads another selected column from the row map.
func TestEach_Deriver(t *testing.T) {
	db := openDB(t,
		`CREATE TABLE t (id INTEGER, n INTEGER)`,
		`INSERT INTO t VALUES (1, 7)`,
		`INSERT INTO t VALUES (2, NULL)`,
	)
	type drec struct {
		ID    int64  `json:"id"`
		N     *int64 `json:"n,omitempty"`
		Label string `json:"label,omitempty"`
	}
	spec := artifacts.ExtractSpec{
		From:    "t",
		OrderBy: "id ASC",
		Fields: []artifacts.FieldSpec{
			{Name: "id", Column: "id"},
			{Name: "n", Column: "n"},
			{Name: "label", Derived: "label"},
		},
	}
	var got []*drec
	_, err := Each(context.Background(), db, spec, Options{
		NewRecord: func() any { return &drec{} },
		Derivers: map[string]Deriver{
			"label": func(row map[string]any) (any, error) {
				v := row["n"]
				if v == nil {
					return nil, nil // NULL → leave label empty.
				}
				n, _ := toInt64(v)
				return fmt.Sprintf("n=%d", n), nil
			},
		},
	}, func(r any) error { got = append(got, r.(*drec)); return nil })
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if got[0].Label != "n=7" {
		t.Errorf("row0 label = %q, want n=7", got[0].Label)
	}
	if got[1].Label != "" || got[1].N != nil {
		t.Errorf("row1 should have empty label and nil N for NULL, got %+v", got[1])
	}
}

func TestEach_UnknownDeriverFailsFast(t *testing.T) {
	db := openDB(t, `CREATE TABLE t (id INTEGER)`, `INSERT INTO t VALUES (1)`)
	spec := artifacts.ExtractSpec{
		From:   "t",
		Fields: []artifacts.FieldSpec{{Name: "x", Derived: "nope"}},
	}
	_, err := Each(context.Background(), db, spec, Options{
		NewRecord: func() any {
			return &struct {
				X string `json:"x"`
			}{}
		},
	}, func(any) error { return nil })
	if err == nil {
		t.Fatal("expected a config error for an unknown deriver")
	}
}

// TestBuildQueryValidatesIdentifiers verifies that buildQuery accepts the
// identifier forms real manifests produce and rejects anything that could break
// out of an identifier position into injected SQL (the F9 defensive guard for
// externally-sourced manifests).
func TestBuildQueryValidatesIdentifiers(t *testing.T) {
	// Valid: bare table, bare and qualified columns, raw clauses passed through.
	q, err := buildQuery(
		artifacts.ExtractSpec{
			From:    "history_items",
			Join:    "JOIN history_visits ON history_visits.history_item = history_items.id",
			Where:   "history_items.url IS NOT NULL",
			OrderBy: "history_visits.visit_time ASC",
		},
		[]string{"history_items.url", "history_visits.title", "value"},
	)
	if err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	want := "SELECT history_items.url, history_visits.title, value FROM history_items " +
		"JOIN history_visits ON history_visits.history_item = history_items.id " +
		"WHERE history_items.url IS NOT NULL ORDER BY history_visits.visit_time ASC"
	if q != want {
		t.Errorf("query =\n%q\nwant\n%q", q, want)
	}

	// Invalid FROM values must be rejected.
	badFrom := []string{
		"history_items; DROP TABLE users",
		"history_items WHERE 1=1",
		"hist ory",
		`"history_items"`,
		"",
		"users--",
	}
	for _, from := range badFrom {
		if _, err := buildQuery(artifacts.ExtractSpec{From: from}, []string{"id"}); err == nil {
			t.Errorf("FROM %q: expected error, got nil", from)
		}
	}

	// Invalid column references must be rejected.
	badCols := []string{
		"url) FROM secrets --",
		"a.b.c",
		"col; DELETE",
		"*",
		"count(*)",
	}
	for _, col := range badCols {
		if _, err := buildQuery(artifacts.ExtractSpec{From: "t"}, []string{col}); err == nil {
			t.Errorf("column %q: expected error, got nil", col)
		}
	}
}

// TestEach_CoverageGapWarning verifies the FR-MDC-2.4 coverage-gap warning:
// columns present in the primary table but mapped by no field are warned (once
// each), while mapped columns — including those referenced with a table
// qualifier — are not. Joined-table columns are out of scope by design.
func TestEach_CoverageGapWarning(t *testing.T) {
	db := openDB(t,
		`CREATE TABLE t (id INTEGER, name TEXT, flag INTEGER, opt INTEGER, secret TEXT, leftover TEXT)`,
		`INSERT INTO t VALUES (1, 'a', 1, 7, 's', 'l')`,
	)

	var gaps []string
	spec := baseSpec()
	// Map id via a table-qualified reference to confirm qualified refs count.
	spec.Fields[len(spec.Fields)-1] = artifacts.FieldSpec{Name: "id", Column: "t.id"}

	_, err := Each(context.Background(), db, spec, Options{
		NewRecord: func() any { return &rec{} },
		Context:   map[string]string{"username": "u"},
		Warnf: func(format string, args ...any) {
			msg := fmt.Sprintf(format, args...)
			if strings.Contains(msg, "coverage gap") {
				gaps = append(gaps, msg)
			}
		},
	}, func(any) error { return nil })
	if err != nil {
		t.Fatalf("Each: %v", err)
	}

	// One aggregated warning per table, naming both unmapped columns.
	if len(gaps) != 1 {
		t.Fatalf("expected 1 aggregated coverage-gap warning, got %d: %v", len(gaps), gaps)
	}
	for _, want := range []string{"secret", "leftover"} {
		if !strings.Contains(gaps[0], want) {
			t.Errorf("coverage-gap warning missing %q; got %q", want, gaps[0])
		}
	}
	// Mapped columns (including the table-qualified "t.id") must not appear.
	for _, mapped := range []string{"name", "flag", "opt"} {
		// Use word-ish boundaries: the message lists columns comma-separated.
		for _, tok := range strings.Split(gaps[0], ": ") {
			if tok == mapped {
				t.Errorf("mapped column %q should not be reported; got %q", mapped, gaps[0])
			}
		}
	}
}

// TestUnixRFC3339Transform verifies the unix_rfc3339 transform matches the
// native time.Unix(sec,nsec).UTC().Format(time.RFC3339) used by dropbox.
func TestUnixRFC3339Transform(t *testing.T) {
	tr, err := resolveTransform(artifacts.TransformUnixRFC3339)
	if err != nil {
		t.Fatalf("resolveTransform: %v", err)
	}
	cases := []struct {
		in   any
		want string
	}{
		{int64(0), "1970-01-01T00:00:00Z"},
		{float64(1700000000), "2023-11-14T22:13:20Z"},
		{float64(1700000000.75), "2023-11-14T22:13:20Z"}, // RFC3339 drops sub-second.
	}
	for _, c := range cases {
		got, err := tr(c.in)
		if err != nil {
			t.Errorf("unix_rfc3339(%v): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("unix_rfc3339(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := tr("not-a-number"); err == nil {
		t.Error("unix_rfc3339(string): expected error, got nil")
	}
}

// TestNullifyEmptyTransform verifies "" maps to nil while non-empty values pass
// through (the dropbox file_id empty-string semantic).
func TestNullifyEmptyTransform(t *testing.T) {
	tr, err := resolveTransform(artifacts.TransformNullifyEmpty)
	if err != nil {
		t.Fatalf("resolveTransform: %v", err)
	}
	if got, _ := tr(""); got != nil {
		t.Errorf(`nullify_empty("") = %v, want nil`, got)
	}
	if got, _ := tr([]byte{}); got != nil {
		t.Errorf("nullify_empty([]byte{}) = %v, want nil", got)
	}
	if got, _ := tr("id:abc"); got != "id:abc" {
		t.Errorf(`nullify_empty("id:abc") = %v, want "id:abc"`, got)
	}
}
