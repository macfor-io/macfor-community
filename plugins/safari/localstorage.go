package safari

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/sqlite"
	"github.com/macfor-io/macfor-community/pkg/macfor/tablemapper"
)

// LocalStorageRecord represents a key-value entry from Safari's LocalStorage.
// Each record corresponds to a row from the ItemTable in a .localstorage database.
type LocalStorageRecord struct {
	// Type identifies this record type for downstream processing.
	Type string `json:"type"`

	// Browser identifies the source browser.
	Browser string `json:"browser"`

	// User is the username who owns this local storage data.
	User string `json:"user"`

	// Origin is the web origin (e.g., "https://github.com") extracted from the filename.
	Origin string `json:"origin"`

	// Key is the localStorage key name.
	Key string `json:"key"`

	// ValuePreview is a preview of the value, truncated for large values.
	// Values longer than 200 bytes show the first 200 bytes with a "..." suffix
	// (the manifest's "truncate:200" transform).
	ValuePreview string `json:"value_preview"`

	// ValueSize is the size of the value in bytes.
	ValueSize int `json:"value_size"`

	// SourceFile is the path to the .localstorage file on the source system.
	SourceFile string `json:"source_file"`
}

// Constants for local storage processing.
const (
	// localStorageExtension is the file extension for local storage databases.
	localStorageExtension = ".localstorage"

	// itemTableName is the table name in local storage databases.
	itemTableName = "ItemTable"
)

// collectLocalStorageForUser collects Safari LocalStorage databases for a single user.
// It enumerates all .localstorage files via the manifest's version-aware glob
// paths, parses each database, and adds the records to the collector along with
// the raw files.
func collectLocalStorageForUser(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	// Resolve LocalStorage glob paths from the manifest (the location differs by
	// macOS version) and enumerate the matching .localstorage databases.
	paths, err := locator.Paths(src, user, string(ArtifactLocalStorage))
	if err != nil {
		return err
	}

	var localStorageFiles []string
	for _, rp := range paths {
		if rp.Type != artifacts.PathTypeGlob {
			continue
		}
		matches, err := src.Glob(rp.Path)
		if err != nil {
			return fmt.Errorf("globbing LocalStorage databases: %w", err)
		}
		localStorageFiles = append(localStorageFiles, matches...)
	}

	if len(localStorageFiles) == 0 {
		out.Log(macfor.LogDebug, "No LocalStorage databases found for user %s", user.Username)
		return nil
	}

	out.Log(macfor.LogInfo, "Found %d LocalStorage databases for user %s", len(localStorageFiles), user.Username)

	// Process each local storage database.
	totalRecords := 0
	var collectionErrors []error

	for _, filePath := range localStorageFiles {
		// Check for context cancellation.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		filename := filepath.Base(filePath)
		recordCount, err := collectSingleLocalStorage(ctx, user, src, out, filePath, filename)
		if err != nil {
			collectionErrors = append(collectionErrors, fmt.Errorf("%s: %w", filename, err))
			out.Log(macfor.LogWarn, "Error collecting LocalStorage %s: %v", filename, err)
			continue
		}
		totalRecords += recordCount
	}

	out.Log(macfor.LogInfo, "Collected %d LocalStorage records from %d databases for user %s",
		totalRecords, len(localStorageFiles), user.Username)

	if len(collectionErrors) > 0 {
		return errors.Join(collectionErrors...)
	}

	return nil
}

// collectSingleLocalStorage collects a single LocalStorage database file.
// Returns the number of records collected.
func collectSingleLocalStorage(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector, filePath, filename string) (int, error) {
	// Verify it's a valid SQLite file.
	isValid, err := sqlite.IsSQLiteFile(src, filePath)
	if err != nil {
		return 0, fmt.Errorf("validating LocalStorage file: %w", err)
	}
	if !isValid {
		out.Log(macfor.LogWarn, "LocalStorage file %s is not a valid SQLite database", filename)
		return 0, fmt.Errorf("invalid SQLite database: %s", filename)
	}

	// Extract origin from filename.
	origin := extractOriginFromFilename(filename)

	// Create artifact for the raw file and parsed records.
	artifact, err := macfor.NewArtifact(PluginID, "browser", "local_storage")
	if err != nil {
		return 0, fmt.Errorf("creating artifact: %w", err)
	}
	artifact.SourcePath = filePath
	artifact.Username = user.Username
	artifact.Description = fmt.Sprintf("Safari LocalStorage for %s", origin)

	// Get file info for metadata.
	fileInfo, err := src.Stat(filePath)
	if err == nil {
		artifact.OriginalModTime = fileInfo.ModTime()
		artifact.OriginalSize = fileInfo.Size()
	}

	// Add the raw database file to the container.
	rawFile, err := src.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("opening LocalStorage file: %w", err)
	}
	if err := out.AddFile(ctx, *artifact, rawFile); err != nil {
		_ = rawFile.Close()
		return 0, fmt.Errorf("adding LocalStorage file: %w", err)
	}
	_ = rawFile.Close()

	// Copy database for safe reading.
	tempPath, cleanup, err := sqlite.CopyForReading(src, filePath)
	if err != nil {
		// If we can't copy, log warning but don't fail - we still have the raw file.
		out.Log(macfor.LogWarn, "Could not copy LocalStorage %s for parsing: %v", filename, err)
		return 0, nil
	}
	defer cleanup()

	// Open the copy for reading.
	db, err := sqlite.OpenReadOnly(tempPath)
	if err != nil {
		out.Log(macfor.LogWarn, "Could not open LocalStorage %s copy: %v", filename, err)
		return 0, nil
	}
	defer func() { _ = db.Close() }()

	// Verify ItemTable exists.
	if !sqlite.TableExists(db, itemTableName) {
		out.Log(macfor.LogWarn, "LocalStorage %s missing ItemTable", filename)
		return 0, nil
	}

	// Extract records via the declarative table mapper (driven by the manifest's
	// extract block). The per-file origin is supplied as context.
	def := artifacts.GetArtifactByID(manifest, string(ArtifactLocalStorage))
	if def == nil || def.Extract == nil {
		return 0, fmt.Errorf("local_storage artifact missing extract spec in manifest")
	}

	recordCount, err := tablemapper.Each(ctx, db, *def.Extract, tablemapper.Options{
		NewRecord: func() any { return &LocalStorageRecord{} },
		Context: map[string]string{
			ctxUsername:   user.Username,
			ctxOrigin:     origin,
			ctxSourceFile: filePath,
		},
		Warnf: func(format string, args ...any) { out.Log(macfor.LogWarn, format, args...) },
	}, func(rec any) error {
		return out.AddRecord(ctx, *artifact, rec)
	})
	if err != nil {
		out.Log(macfor.LogError, "Error parsing LocalStorage %s: %v", filename, err)
		return recordCount, err
	}

	return recordCount, nil
}

// extractOriginFromFilename extracts the web origin from a LocalStorage filename.
// Safari uses filenames like "https_github.com_0.localstorage".
// This function converts it to "https://github.com".
func extractOriginFromFilename(filename string) string {
	// Remove the extension.
	name := strings.TrimSuffix(filename, localStorageExtension)

	// Remove the trailing _N (index number).
	lastUnderscore := strings.LastIndex(name, "_")
	if lastUnderscore > 0 {
		// Check if the part after the last underscore is a number.
		suffix := name[lastUnderscore+1:]
		isNumber := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				isNumber = false
				break
			}
		}
		if isNumber {
			name = name[:lastUnderscore]
		}
	}

	// Find the first underscore which separates scheme from domain.
	firstUnderscore := strings.Index(name, "_")
	if firstUnderscore < 0 {
		// No underscore found, return as-is.
		return name
	}

	scheme := name[:firstUnderscore]
	domain := name[firstUnderscore+1:]

	// Replace remaining underscores in domain (if any) with dots.
	// Note: This is a heuristic. Some domains may have underscores,
	// but Safari's format replaces dots with underscores.
	// Actually, Safari only replaces the scheme separator, not dots in domain.
	// The domain part keeps its original format with dots.
	// Example: https_github.com_0 -> https://github.com

	return scheme + "://" + domain
}
