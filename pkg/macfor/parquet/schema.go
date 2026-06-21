package parquet

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// timeType is the reflect.Type for time.Time, treated as a leaf type.
var timeType = reflect.TypeOf(time.Time{})

// schemaTypeJSONString is the schema type for JSON-serialized values.
const schemaTypeJSONString = "json_string"

// Schema type names for the primitive Go kinds the writer supports.
const (
	schemaTypeBool      = "bool"
	schemaTypeInt32     = "int32"
	schemaTypeInt64     = "int64"
	schemaTypeUint32    = "uint32"
	schemaTypeFloat64   = "float64"
	schemaTypeStringVal = "string"
)

// resolveColumnName determines the column name for a struct field.
// Priority: parquet tag > json tag (stripping options) > field name.
// Returns empty string and false if the field should be skipped.
func resolveColumnName(f reflect.StructField) (string, bool) {
	// Skip unexported fields.
	if !f.IsExported() {
		return "", false
	}

	// Check parquet tag first.
	if tag, ok := f.Tag.Lookup("parquet"); ok {
		name := strings.Split(tag, ",")[0]
		if name != "" && name != "-" {
			return name, true
		}
		if name == "-" {
			return "", false
		}
	}

	// Check json tag.
	if tag, ok := f.Tag.Lookup("json"); ok {
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			return "", false
		}
		if name != "" {
			return name, true
		}
	}

	// Fall back to field name.
	return f.Name, true
}

// ComputeSchemaChecksum computes a deterministic SHA-256 checksum of the
// schema derived from the given Go struct type. The checksum is sensitive
// to field names, types, and nullability, but insensitive to field ordering.
func ComputeSchemaChecksum(t reflect.Type) string {
	// Dereference pointer types.
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	var fields []string
	walkFields("", t, &fields)
	sort.Strings(fields)

	h := sha256.Sum256([]byte(strings.Join(fields, "\n")))
	return fmt.Sprintf("sha256:%x", h)
}

// walkFields recursively traverses struct fields and builds canonical
// field descriptors in the format "field_path:type:required|optional".
func walkFields(prefix string, t reflect.Type, fields *[]string) {
	// Dereference pointer types.
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		// Skip unexported fields.
		if !f.IsExported() {
			continue
		}

		name, ok := resolveColumnName(f)
		if !ok {
			continue
		}

		fullPath := name
		if prefix != "" {
			fullPath = prefix + "." + name
		}

		ft := f.Type
		nullable := false

		// Dereference pointer for type inspection; mark as nullable.
		if ft.Kind() == reflect.Pointer {
			nullable = true
			ft = ft.Elem()
		}

		typeName, recurse := goTypeToSchemaType(ft)

		nullStr := "required"
		if nullable {
			nullStr = "optional"
		}

		if recurse {
			// Nested struct — recurse with dotted prefix.
			walkFields(fullPath, ft, fields)
		} else {
			*fields = append(*fields, fmt.Sprintf("%s:%s:%s", fullPath, typeName, nullStr))
		}
	}
}

// goTypeToSchemaType maps a Go type to a schema type name.
// Returns the type name and whether the type should be recursed into.
func goTypeToSchemaType(t reflect.Type) (string, bool) {
	// time.Time is a leaf type.
	if t == timeType {
		return "timestamp", false
	}

	//exhaustive:enforce
	switch t.Kind() { //nolint:exhaustive // only mapping supported Go types to schema types.
	case reflect.Bool:
		return schemaTypeBool, false
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return schemaTypeInt32, false
	case reflect.Int, reflect.Int64:
		return schemaTypeInt64, false
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return schemaTypeUint32, false
	case reflect.Uint, reflect.Uint64:
		return "uint64", false
	case reflect.Float32:
		return "float32", false
	case reflect.Float64:
		return schemaTypeFloat64, false
	case reflect.String:
		return schemaTypeStringVal, false
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "bytes", false
		}
		elemType, _ := goTypeToSchemaType(t.Elem())
		return "list<" + elemType + ">", false
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return schemaTypeJSONString, false
		}
		// map[string]any → JSON fallback.
		if t.Elem().Kind() == reflect.Interface {
			return schemaTypeJSONString, false
		}
		valType, _ := goTypeToSchemaType(t.Elem())
		return "map<string," + valType + ">", false
	case reflect.Struct:
		return "group", true
	case reflect.Interface:
		return schemaTypeJSONString, false
	default:
		return schemaTypeJSONString, false
	}
}
