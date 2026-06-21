package artifacts

import (
	"fmt"
	"strings"
)

// msgRequired is the validation message for a missing required field.
const msgRequired = "required"

// ValidationError represents a manifest validation error.
type ValidationError struct {
	// Field is the field path that failed validation.
	Field string

	// Message describes the validation failure.
	Message string
}

// Error implements the error interface.
func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validate checks a manifest for errors.
// Returns nil if the manifest is valid.
func Validate(m *Manifest) error {
	var errs []ValidationError

	// Schema version.
	if m.SchemaVersion == "" {
		errs = append(errs, ValidationError{"schema_version", msgRequired})
	}

	// Plugin metadata.
	errs = append(errs, validatePlugin(&m.Plugin)...)

	// Artifacts.
	if len(m.Artifacts) == 0 {
		errs = append(errs, ValidationError{"artifacts", "at least one artifact required"})
	}

	seenIDs := make(map[string]bool)
	for i, a := range m.Artifacts {
		prefix := fmt.Sprintf("artifacts[%d]", i)
		errs = append(errs, validateArtifact(&a, prefix, seenIDs)...)
	}

	if len(errs) > 0 {
		return combineErrors(errs)
	}
	return nil
}

// validatePlugin validates plugin metadata.
func validatePlugin(p *Plugin) []ValidationError {
	var errs []ValidationError

	if p.ID == "" {
		errs = append(errs, ValidationError{"plugin.id", msgRequired})
	}
	if p.Name == "" {
		errs = append(errs, ValidationError{"plugin.name", msgRequired})
	}
	if p.Description == "" {
		errs = append(errs, ValidationError{"plugin.description", msgRequired})
	}
	if p.Version == "" {
		errs = append(errs, ValidationError{"plugin.version", msgRequired})
	}
	if len(p.Categories) == 0 {
		errs = append(errs, ValidationError{"plugin.categories", "at least one category required"})
	}
	if len(p.Platforms) == 0 {
		errs = append(errs, ValidationError{"plugin.platforms", "at least one platform required"})
	}

	for i, platform := range p.Platforms {
		prefix := fmt.Sprintf("plugin.platforms[%d]", i)
		if platform.OS == "" {
			errs = append(errs, ValidationError{prefix + ".os", msgRequired})
		}
	}

	return errs
}

// validateArtifact validates a single artifact.
func validateArtifact(a *Artifact, prefix string, seenIDs map[string]bool) []ValidationError {
	var errs []ValidationError

	switch {
	case a.ID == "":
		errs = append(errs, ValidationError{prefix + ".id", msgRequired})
	case seenIDs[a.ID]:
		errs = append(errs, ValidationError{prefix + ".id", "duplicate ID: " + a.ID})
	default:
		seenIDs[a.ID] = true
	}

	if a.Name == "" {
		errs = append(errs, ValidationError{prefix + ".name", msgRequired})
	}
	if a.Category == "" {
		errs = append(errs, ValidationError{prefix + ".category", msgRequired})
	} else if !isValidCategory(a.Category) {
		errs = append(errs, ValidationError{prefix + ".category", "invalid category: " + a.Category})
	}
	if a.Format == "" {
		errs = append(errs, ValidationError{prefix + ".format", msgRequired})
	} else if !isValidFormat(a.Format) {
		errs = append(errs, ValidationError{prefix + ".format", "invalid format: " + a.Format})
	}
	if len(a.Paths) == 0 {
		errs = append(errs, ValidationError{prefix + ".paths", "at least one path required"})
	}

	for j, p := range a.Paths {
		pathPrefix := fmt.Sprintf("%s.paths[%d]", prefix, j)
		errs = append(errs, validatePathSpec(&p, pathPrefix)...)
	}

	for k, s := range a.Schema {
		schemaPrefix := fmt.Sprintf("%s.schema[%d]", prefix, k)
		errs = append(errs, validateSchemaSpec(&s, schemaPrefix)...)
	}

	// An artifact declares at most one parse path: a declarative extract block
	// or a named native parser, never both.
	if a.Parser != "" && a.Extract != nil {
		errs = append(errs, ValidationError{prefix, "parser and extract are mutually exclusive"})
	}
	if a.Extract != nil {
		errs = append(errs, validateExtract(a.Extract, a.Format, prefix+".extract")...)
	}

	return errs
}

// validateExtract validates a declarative extract block against the artifact's
// format: every field needs a name and exactly one value source, the source
// kind must match the format's mapper (column for SQLite/JSONL; key/derived
// for property lists), and transform references must name transforms that
// mapper supports. Deriver references are plugin-supplied and resolved by the
// mappers at runtime (fail-fast); plugins guard them with a dispatch test.
func validateExtract(e *ExtractSpec, format, prefix string) []ValidationError {
	var errs []ValidationError

	if !extractCapableFormat(format) {
		errs = append(errs, ValidationError{prefix, "extract is not supported for format " + format})
		return errs
	}

	if isTableFormat(format) && e.From == "" {
		errs = append(errs, ValidationError{prefix + ".from", "required for " + format + " extraction"})
	}

	if len(e.Fields) == 0 {
		errs = append(errs, ValidationError{prefix + ".fields", "at least one field required"})
	}

	for i := range e.Fields {
		f := &e.Fields[i]
		fieldPrefix := fmt.Sprintf("%s.fields[%d]", prefix, i)

		if f.Name == "" {
			errs = append(errs, ValidationError{fieldPrefix + ".name", msgRequired})
		}

		sources := 0
		for _, s := range []string{f.Column, f.Key, f.Const, f.Context, f.Derived} {
			if s != "" {
				sources++
			}
		}
		switch {
		case sources == 0:
			errs = append(errs, ValidationError{fieldPrefix, "field needs a source: one of column, key, const, context, or derived"})
		case sources > 1:
			errs = append(errs, ValidationError{fieldPrefix, "field sources are mutually exclusive: set only one of column, key, const, context, derived"})
		}

		// Source kinds must match the format's mapper. The table mapper reads
		// columns; the plist mapper reads dict keys. Both support const, context,
		// and derived sources.
		if isTableFormat(format) {
			if f.Key != "" {
				errs = append(errs, ValidationError{fieldPrefix + ".key", "not valid for " + format + " extraction (use column)"})
			}
		} else if f.Column != "" {
			errs = append(errs, ValidationError{fieldPrefix + ".column", "not valid for " + format + " extraction (use key)"})
		}

		if f.Transform != "" && !IsValidTransform(format, f.Transform) {
			errs = append(errs, ValidationError{fieldPrefix + ".transform", fmt.Sprintf("unknown transform %q for format %s", f.Transform, format)})
		}
	}

	return errs
}

// validatePathSpec validates a path specification.
func validatePathSpec(p *PathSpec, prefix string) []ValidationError {
	var errs []ValidationError

	if p.Pattern == "" {
		errs = append(errs, ValidationError{prefix + ".pattern", msgRequired})
	}
	if p.Type != "" && !isValidPathType(p.Type) {
		errs = append(errs, ValidationError{prefix + ".type", "invalid type: " + p.Type})
	}

	return errs
}

// validateSchemaSpec validates a schema specification.
func validateSchemaSpec(_ *SchemaSpec, _ string) []ValidationError {
	// OSRange is optional but if provided must have valid structure.
	// Empty strings are allowed for unbounded ranges.
	// Currently no validation rules are required beyond struct parsing.
	return nil
}

// isValidFormat checks if a format string is valid.
func isValidFormat(f string) bool {
	switch f {
	case FormatSQLite, FormatBinaryPlist, FormatXMLPlist,
		FormatText, FormatYAML, FormatJSON, FormatBinary,
		FormatBinaryCookies, FormatDirectory, FormatLevelDB,
		FormatSNSS, FormatJSONL:
		return true
	}
	return false
}

// isValidPathType checks if a path type string is valid.
func isValidPathType(t string) bool {
	switch t {
	case PathTypeFile, PathTypeGlob, PathTypeDirectory:
		return true
	}
	return false
}

// isValidCategory checks if a category string is valid.
func isValidCategory(c string) bool {
	switch c {
	case CategoryHistory, CategoryDownloads, CategoryBookmarks,
		CategoryCookies, CategoryStorage, CategorySession,
		CategoryExtensions, CategoryLogs, CategoryCommandHistory,
		CategoryConfig, CategoryCache, CategoryCredentials,
		CategoryAutofill, CategoryContacts, CategoryMedia,
		CategoryMetadata, CategoryMessaging, CategoryCalls,
		CategoryEmail, CategoryPersistence:
		return true
	}
	return false
}

// combineErrors combines multiple validation errors into a single error.
func combineErrors(errs []ValidationError) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	return fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
}

// ValidFormats returns all valid format constants.
func ValidFormats() []string {
	return []string{
		FormatSQLite, FormatBinaryPlist, FormatXMLPlist,
		FormatText, FormatYAML, FormatJSON, FormatBinary,
		FormatBinaryCookies, FormatDirectory, FormatLevelDB,
		FormatSNSS, FormatJSONL,
	}
}

// ValidCategories returns all valid category constants.
func ValidCategories() []string {
	return []string{
		CategoryHistory, CategoryDownloads, CategoryBookmarks,
		CategoryCookies, CategoryStorage, CategorySession,
		CategoryExtensions, CategoryLogs, CategoryCommandHistory,
		CategoryConfig, CategoryCache, CategoryCredentials,
		CategoryAutofill, CategoryContacts, CategoryMedia,
		CategoryMetadata, CategoryMessaging, CategoryCalls,
		CategoryEmail, CategoryPersistence,
	}
}

// ValidPathTypes returns all valid path type constants.
func ValidPathTypes() []string {
	return []string{
		PathTypeFile, PathTypeGlob, PathTypeDirectory,
	}
}
