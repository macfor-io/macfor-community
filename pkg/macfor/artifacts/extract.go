package artifacts

// ExtractSpec declares how to extract structured records from a tabular
// artifact. It covers two backends, selected by the artifact's format:
//
//   - SQLite/JSONL (table mapper): From/Join/Where/OrderBy plus column-sourced
//     fields. The declarative counterpart to a hand-written SQL query + scan.
//   - Property lists (plist mapper): Records names the array of element dicts,
//     and fields are sourced by Key (a dict key within each element).
//
// In both cases the record's typed schema remains a Go struct that the mapper
// populates. ExtractSpec is optional; artifacts without it are parsed by native
// Go via the parser registry.
type ExtractSpec struct {
	// From is the primary table name (the FROM clause target) for SQLite.
	From string `yaml:"from,omitempty"`

	// Join is an optional raw JOIN clause (SQLite), e.g.
	// "JOIN history_visits ON history_visits.history_item = history_items.id".
	Join string `yaml:"join,omitempty"`

	// Where is an optional WHERE clause body (SQLite, without the "WHERE" keyword).
	Where string `yaml:"where,omitempty"`

	// OrderBy is an optional ORDER BY clause body (SQLite, without "ORDER BY").
	OrderBy string `yaml:"order_by,omitempty"`

	// Records is the plist dictionary key holding the array of element dicts to
	// map (one record per element). If empty and the plist root is itself an
	// array, the root array is used.
	Records string `yaml:"records,omitempty"`

	// Fields is the ordered list of output fields. Order is preserved from the
	// manifest; column-sourced fields are selected in this order.
	Fields []FieldSpec `yaml:"fields"`
}

// FieldSpec maps a single output field to its source. Exactly one of Column,
// Key, Const, or Context should be set.
type FieldSpec struct {
	// Name is the record field to populate, matched against the Go struct's
	// JSON tag (or field name).
	Name string `yaml:"name"`

	// Column is a (optionally table-qualified) source column for SQLite, e.g.
	// "history_visits.visit_time".
	Column string `yaml:"column,omitempty"`

	// Key is the source dictionary key for plist elements, e.g.
	// "DownloadEntryURL".
	Key string `yaml:"key,omitempty"`

	// Const is a literal string value (used for discriminator fields such as
	// record type or browser name).
	Const string `yaml:"const,omitempty"`

	// Context names a value injected by the collector rather than read from the
	// source (e.g. "username", "source_file", "origin").
	Context string `yaml:"context,omitempty"`

	// Derived names a registered derive function that computes this field's value
	// from the whole source element. It is the field-level escape hatch for
	// cross-field logic that is not a simple value transform.
	Derived string `yaml:"derived,omitempty"`

	// Transform names a value transformation applied to the sourced value
	// (e.g. "coredata_timestamp", "bool_from_int", "basename", "truncate:N").
	Transform string `yaml:"transform,omitempty"`

	// Optional indicates the source column/key may be physically absent in some
	// schema versions. When absent, the field is left at its zero value
	// (nil pointer / empty), matching drift-tolerant native parsers.
	Optional bool `yaml:"optional,omitempty"`
}
