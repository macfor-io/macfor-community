// Package tablemapper executes a declarative artifacts.ExtractSpec against a
// SQLite database, populating a caller-provided record struct per row. It
// replaces hand-written SELECT queries and row-scanning code with a manifest
// driven mapping while keeping the record's typed schema (a Go struct) as the
// output contract — so the emitted JSON is identical to the hand-written parser.
//
// Only tabular extraction is handled here; opaque binary formats remain native
// Go (see the parser-kind registry described in the manifest-driven collection
// spec).
package tablemapper

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/sqlite"
	"github.com/macfor-io/macfor-community/pkg/macfor/timestamp"
)

// Transform converts a raw column value into the value assigned to a record
// field. Transforms are referenced by name from FieldSpec.Transform.
type Transform func(v any) (any, error)

// transforms is the closed registry of named value transformations.
var transforms = map[string]Transform{
	// coredata_timestamp converts a Core Data timestamp (seconds since
	// 2001-01-01 UTC) to a time.Time. Accepts float64 or int64.
	artifacts.TransformCoreDataTimestamp: func(v any) (any, error) {
		f, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("coredata_timestamp: expected number, got %T", v)
		}
		return timestamp.CoreDataToTime(f), nil
	},
	// bool_from_int maps an integer to a boolean using the convention 1 == true
	// (matching Safari's load_successful column).
	artifacts.TransformBoolFromInt: func(v any) (any, error) {
		i, ok := toInt64(v)
		if !ok {
			return nil, fmt.Errorf("bool_from_int: expected integer, got %T", v)
		}
		return i == 1, nil
	},
	// byte_length returns the length in bytes of a string or blob value.
	artifacts.TransformByteLength: func(v any) (any, error) {
		switch s := v.(type) {
		case []byte:
			return int64(len(s)), nil
		case string:
			return int64(len(s)), nil
		default:
			return nil, fmt.Errorf("byte_length: expected string or bytes, got %T", v)
		}
	},
	// coredata_rfc3339 converts a Core Data timestamp (seconds since
	// 2001-01-01 UTC) to an RFC 3339 string. Accepts float64 or int64.
	artifacts.TransformCoreDataRFC3339: func(v any) (any, error) {
		f, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("coredata_rfc3339: expected number, got %T", v)
		}
		return timestamp.CoreDataToTime(f).Format(time.RFC3339), nil
	},
	// unix_rfc3339 converts a Unix timestamp (seconds since 1970-01-01 UTC, with
	// an optional fractional part) to an RFC 3339 UTC string. Accepts float64 or
	// int64. Mirrors time.Unix(sec, nsec).UTC().Format(time.RFC3339).
	artifacts.TransformUnixRFC3339: func(v any) (any, error) {
		f, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("unix_rfc3339: expected number, got %T", v)
		}
		sec := int64(f)
		nsec := int64((f - float64(sec)) * 1e9)
		return time.Unix(sec, nsec).UTC().Format(time.RFC3339), nil
	},
	// nullify_empty maps an empty string to nil (leaving a pointer field unset),
	// passing any non-empty string through unchanged. Mirrors the common native
	// idiom of treating an empty column the same as NULL. A NULL column never
	// reaches a transform, so it already maps to nil.
	artifacts.TransformNullifyEmpty: func(v any) (any, error) {
		switch s := v.(type) {
		case string:
			if s == "" {
				return nil, nil
			}
			return s, nil
		case []byte:
			if len(s) == 0 {
				return nil, nil
			}
			return string(s), nil
		default:
			return v, nil
		}
	},
	// base64 encodes a blob (or string) as standard base64.
	artifacts.TransformBase64: func(v any) (any, error) {
		switch s := v.(type) {
		case []byte:
			return base64.StdEncoding.EncodeToString(s), nil
		case string:
			return base64.StdEncoding.EncodeToString([]byte(s)), nil
		default:
			return nil, fmt.Errorf("base64: expected string or bytes, got %T", v)
		}
	},
}

// Deriver computes a field value from a row's selected column values, keyed by
// the column reference as written in the manifest (e.g. "LSQuarantineTypeNumber"
// or "history_items.url"). It is the table mapper's field-level escape hatch for
// computed values that are not a single-value transform — mirroring the plist
// mapper's Deriver. A deriver may only read columns that some field selects.
type Deriver func(row map[string]any) (any, error)

// resolveTransform returns the Transform for a transform reference. It handles
// both registry names (e.g. "coredata_timestamp") and the parameterized form
// "truncate:N", which truncates a string/blob to N bytes, appending "..." when
// truncation occurs.
func resolveTransform(name string) (Transform, error) {
	if tf, ok := transforms[name]; ok {
		return tf, nil
	}
	if arg, ok := strings.CutPrefix(name, "truncate:"); ok {
		n, err := strconv.Atoi(arg)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid truncate length %q", arg)
		}
		return func(v any) (any, error) {
			s := toString(v)
			if len(s) <= n {
				return s, nil
			}
			return s[:n] + "...", nil
		}, nil
	}
	return nil, fmt.Errorf("unknown transform %q", name)
}

// Options configures a mapping run.
type Options struct {
	// NewRecord returns a fresh pointer to the record struct to populate
	// (e.g. func() any { return &HistoryVisit{} }).
	NewRecord func() any

	// Context supplies values for context-sourced fields, keyed by the name
	// referenced in FieldSpec.Context (e.g. "username", "source_file", "origin").
	Context map[string]string

	// Derivers maps a FieldSpec.Derived name to its compute function (the
	// field-level escape hatch for computed values).
	Derivers map[string]Deriver

	// Warnf logs a non-fatal warning (drift, coverage gaps, bad rows).
	Warnf func(format string, args ...any)
}

// Each runs spec against db, invoking emit once per successfully mapped row.
// Returns the number of records emitted. Per-row scan/transform errors are
// logged via opts.Warnf and skipped — they never abort the run, preserving the
// error-aggregation contract of the native parsers.
func Each(ctx context.Context, db *sql.DB, spec artifacts.ExtractSpec, opts Options, emit func(rec any) error) (int, error) {
	if opts.NewRecord == nil {
		return 0, fmt.Errorf("tablemapper: Options.NewRecord is required")
	}
	warnf := opts.Warnf
	if warnf == nil {
		warnf = func(string, ...any) {}
	}

	// Resolve the field set, dropping optional fields whose column is absent
	// and binding transforms/derivers up front (fail fast on bad references).
	active, selectCols, err := resolveFields(db, spec, opts, warnf)
	if err != nil {
		return 0, err
	}
	if len(selectCols) == 0 {
		return 0, fmt.Errorf("tablemapper: extract for %q selects no columns", spec.From)
	}

	// Build the field-name → struct field index map from the record type.
	fieldIndex, err := buildFieldIndex(opts.NewRecord())
	if err != nil {
		return 0, err
	}

	warnCoverageGaps(db, spec, warnf)

	query, err := buildQuery(spec, selectCols)
	if err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx, query) //nolint:rowserrcheck // checked via rows.Err() below.
	if err != nil {
		return 0, fmt.Errorf("tablemapper: querying %q: %w", spec.From, err)
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		holders := make([]any, len(selectCols))
		ptrs := make([]any, len(selectCols))
		for i := range holders {
			ptrs[i] = &holders[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			warnf("tablemapper: scanning row: %v", err)
			continue
		}

		// Build a column→value map of the selected columns for any derivers.
		var rowCols map[string]any
		if len(opts.Derivers) > 0 {
			rowCols = make(map[string]any, len(selectCols))
			for i, col := range selectCols {
				rowCols[col] = holders[i]
			}
		}

		rec := opts.NewRecord()
		rv := reflect.ValueOf(rec).Elem()
		if err := populate(rv, fieldIndex, active, holders, opts.Context, rowCols); err != nil {
			warnf("tablemapper: mapping row: %v", err)
			continue
		}

		if err := emit(rec); err != nil {
			warnf("tablemapper: emitting record: %v", err)
			continue
		}
		count++
	}

	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("tablemapper: iterating rows: %w", err)
	}
	return count, nil
}

// activeField pairs a field spec with its position in the scanned column slice
// (-1 for non-column fields) and its resolved transform/deriver (nil if none).
type activeField struct {
	spec      artifacts.FieldSpec
	scanPos   int
	transform Transform
	deriver   Deriver
}

// resolveFields determines which fields are active (dropping optional fields
// whose column is physically absent), resolves each field's transform up front
// (failing fast on an invalid reference), and returns the ordered SELECT columns.
func resolveFields(db *sql.DB, spec artifacts.ExtractSpec, opts Options, warnf func(string, ...any)) ([]activeField, []string, error) {
	colCache := map[string]map[string]bool{}
	columnPresent := func(qualified string) (bool, error) {
		table, col := splitColumn(qualified, spec.From)
		set, ok := colCache[table]
		if !ok {
			cols, err := sqlite.GetTableColumns(db, table)
			if err != nil {
				return false, err
			}
			set = map[string]bool{}
			for _, c := range cols {
				set[c] = true
			}
			colCache[table] = set
		}
		return set[col], nil
	}

	var active []activeField
	var selectCols []string
	for _, f := range spec.Fields {
		af := activeField{spec: f, scanPos: -1}

		// The table mapper sources values from columns, constants, context, or
		// derivers. Anything else is a config error: fail fast rather than
		// indexing a non-existent scan slot.
		if f.Column == "" && f.Const == "" && f.Context == "" && f.Derived == "" {
			return nil, nil, fmt.Errorf("tablemapper: field %q has no column, const, context, or derived source", f.Name)
		}

		if f.Derived != "" {
			d, ok := opts.Derivers[f.Derived]
			if !ok {
				return nil, nil, fmt.Errorf("tablemapper: field %q references unknown deriver %q", f.Name, f.Derived)
			}
			af.deriver = d
		}

		if f.Column != "" {
			if f.Optional {
				present, err := columnPresent(f.Column)
				if err != nil {
					return nil, nil, err
				}
				if !present {
					warnf("tablemapper: optional column %q absent; field %q left empty", f.Column, f.Name)
					continue
				}
			}
			af.scanPos = len(selectCols)
			selectCols = append(selectCols, f.Column)
		}

		if f.Transform != "" {
			tf, err := resolveTransform(f.Transform)
			if err != nil {
				return nil, nil, fmt.Errorf("field %q: %w", f.Name, err)
			}
			af.transform = tf
		}

		active = append(active, af)
	}
	return active, selectCols, nil
}

// sqlIdentRe matches a bare SQL identifier: a leading letter or underscore
// followed by letters, digits, or underscores. Deliberately conservative — it
// rejects quotes, whitespace, dots, and punctuation that could break out of an
// identifier position into injected SQL.
var sqlIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateIdentifier returns an error if s is not a bare SQL identifier.
func validateIdentifier(s string) error {
	if !sqlIdentRe.MatchString(s) {
		return fmt.Errorf("invalid SQL identifier %q", s)
	}
	return nil
}

// validateColumnRef returns an error if s is not a bare identifier or a
// table-qualified identifier ("table.column"). These are the only column-
// reference forms the manifest produces.
func validateColumnRef(s string) error {
	for _, part := range strings.Split(s, ".") {
		if err := validateIdentifier(part); err != nil {
			return fmt.Errorf("invalid column reference %q: %w", s, err)
		}
	}
	if strings.Count(s, ".") > 1 {
		return fmt.Errorf("invalid column reference %q: too many qualifiers", s)
	}
	return nil
}

// warnCoverageGaps emits a warning for each column physically present in the
// primary table (spec.From) that no manifest field references — the "coverage
// gap" half of FR-MDC-2.4 (spec §4.2 step 6). The schema-drift half (a manifest
// field whose column is absent) is handled in resolveFields.
//
// Scope is deliberately limited to the primary table: joined tables routinely
// expose many columns the extract never intends to map, so warning on them
// would be pure noise. A column mapped to ANY field counts as covered, even an
// optional field whose column happens to be absent on this source. Best-effort:
// if the table's columns can't be read, the warning is silently skipped (the
// query path surfaces the real error).
func warnCoverageGaps(db *sql.DB, spec artifacts.ExtractSpec, warnf func(string, ...any)) {
	present, err := sqlite.GetTableColumns(db, spec.From)
	if err != nil {
		return
	}

	mapped := make(map[string]bool, len(spec.Fields))
	for i := range spec.Fields {
		col := spec.Fields[i].Column
		if col == "" {
			continue
		}
		if table, name := splitColumn(col, spec.From); table == spec.From {
			mapped[name] = true
		}
	}

	var unmapped []string
	for _, col := range present {
		if !mapped[col] {
			unmapped = append(unmapped, col)
		}
	}
	if len(unmapped) > 0 {
		// One aggregated warning per table keeps the signal readable rather than
		// emitting one line per column (a deliberately-partial extract can leave
		// many columns unmapped).
		warnf("tablemapper: coverage gap: table %q has %d column(s) not mapped by any field: %s",
			spec.From, len(unmapped), strings.Join(unmapped, ", "))
	}
}

// buildQuery assembles the SELECT statement from the spec and selected columns.
//
// The table name and every column reference are validated as SQL identifiers
// before interpolation, so a malformed (or, once manifest packs land, an
// externally-sourced) manifest cannot inject SQL through those positions.
//
// TRUST BOUNDARY: spec.Join, spec.Where, and spec.OrderBy are raw SQL fragments
// by design (e.g. a full JOIN clause, a WHERE body, an "col DESC" ordering) and
// are interpolated verbatim. They are safe only with a reviewed/trusted
// manifest. The out-of-band manifest-pack work (Epic MDC-4) must constrain or
// parse these clauses — or forbid them on untrusted packs — before driving a
// query from an untrusted source.
func buildQuery(spec artifacts.ExtractSpec, selectCols []string) (string, error) {
	if err := validateIdentifier(spec.From); err != nil {
		return "", fmt.Errorf("tablemapper: FROM %w", err)
	}
	for _, col := range selectCols {
		if err := validateColumnRef(col); err != nil {
			return "", fmt.Errorf("tablemapper: %w", err)
		}
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(selectCols, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(spec.From)
	if spec.Join != "" {
		sb.WriteString(" ")
		sb.WriteString(spec.Join)
	}
	if spec.Where != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(spec.Where)
	}
	if spec.OrderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(spec.OrderBy)
	}
	return sb.String(), nil
}

// populate sets each active field on the record struct from the scanned values,
// constants, context, or a deriver, applying any transform.
func populate(rv reflect.Value, fieldIndex map[string]int, active []activeField, holders []any, contextVals map[string]string, rowCols map[string]any) error {
	for _, af := range active {
		var raw any
		switch {
		case af.spec.Const != "":
			raw = af.spec.Const
		case af.spec.Context != "":
			raw = contextVals[af.spec.Context]
		case af.deriver != nil:
			v, err := af.deriver(rowCols)
			if err != nil {
				return fmt.Errorf("deriving %q: %w", af.spec.Name, err)
			}
			raw = v
		default:
			raw = holders[af.scanPos]
		}

		if af.transform != nil && raw != nil {
			v, err := af.transform(raw)
			if err != nil {
				return err
			}
			raw = v
		}

		idx, ok := fieldIndex[af.spec.Name]
		if !ok {
			return fmt.Errorf("record has no field for %q", af.spec.Name)
		}
		if err := setField(rv.Field(idx), raw); err != nil {
			return fmt.Errorf("field %q: %w", af.spec.Name, err)
		}
	}
	return nil
}

// buildFieldIndex maps JSON field names (or field names) to struct field
// indexes for the record type behind rec (a pointer to a struct).
func buildFieldIndex(rec any) (map[string]int, error) {
	t := reflect.TypeOf(rec)
	if t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("tablemapper: NewRecord must return a pointer to a struct, got %T", rec)
	}
	t = t.Elem()
	index := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := f.Name
		if tag, ok := f.Tag.Lookup("json"); ok {
			if n := strings.Split(tag, ",")[0]; n != "" && n != "-" {
				name = n
			}
		}
		index[name] = i
	}
	return index, nil
}

// setField assigns val to the struct field, coercing common SQLite-driver types
// (int64, float64, string, []byte, nil, time.Time) into the field's type,
// including pointer fields for nullable columns.
func setField(fv reflect.Value, val any) error {
	if val == nil {
		return nil // Leave zero value (nil pointer / empty).
	}

	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		if err := setField(p.Elem(), val); err != nil {
			return err
		}
		fv.Set(p)
		return nil
	}

	switch fv.Kind() { //nolint:exhaustive // default handles all remaining kinds (incl. time.Time and other concrete types).
	case reflect.String:
		fv.SetString(toString(val))
	case reflect.Bool:
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected bool, got %T", val)
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, ok := toInt64(val)
		if !ok {
			return fmt.Errorf("expected integer, got %T", val)
		}
		fv.SetInt(i)
	case reflect.Float32, reflect.Float64:
		f, ok := toFloat(val)
		if !ok {
			return fmt.Errorf("expected float, got %T", val)
		}
		fv.SetFloat(f)
	default:
		// time.Time and other concrete types assigned directly.
		vv := reflect.ValueOf(val)
		if vv.Type().AssignableTo(fv.Type()) {
			fv.Set(vv)
			return nil
		}
		if _, ok := val.(time.Time); ok && fv.Type() == reflect.TypeOf(time.Time{}) {
			fv.Set(vv)
			return nil
		}
		return fmt.Errorf("cannot assign %T to %s", val, fv.Type())
	}
	return nil
}

// splitColumn splits a (possibly table-qualified) column reference into table
// and column, defaulting to the primary table when unqualified.
func splitColumn(qualified, defaultTable string) (table, col string) {
	if i := strings.IndexByte(qualified, '.'); i >= 0 {
		return qualified[:i], qualified[i+1:]
	}
	return defaultTable, qualified
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}
