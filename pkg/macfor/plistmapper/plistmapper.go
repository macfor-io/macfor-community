// Package plistmapper executes a declarative artifacts.ExtractSpec against a
// property-list file, populating a caller-provided record struct per element.
// It is the plist counterpart of the table mapper: the record-array location
// and the key-to-field mapping live in the manifest, while the record's typed
// schema stays a Go struct.
//
// Cross-field logic that cannot be expressed as a single-value transform (for
// example a download's complete/incomplete state derived from two byte counts)
// is handled by a named Deriver supplied via Options — the field-level escape
// hatch.
package plistmapper

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"howett.net/plist"
)

// Deriver computes a field value from a whole plist element (a decoded dict).
type Deriver func(element map[string]any) (any, error)

// Options configures a mapping run.
type Options struct {
	// NewRecord returns a fresh pointer to the record struct to populate.
	NewRecord func() any

	// Context supplies values for context-sourced fields (e.g. "username",
	// "source_file").
	Context map[string]string

	// Derivers maps a FieldSpec.Derived name to its compute function.
	Derivers map[string]Deriver

	// Warnf logs a non-fatal warning (bad element, derive error).
	Warnf func(format string, args ...any)
}

// Each decodes data, locates the record array per spec, and invokes emit once
// per element mapped into a fresh record. Configuration errors (unknown
// transform or deriver references) fail fast before any element is processed;
// per-element data errors are logged via Warnf and skipped — they never abort
// the run.
func Each(ctx context.Context, data []byte, spec artifacts.ExtractSpec, opts Options, emit func(rec any) error) (int, error) {
	if opts.NewRecord == nil {
		return 0, fmt.Errorf("plistmapper: Options.NewRecord is required")
	}
	warnf := opts.Warnf
	if warnf == nil {
		warnf = func(string, ...any) {}
	}

	// Resolve transform and deriver references up front: a bad reference is a
	// manifest/config error and must not silently drop every record.
	fields, err := resolveFields(spec, opts)
	if err != nil {
		return 0, err
	}

	var root any
	if _, err := plist.Unmarshal(data, &root); err != nil {
		return 0, fmt.Errorf("plistmapper: decoding plist: %w", err)
	}

	elements, err := recordElements(root, spec.Records)
	if err != nil {
		return 0, err
	}

	fieldIndex, err := buildFieldIndex(opts.NewRecord())
	if err != nil {
		return 0, err
	}

	count := 0
	for _, raw := range elements {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}
		element, ok := raw.(map[string]any)
		if !ok {
			warnf("plistmapper: skipping non-dict element of type %T", raw)
			continue
		}

		rec := opts.NewRecord()
		rv := reflect.ValueOf(rec).Elem()
		if err := populate(rv, fieldIndex, fields, element, opts); err != nil {
			warnf("plistmapper: mapping element: %v", err)
			continue
		}

		if err := emit(rec); err != nil {
			warnf("plistmapper: emitting record: %v", err)
			continue
		}
		count++
	}
	return count, nil
}

// resolvedField pairs a FieldSpec with its pre-resolved transform and deriver.
type resolvedField struct {
	spec      artifacts.FieldSpec
	transform transform
	deriver   Deriver
}

// resolveFields resolves every field's transform and deriver references,
// failing fast on an invalid reference.
func resolveFields(spec artifacts.ExtractSpec, opts Options) ([]resolvedField, error) {
	fields := make([]resolvedField, 0, len(spec.Fields))
	for _, f := range spec.Fields {
		rf := resolvedField{spec: f}

		// The plist mapper sources values from dict keys, constants, context,
		// or derivers; a SQL column reference is a config error.
		if f.Column != "" {
			return nil, fmt.Errorf("plistmapper: field %q uses column %q; the plist mapper sources from keys", f.Name, f.Column)
		}

		if f.Derived != "" {
			d, ok := opts.Derivers[f.Derived]
			if !ok {
				return nil, fmt.Errorf("plistmapper: field %q references unknown deriver %q", f.Name, f.Derived)
			}
			rf.deriver = d
		}

		if f.Transform != "" {
			tf, err := resolveTransform(f.Transform)
			if err != nil {
				return nil, fmt.Errorf("plistmapper: field %q: %w", f.Name, err)
			}
			rf.transform = tf
		}

		fields = append(fields, rf)
	}
	return fields, nil
}

// recordElements resolves the slice of element dicts from the decoded root.
// When recordsKey is set, it indexes a root dictionary; otherwise (or as a
// fallback) a root array is used directly.
func recordElements(root any, recordsKey string) ([]any, error) {
	if arr, ok := root.([]any); ok {
		return arr, nil
	}
	dict, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("plistmapper: plist root is neither array nor dictionary (%T)", root)
	}
	if recordsKey == "" {
		return nil, fmt.Errorf("plistmapper: plist root is a dictionary but no records key was given")
	}
	switch v := dict[recordsKey].(type) {
	case nil:
		return nil, nil // Key absent — zero records (valid, e.g. empty history).
	case []any:
		return v, nil
	default:
		return nil, fmt.Errorf("plistmapper: records key %q is not an array (%T)", recordsKey, v)
	}
}

// populate sets each field on the record from the element, constants, context,
// or a deriver, applying any pre-resolved transform.
func populate(rv reflect.Value, fieldIndex map[string]int, fields []resolvedField, element map[string]any, opts Options) error {
	for _, rf := range fields {
		f := rf.spec

		var raw any
		switch {
		case f.Const != "":
			raw = f.Const
		case f.Context != "":
			raw = opts.Context[f.Context]
		case f.Derived != "":
			v, err := rf.deriver(element)
			if err != nil {
				return fmt.Errorf("deriving %q: %w", f.Name, err)
			}
			raw = v
		default:
			raw = element[f.Key]
		}

		if rf.transform != nil && raw != nil {
			v, err := rf.transform(raw)
			if err != nil {
				return err
			}
			raw = v
		}

		idx, ok := fieldIndex[f.Name]
		if !ok {
			return fmt.Errorf("record has no field for %q", f.Name)
		}
		if err := setField(rv.Field(idx), raw); err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
	}
	return nil
}

// transform converts a sourced plist value into a record field value.
type transform func(v any) (any, error)

// resolveTransform returns the transform for the given reference. Plist values
// arrive already typed (string/int64/float64/bool/time.Time), so the set is
// small: "basename" and the parameterized "truncate:N".
func resolveTransform(name string) (transform, error) {
	switch {
	case name == artifacts.TransformBasename:
		return func(v any) (any, error) { return filepath.Base(toString(v)), nil }, nil
	case strings.HasPrefix(name, "truncate:"):
		n, err := strconv.Atoi(strings.TrimPrefix(name, "truncate:"))
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid truncate length in %q", name)
		}
		return func(v any) (any, error) {
			s := toString(v)
			if len(s) <= n {
				return s, nil
			}
			return s[:n] + "...", nil
		}, nil
	default:
		return nil, fmt.Errorf("unknown transform %q", name)
	}
}

// buildFieldIndex maps JSON field names (or field names) to struct field
// indexes for the record type behind rec (a pointer to a struct).
func buildFieldIndex(rec any) (map[string]int, error) {
	t := reflect.TypeOf(rec)
	if t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("plistmapper: NewRecord must return a pointer to a struct, got %T", rec)
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

// setField assigns val to the struct field, coercing the plist-decoded value
// types (string, int64, uint64, float64, bool, time.Time, []byte) into the
// field's type, including pointer fields and string-typed enums.
func setField(fv reflect.Value, val any) error {
	if val == nil {
		return nil // Leave zero value.
	}

	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		if err := setField(p.Elem(), val); err != nil {
			return err
		}
		fv.Set(p)
		return nil
	}

	switch fv.Kind() { //nolint:exhaustive // default handles time.Time and other concrete/string-defined types.
	case reflect.String:
		// Assign directly when the value is itself a string-kinded type (e.g. a
		// derived enum like DownloadState); otherwise stringify.
		if reflect.TypeOf(val).Kind() == reflect.String {
			fv.SetString(reflect.ValueOf(val).String())
		} else {
			fv.SetString(toString(val))
		}
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

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
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
	case uint64:
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
