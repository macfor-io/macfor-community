package parquet

import (
	"reflect"
	"testing"
	"time"
)

// Test structs for schema inference.
type flatStruct struct {
	Name    string    `json:"name"`
	Age     int       `json:"age"`
	Active  bool      `json:"active"`
	Created time.Time `json:"created"`
}

type nestedSource struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type nestedStruct struct {
	ID     string       `json:"id"`
	Source nestedSource `json:"source"`
}

type tagPriorityStruct struct {
	ParquetTag string `parquet:"pq_name" json:"json_name"`
	JSONTag    string `json:"json_only"`
	NoTag      string
}

type skipFieldStruct struct {
	Included string `json:"included"`
	Excluded string `json:"-"`
	internal string //nolint:unused // testing unexported field exclusion.
}

type nullableStruct struct {
	Required string  `json:"required"`
	Optional *string `json:"optional"`
}

type sliceStruct struct {
	Tags    []string `json:"tags"`
	Scores  []int    `json:"scores"`
	RawData []byte   `json:"raw_data"`
}

type mapStruct struct {
	TypedMap   map[string]string `json:"typed_map"`
	DynamicMap map[string]any    `json:"dynamic_map"`
}

func TestResolveColumnName(t *testing.T) {
	tests := []struct {
		name     string
		field    reflect.StructField
		wantName string
		wantOK   bool
	}{
		{
			name:     "parquet tag takes priority",
			field:    reflect.TypeOf(tagPriorityStruct{}).Field(0),
			wantName: "pq_name",
			wantOK:   true,
		},
		{
			name:     "json tag used as fallback",
			field:    reflect.TypeOf(tagPriorityStruct{}).Field(1),
			wantName: "json_only",
			wantOK:   true,
		},
		{
			name:     "field name used when no tags",
			field:    reflect.TypeOf(tagPriorityStruct{}).Field(2),
			wantName: "NoTag",
			wantOK:   true,
		},
		{
			name:   "json dash skips field",
			field:  reflect.TypeOf(skipFieldStruct{}).Field(1),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, ok := resolveColumnName(tt.field)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestComputeSchemaChecksum_FlatStruct(t *testing.T) {
	cs := ComputeSchemaChecksum(reflect.TypeOf(flatStruct{}))
	if cs == "" {
		t.Fatal("checksum should not be empty")
	}
	if len(cs) < 10 {
		t.Fatalf("checksum too short: %s", cs)
	}
	if cs[:7] != "sha256:" {
		t.Errorf("checksum should start with sha256:, got %s", cs[:7])
	}
}

func TestComputeSchemaChecksum_Deterministic(t *testing.T) {
	cs1 := ComputeSchemaChecksum(reflect.TypeOf(flatStruct{}))
	cs2 := ComputeSchemaChecksum(reflect.TypeOf(flatStruct{}))
	if cs1 != cs2 {
		t.Errorf("checksum not deterministic: %s != %s", cs1, cs2)
	}
}

func TestComputeSchemaChecksum_DifferentStructs(t *testing.T) {
	cs1 := ComputeSchemaChecksum(reflect.TypeOf(flatStruct{}))
	cs2 := ComputeSchemaChecksum(reflect.TypeOf(nestedStruct{}))
	if cs1 == cs2 {
		t.Error("different structs should produce different checksums")
	}
}

func TestComputeSchemaChecksum_NestedStruct(t *testing.T) {
	cs := ComputeSchemaChecksum(reflect.TypeOf(nestedStruct{}))
	if cs == "" {
		t.Fatal("checksum should not be empty")
	}
	// Nested struct should produce dotted paths like "source.file".
	// We verify indirectly via checksum difference from flat struct.
	csFlat := ComputeSchemaChecksum(reflect.TypeOf(flatStruct{}))
	if cs == csFlat {
		t.Error("nested struct should have different checksum than flat struct")
	}
}

func TestComputeSchemaChecksum_Nullable(t *testing.T) {
	// Struct with pointer field should differ from non-pointer equivalent.
	cs1 := ComputeSchemaChecksum(reflect.TypeOf(nullableStruct{}))
	type allRequired struct {
		Required string `json:"required"`
		Optional string `json:"optional"`
	}
	cs2 := ComputeSchemaChecksum(reflect.TypeOf(allRequired{}))
	if cs1 == cs2 {
		t.Error("nullable (pointer) field should change checksum")
	}
}

func TestComputeSchemaChecksum_SkipsExcluded(t *testing.T) {
	cs1 := ComputeSchemaChecksum(reflect.TypeOf(skipFieldStruct{}))
	// Should only include "included" field.
	type onlyIncluded struct {
		Included string `json:"included"`
	}
	cs2 := ComputeSchemaChecksum(reflect.TypeOf(onlyIncluded{}))
	if cs1 != cs2 {
		t.Error("excluded fields (json:\"-\" and unexported) should not affect checksum")
	}
}

func TestComputeSchemaChecksum_Slices(t *testing.T) {
	cs := ComputeSchemaChecksum(reflect.TypeOf(sliceStruct{}))
	if cs == "" {
		t.Fatal("checksum should not be empty for slice struct")
	}
}

func TestComputeSchemaChecksum_Maps(t *testing.T) {
	cs := ComputeSchemaChecksum(reflect.TypeOf(mapStruct{}))
	if cs == "" {
		t.Fatal("checksum should not be empty for map struct")
	}
}

func TestComputeSchemaChecksum_FieldOrder(t *testing.T) {
	// Field order should not affect the checksum (fields are sorted canonically).
	type orderA struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	type orderB struct {
		Age  int    `json:"age"`
		Name string `json:"name"`
	}
	cs1 := ComputeSchemaChecksum(reflect.TypeOf(orderA{}))
	cs2 := ComputeSchemaChecksum(reflect.TypeOf(orderB{}))
	if cs1 != cs2 {
		t.Errorf("reordered fields should produce the same checksum: %s != %s", cs1, cs2)
	}
}

func TestComputeSchemaChecksum_AddedField(t *testing.T) {
	// Adding a field should change the checksum.
	type base struct {
		Name string `json:"name"`
	}
	type extended struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	cs1 := ComputeSchemaChecksum(reflect.TypeOf(base{}))
	cs2 := ComputeSchemaChecksum(reflect.TypeOf(extended{}))
	if cs1 == cs2 {
		t.Error("added field should produce a different checksum")
	}
}

func TestComputeSchemaChecksum_PointerType(t *testing.T) {
	// Passing a *T should work the same as T.
	cs1 := ComputeSchemaChecksum(reflect.TypeOf(flatStruct{}))
	cs2 := ComputeSchemaChecksum(reflect.TypeOf(&flatStruct{}))
	if cs1 != cs2 {
		t.Error("pointer to struct should produce same checksum as struct")
	}
}

func TestGoTypeToSchemaType(t *testing.T) {
	tests := []struct {
		name      string
		goType    reflect.Type
		wantType  string
		wantRecur bool
	}{
		{"bool", reflect.TypeOf(true), "bool", false},
		{"int", reflect.TypeOf(0), "int64", false},
		{"int32", reflect.TypeOf(int32(0)), "int32", false},
		{"int64", reflect.TypeOf(int64(0)), "int64", false},
		{"uint32", reflect.TypeOf(uint32(0)), "uint32", false},
		{"float64", reflect.TypeOf(float64(0)), "float64", false},
		{"string", reflect.TypeOf(""), "string", false},
		{"time.Time", reflect.TypeOf(time.Time{}), "timestamp", false},
		{"[]byte", reflect.TypeOf([]byte{}), "bytes", false},
		{"[]string", reflect.TypeOf([]string{}), "list<string>", false},
		{"[]int", reflect.TypeOf([]int{}), "list<int64>", false},
		{"map[string]string", reflect.TypeOf(map[string]string{}), "map<string,string>", false},
		{"map[string]any", reflect.TypeOf(map[string]any{}), "json_string", false},
		{"struct", reflect.TypeOf(flatStruct{}), "group", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotRecur := goTypeToSchemaType(tt.goType)
			if gotType != tt.wantType {
				t.Errorf("type = %q, want %q", gotType, tt.wantType)
			}
			if gotRecur != tt.wantRecur {
				t.Errorf("recurse = %v, want %v", gotRecur, tt.wantRecur)
			}
		})
	}
}

func TestWalkFields_FlatStruct(t *testing.T) {
	var fields []string
	walkFields("", reflect.TypeOf(flatStruct{}), &fields)

	if len(fields) != 4 {
		t.Fatalf("expected 4 fields, got %d: %v", len(fields), fields)
	}

	// Check that all expected fields are present.
	expected := map[string]bool{
		"name:string:required":       false,
		"age:int64:required":         false,
		"active:bool:required":       false,
		"created:timestamp:required": false,
	}
	for _, f := range fields {
		if _, ok := expected[f]; ok {
			expected[f] = true
		} else {
			t.Errorf("unexpected field: %s", f)
		}
	}
	for f, found := range expected {
		if !found {
			t.Errorf("missing expected field: %s", f)
		}
	}
}

func TestWalkFields_NestedStruct(t *testing.T) {
	var fields []string
	walkFields("", reflect.TypeOf(nestedStruct{}), &fields)

	expected := map[string]bool{
		"id:string:required":          false,
		"source.file:string:required": false,
		"source.line:int64:required":  false,
	}
	for _, f := range fields {
		if _, ok := expected[f]; ok {
			expected[f] = true
		} else {
			t.Errorf("unexpected field: %s", f)
		}
	}
	for f, found := range expected {
		if !found {
			t.Errorf("missing expected field: %s", f)
		}
	}
}
