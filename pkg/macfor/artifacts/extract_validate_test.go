package artifacts

import (
	"strings"
	"testing"
)

// manifestWithExtract builds a minimal valid manifest whose single artifact
// carries the given format, extract block, and parser name.
func manifestWithExtract(format string, extract *ExtractSpec, parser string) *Manifest {
	return &Manifest{
		SchemaVersion: "1.0",
		Plugin: Plugin{
			ID:          "test.plugin",
			Name:        "Test",
			Description: "Test plugin",
			Version:     "1.0.0",
			Categories:  []string{"test"},
			Platforms:   []Platform{{OS: "darwin"}},
		},
		Artifacts: []Artifact{
			{
				ID:       "thing",
				Name:     "Thing",
				Category: CategoryHistory,
				Format:   format,
				Parser:   parser,
				Extract:  extract,
				Paths:    []PathSpec{{Pattern: "~/thing"}},
			},
		},
	}
}

// expectInvalid asserts validation fails with a message containing want.
func expectInvalid(t *testing.T, m *Manifest, want string) {
	t.Helper()
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("expected error containing %q, got: %v", want, err)
	}
}

func TestValidateExtract_ValidSQLite(t *testing.T) {
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		From: "items",
		Fields: []FieldSpec{
			{Name: "type", Const: "thing"},
			{Name: "user", Context: "username"},
			{Name: "url", Column: "items.url"},
			{Name: "when", Column: "items.ts", Transform: "coredata_timestamp"},
			{Name: "size", Column: "items.data", Transform: "byte_length"},
			{Name: "preview", Column: "items.data", Transform: "truncate:200"},
		},
	}, "")
	if err := Validate(m); err != nil {
		t.Errorf("expected valid sqlite extract, got: %v", err)
	}
}

func TestValidateExtract_ValidPlist(t *testing.T) {
	m := manifestWithExtract(FormatBinaryPlist, &ExtractSpec{
		Records: "Items",
		Fields: []FieldSpec{
			{Name: "type", Const: "thing"},
			{Name: "user", Context: "username"},
			{Name: "label", Key: "Label"},
			{Name: "file", Key: "Path", Transform: "basename"},
			{Name: "preview", Key: "Blob", Transform: "truncate:64"},
			{Name: "state", Derived: "test.state"},
		},
	}, "")
	if err := Validate(m); err != nil {
		t.Errorf("expected valid plist extract, got: %v", err)
	}
}

func TestValidateExtract_UnknownTransform(t *testing.T) {
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		From:   "items",
		Fields: []FieldSpec{{Name: "url", Column: "items.url", Transform: "does_not_exist"}},
	}, "")
	expectInvalid(t, m, `unknown transform "does_not_exist"`)
}

func TestValidateExtract_TransformWrongFormat(t *testing.T) {
	// basename is a plist-mapper transform; the table mapper does not have it.
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		From:   "items",
		Fields: []FieldSpec{{Name: "file", Column: "items.path", Transform: "basename"}},
	}, "")
	expectInvalid(t, m, `unknown transform "basename"`)

	// coredata_timestamp is a table-mapper transform.
	m = manifestWithExtract(FormatBinaryPlist, &ExtractSpec{
		Records: "Items",
		Fields:  []FieldSpec{{Name: "when", Key: "Date", Transform: "coredata_timestamp"}},
	}, "")
	expectInvalid(t, m, `unknown transform "coredata_timestamp"`)
}

func TestValidateExtract_InvalidTruncate(t *testing.T) {
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		From:   "items",
		Fields: []FieldSpec{{Name: "p", Column: "items.data", Transform: "truncate:notanumber"}},
	}, "")
	expectInvalid(t, m, "unknown transform")
}

func TestValidateExtract_SourceKindPerFormat(t *testing.T) {
	// A plist key on a sqlite extract previously passed validation and made
	// the table mapper panic; it must be a validation error.
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		From:   "items",
		Fields: []FieldSpec{{Name: "label", Key: "Label"}},
	}, "")
	expectInvalid(t, m, "not valid for sqlite extraction (use column)")

	// Derivers are valid on both mappers (table and plist) — a sqlite extract
	// with a derived field passes validation.
	m = manifestWithExtract(FormatSQLite, &ExtractSpec{
		From:   "items",
		Fields: []FieldSpec{{Name: "id", Column: "items.id"}, {Name: "state", Derived: "test.state"}},
	}, "")
	if err := Validate(m); err != nil {
		t.Errorf("derived field on sqlite extract should be valid, got: %v", err)
	}

	// A SQL column on a plist extract is silently empty at runtime; reject it.
	m = manifestWithExtract(FormatBinaryPlist, &ExtractSpec{
		Records: "Items",
		Fields:  []FieldSpec{{Name: "url", Column: "items.url"}},
	}, "")
	expectInvalid(t, m, "not valid for binary_plist extraction (use key)")
}

func TestValidateExtract_UnsupportedFormat(t *testing.T) {
	m := manifestWithExtract(FormatBinary, &ExtractSpec{
		Fields: []FieldSpec{{Name: "x", Key: "X"}},
	}, "")
	expectInvalid(t, m, "extract is not supported for format binary")
}

func TestValidateExtract_FromRequiredForSQLite(t *testing.T) {
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		Fields: []FieldSpec{{Name: "url", Column: "items.url"}},
	}, "")
	expectInvalid(t, m, "required for sqlite extraction")
}

func TestValidateExtract_EmptyFields(t *testing.T) {
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{From: "items"}, "")
	expectInvalid(t, m, "at least one field required")
}

func TestValidateExtract_MissingName(t *testing.T) {
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		From:   "items",
		Fields: []FieldSpec{{Column: "items.url"}},
	}, "")
	expectInvalid(t, m, ".name: required")
}

func TestValidateExtract_NoSource(t *testing.T) {
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		From:   "items",
		Fields: []FieldSpec{{Name: "url", Transform: "byte_length"}},
	}, "")
	expectInvalid(t, m, "needs a source")
}

func TestValidateExtract_MultipleSources(t *testing.T) {
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		From:   "items",
		Fields: []FieldSpec{{Name: "url", Column: "items.url", Const: "x"}},
	}, "")
	expectInvalid(t, m, "mutually exclusive")
}

func TestValidateExtract_ParserAndExtractExclusive(t *testing.T) {
	m := manifestWithExtract(FormatSQLite, &ExtractSpec{
		From:   "items",
		Fields: []FieldSpec{{Name: "url", Column: "items.url"}},
	}, "test.native")
	expectInvalid(t, m, "parser and extract are mutually exclusive")
}

func TestIsValidTransform(t *testing.T) {
	cases := []struct {
		format, name string
		want         bool
	}{
		{FormatSQLite, "coredata_timestamp", true},
		{FormatSQLite, "bool_from_int", true},
		{FormatSQLite, "byte_length", true},
		{FormatSQLite, "truncate:200", true},
		{FormatSQLite, "basename", false},
		{FormatBinaryPlist, "basename", true},
		{FormatBinaryPlist, "truncate:0", true},
		{FormatBinaryPlist, "coredata_timestamp", false},
		{FormatSQLite, "truncate:-1", false},
		{FormatSQLite, "truncate:abc", false},
		{FormatBinary, "basename", false},
	}
	for _, c := range cases {
		if got := IsValidTransform(c.format, c.name); got != c.want {
			t.Errorf("IsValidTransform(%s, %s) = %v, want %v", c.format, c.name, got, c.want)
		}
	}
}
