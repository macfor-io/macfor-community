package safari

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"howett.net/plist"
)

// SessionWindowRecord represents a Safari window from LastSession.plist.
// Each record corresponds to a single window with its tabs and state.
type SessionWindowRecord struct {
	// Type identifies this record type for downstream processing.
	Type string `json:"type"`

	// Browser identifies the source browser.
	Browser string `json:"browser"`

	// User is the username who owned this session.
	User string `json:"user"`

	// WindowIndex is the position of this window in the session (0-based).
	WindowIndex int `json:"window_index"`

	// WindowUUID is the unique identifier for this window.
	WindowUUID string `json:"window_uuid"`

	// IsPrivate indicates if this was a private browsing window.
	IsPrivate bool `json:"is_private"`

	// SelectedTabIndex is the index of the active tab when the session was saved.
	SelectedTabIndex int `json:"selected_tab_index"`

	// TabCount is the number of tabs in this window.
	TabCount int `json:"tab_count"`

	// SourceFile is the path to the LastSession.plist file on the source system.
	SourceFile string `json:"source_file"`
}

// SessionTabRecord represents a Safari tab from LastSession.plist.
// Each record corresponds to a single tab within a window.
type SessionTabRecord struct {
	// Type identifies this record type for downstream processing.
	Type string `json:"type"`

	// Browser identifies the source browser.
	Browser string `json:"browser"`

	// User is the username who owned this session.
	User string `json:"user"`

	// WindowIndex is the position of the parent window (0-based).
	WindowIndex int `json:"window_index"`

	// TabIndex is the position of this tab within its window (0-based).
	TabIndex int `json:"tab_index"`

	// TabUUID is the unique identifier for this tab.
	TabUUID string `json:"tab_uuid"`

	// URL is the tab's current URL.
	URL string `json:"url"`

	// Title is the tab's title.
	Title string `json:"title"`

	// LastVisitTime is when the tab was last visited (if available).
	LastVisitTime time.Time `json:"last_visit_time,omitempty"`

	// IsAppInitiated indicates if the tab was opened by the app (e.g., startup restore).
	IsAppInitiated bool `json:"is_app_initiated"`

	// SourceFile is the path to the LastSession.plist file on the source system.
	SourceFile string `json:"source_file"`
}

// sessionData represents the root structure of LastSession.plist.
type sessionData struct {
	SessionVersion int             `plist:"SessionVersion"`
	SessionWindows []sessionWindow `plist:"SessionWindows"`
}

// sessionWindow represents a window in the LastSession.plist.
type sessionWindow struct {
	WindowUUID       string       `plist:"WindowUUID"`
	IsPrivate        bool         `plist:"IsPrivate"`
	SelectedTabIndex int          `plist:"SelectedTabIndex"`
	TabStates        []sessionTab `plist:"TabStates"`
}

// sessionTab represents a tab in a window in the LastSession.plist.
type sessionTab struct {
	TabUUID        string    `plist:"TabUUID"`
	TabURL         string    `plist:"TabURL"`
	TabTitle       string    `plist:"TabTitle"`
	LastVisitTime  time.Time `plist:"LastVisitTime"`
	IsAppInitiated bool      `plist:"isAppInitiated"`
}

// collectLastSessionForUser collects Safari last session data from LastSession.plist for a single user.
// It reads the plist file, parses all window and tab entries, and adds them to the collector
// along with the raw plist file.
func collectLastSessionForUser(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	sessionPath, err := locator.Path(src, user, string(ArtifactLastSession))
	if err != nil {
		return err
	}
	if sessionPath == "" {
		out.Log(macfor.LogDebug, "LastSession.plist not found for user %s", user.Username)
		return nil
	}

	// Check if the session file exists.
	fileInfo, err := src.Stat(sessionPath)
	if err != nil {
		if errors.Is(err, macfor.ErrNotFound) || errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			out.Log(macfor.LogDebug, "LastSession.plist not found for user %s", user.Username)
			return nil
		}
		return fmt.Errorf("checking LastSession.plist: %w", err)
	}

	// Create artifact for the raw file and parsed records.
	artifact, err := macfor.NewArtifact(PluginID, "browser", "session")
	if err != nil {
		return fmt.Errorf("creating artifact: %w", err)
	}
	artifact.SourcePath = sessionPath
	artifact.Username = user.Username
	artifact.Description = "Safari last session state"
	artifact.OriginalModTime = fileInfo.ModTime()
	artifact.OriginalSize = fileInfo.Size()

	// Read the plist file.
	rawFile, err := src.Open(sessionPath)
	if err != nil {
		return fmt.Errorf("opening LastSession.plist: %w", err)
	}

	// Read all content for parsing and container storage.
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rawFile); err != nil {
		_ = rawFile.Close()
		return fmt.Errorf("reading LastSession.plist: %w", err)
	}
	_ = rawFile.Close()
	plistData := buf.Bytes()

	// Add raw file to container from the already-read buffer.
	if err := out.AddFile(ctx, *artifact, bytes.NewReader(plistData)); err != nil {
		return fmt.Errorf("adding LastSession.plist file: %w", err)
	}

	// Parse the plist.
	var session sessionData
	decoder := plist.NewDecoder(bytes.NewReader(plistData))
	if err := decoder.Decode(&session); err != nil {
		out.Log(macfor.LogWarn, "Error parsing LastSession.plist for user %s: %v", user.Username, err)
		return fmt.Errorf("parsing LastSession.plist: %w", err)
	}

	// Handle empty session.
	if len(session.SessionWindows) == 0 {
		out.Log(macfor.LogInfo, "No session windows found for user %s", user.Username)
		return nil
	}

	// Convert and output window and tab records.
	windowCount := 0
	tabCount := 0

	for windowIndex, window := range session.SessionWindows {
		// Check for context cancellation.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Create window record.
		windowRecord := convertSessionWindow(window, windowIndex, user.Username, sessionPath)
		if err := out.AddRecord(ctx, *artifact, windowRecord); err != nil {
			out.Log(macfor.LogWarn, "Error adding session window record: %v", err)
		} else {
			windowCount++
		}

		// Create tab records.
		for tabIndex, tab := range window.TabStates {
			// Check for context cancellation.
			if ctx.Err() != nil {
				return ctx.Err()
			}

			tabRecord := convertSessionTab(tab, windowIndex, tabIndex, user.Username, sessionPath)
			if err := out.AddRecord(ctx, *artifact, tabRecord); err != nil {
				out.Log(macfor.LogWarn, "Error adding session tab record: %v", err)
			} else {
				tabCount++
			}
		}
	}

	out.Log(macfor.LogInfo, "Collected %d windows and %d tabs for user %s", windowCount, tabCount, user.Username)
	return nil
}

// convertSessionWindow converts a sessionWindow to a SessionWindowRecord.
func convertSessionWindow(window sessionWindow, windowIndex int, username, sourcePath string) *SessionWindowRecord {
	return &SessionWindowRecord{
		Type:             "browser_session_window",
		Browser:          browserSafari,
		User:             username,
		WindowIndex:      windowIndex,
		WindowUUID:       window.WindowUUID,
		IsPrivate:        window.IsPrivate,
		SelectedTabIndex: window.SelectedTabIndex,
		TabCount:         len(window.TabStates),
		SourceFile:       sourcePath,
	}
}

// convertSessionTab converts a sessionTab to a SessionTabRecord.
func convertSessionTab(tab sessionTab, windowIndex, tabIndex int, username, sourcePath string) *SessionTabRecord {
	return &SessionTabRecord{
		Type:           "browser_session_tab",
		Browser:        browserSafari,
		User:           username,
		WindowIndex:    windowIndex,
		TabIndex:       tabIndex,
		TabUUID:        tab.TabUUID,
		URL:            tab.TabURL,
		Title:          tab.TabTitle,
		LastVisitTime:  tab.LastVisitTime,
		IsAppInitiated: tab.IsAppInitiated,
		SourceFile:     sourcePath,
	}
}
