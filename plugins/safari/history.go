package safari

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/sqlite"
	"github.com/macfor-io/macfor-community/pkg/macfor/tablemapper"
)

// HistoryVisit represents a single browsing history visit from Safari's History.db.
// Each record corresponds to a row from the history_visits table joined with
// its parent history_items row.
type HistoryVisit struct {
	// Type identifies this record type for downstream processing.
	Type string `json:"type"`

	// Browser identifies the source browser.
	Browser string `json:"browser"`

	// User is the username who owns this history.
	User string `json:"user"`

	// URL is the visited page URL from history_items.url.
	URL string `json:"url"`

	// Title is the page title from history_visits.title.
	// May be empty if the page title was not captured.
	Title string `json:"title,omitempty"`

	// VisitTime is the timestamp of this specific visit.
	// Converted from Core Data format (seconds since 2001-01-01 UTC).
	VisitTime time.Time `json:"visit_time"`

	// VisitCount is the total number of visits to this URL across all time.
	// From history_items.visit_count.
	VisitCount int `json:"visit_count"`

	// LoadSuccessful indicates whether the page loaded successfully.
	// False indicates a failed load (e.g., network error, 404).
	LoadSuccessful bool `json:"load_successful"`

	// RedirectSource is the visit ID that redirected to this visit.
	// Nil if this visit was not the result of a redirect.
	RedirectSource *int64 `json:"redirect_source,omitempty"`

	// RedirectDest is the visit ID that this visit redirected to.
	// Nil if this visit did not redirect elsewhere.
	RedirectDest *int64 `json:"redirect_destination,omitempty"`

	// SourceFile is the path to the History.db file on the source system.
	SourceFile string `json:"source_file"`

	// HistoryItemID is the primary key from history_items table.
	// Useful for forensic provenance and correlation.
	HistoryItemID int64 `json:"history_item_id"`

	// VisitID is the primary key from history_visits table.
	// Useful for forensic provenance and redirect chain reconstruction.
	VisitID int64 `json:"visit_id"`
}

// collectHistoryForUser collects Safari browsing history from History.db for a single user.
// It copies the database (with WAL support), parses all visit records, and adds them
// to the collector along with the raw database file.
func collectHistoryForUser(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	historyPath, err := locator.Path(src, user, string(ArtifactHistory))
	if err != nil {
		return err
	}
	if historyPath == "" {
		out.Log(macfor.LogDebug, "History.db not found for user %s", user.Username)
		return nil
	}

	// Check if the history file exists.
	_, err = src.Stat(historyPath)
	if err != nil {
		if errors.Is(err, macfor.ErrNotFound) || errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			out.Log(macfor.LogDebug, "History.db not found for user %s", user.Username)
			return nil
		}
		return fmt.Errorf("checking History.db: %w", err)
	}

	// Verify it's a valid SQLite file.
	isValid, err := sqlite.IsSQLiteFile(src, historyPath)
	if err != nil {
		return fmt.Errorf("validating History.db: %w", err)
	}
	if !isValid {
		out.Log(macfor.LogWarn, "History.db is not a valid SQLite database for user %s", user.Username)
		return fmt.Errorf("invalid SQLite database: %s", historyPath)
	}

	// Create artifact for the raw file and parsed records.
	artifact, err := macfor.NewArtifact(PluginID, "browser", "history")
	if err != nil {
		return fmt.Errorf("creating artifact: %w", err)
	}
	artifact.SourcePath = historyPath
	artifact.Username = user.Username
	artifact.Description = "Safari browsing history database"

	// Get file info for metadata.
	fileInfo, err := src.Stat(historyPath)
	if err == nil {
		artifact.OriginalModTime = fileInfo.ModTime()
		artifact.OriginalSize = fileInfo.Size()
	}

	// Add the raw database file to the container.
	rawFile, err := src.Open(historyPath)
	if err != nil {
		return fmt.Errorf("opening History.db: %w", err)
	}
	if err := out.AddFile(ctx, *artifact, rawFile); err != nil {
		_ = rawFile.Close()
		return fmt.Errorf("adding History.db file: %w", err)
	}
	_ = rawFile.Close()

	// Copy database with WAL support for safe reading.
	tempPath, cleanup, err := sqlite.CopyForReading(src, historyPath)
	if err != nil {
		// If we can't copy, log warning but don't fail - we still have the raw file.
		out.Log(macfor.LogWarn, "Could not copy History.db for parsing: %v", err)
		return nil
	}
	defer cleanup()

	// Open the copy for reading.
	db, err := sqlite.OpenReadOnly(tempPath)
	if err != nil {
		out.Log(macfor.LogWarn, "Could not open History.db copy: %v", err)
		return nil
	}
	defer func() { _ = db.Close() }()

	// Verify required tables exist.
	if !sqlite.TableExists(db, "history_items") || !sqlite.TableExists(db, "history_visits") {
		out.Log(macfor.LogWarn, "History.db missing required tables for user %s", user.Username)
		return nil
	}

	// Parse and output all history visits.
	visitCount, err := parseAndOutputHistory(ctx, db, user.Username, historyPath, artifact, out)
	if err != nil {
		// Log error but don't fail - partial data is better than none.
		out.Log(macfor.LogError, "Error parsing History.db for user %s: %v", user.Username, err)
		return err
	}

	out.Log(macfor.LogInfo, "Collected %d history visits for user %s", visitCount, user.Username)
	return nil
}

// parseAndOutputHistory extracts all history visit records via the declarative
// table mapper (driven by the manifest's extract block) and writes them to the
// collector. Returns the number of visits processed.
func parseAndOutputHistory(ctx context.Context, db *sql.DB, username, sourcePath string, artifact *macfor.Artifact, out macfor.Collector) (int, error) {
	def := artifacts.GetArtifactByID(manifest, string(ArtifactHistory))
	if def == nil || def.Extract == nil {
		return 0, fmt.Errorf("history artifact missing extract spec in manifest")
	}

	return tablemapper.Each(ctx, db, *def.Extract, tablemapper.Options{
		NewRecord: func() any { return &HistoryVisit{} },
		Context: map[string]string{
			ctxUsername:   username,
			ctxSourceFile: sourcePath,
		},
		Warnf: func(format string, args ...any) { out.Log(macfor.LogWarn, format, args...) },
	}, func(rec any) error {
		return out.AddRecord(ctx, *artifact, rec)
	})
}
