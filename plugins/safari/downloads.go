package safari

import (
	"context"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/plistmapper"
)

// DownloadState represents the state of a Safari download.
type DownloadState string

// Download state constants.
const (
	// DownloadStateComplete indicates the download finished successfully.
	DownloadStateComplete DownloadState = "complete"

	// DownloadStateIncomplete indicates the download was interrupted or is still in progress.
	DownloadStateIncomplete DownloadState = "incomplete"
)

// DownloadRecord represents a single Safari download entry from Downloads.plist.
// Each record corresponds to one download that Safari has tracked.
type DownloadRecord struct {
	// Type identifies this record type for downstream processing.
	Type string `json:"type"`

	// Browser identifies the source browser.
	Browser string `json:"browser"`

	// User is the username who performed this download.
	User string `json:"user"`

	// URL is the original download URL.
	URL string `json:"url"`

	// LocalPath is the full path where the file was saved.
	LocalPath string `json:"local_path"`

	// FileName is the downloaded file name (extracted from LocalPath).
	FileName string `json:"file_name"`

	// TotalBytes is the total size of the download in bytes.
	TotalBytes int64 `json:"total_bytes"`

	// ReceivedBytes is the number of bytes downloaded so far.
	ReceivedBytes int64 `json:"received_bytes"`

	// StartTime is when the download was initiated.
	StartTime time.Time `json:"start_time"`

	// EndTime is when the download completed (zero value if incomplete).
	EndTime time.Time `json:"end_time,omitempty"`

	// State indicates whether the download is complete or incomplete.
	State DownloadState `json:"state"`

	// Identifier is a unique UUID for this download entry.
	// Useful for forensic correlation and tracking.
	Identifier string `json:"identifier"`

	// RemoveWhenDone indicates if Safari should remove the entry after completion.
	RemoveWhenDone bool `json:"remove_when_done"`

	// SourceFile is the path to the Downloads.plist file on the source system.
	SourceFile string `json:"source_file"`
}

// downloadStateDeriver is the manifest deriver name for the download state.
const downloadStateDeriver = "safari.download_state"

// pluginDerivers maps every deriver name referenced from extract blocks in
// artifacts.yaml to its implementation. The dispatch guard test
// (TestManifestDeriversAreRegistered) keeps the manifest and this registry in
// sync, so a typo'd or missing deriver fails at test time, not collection time.
var pluginDerivers = map[string]plistmapper.Deriver{
	downloadStateDeriver: downloadState,
}

// collectDownloadsForUser collects Safari download history from Downloads.plist
// via the declarative plist mapper (driven by the manifest's extract block).
func collectDownloadsForUser(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	return collectPlistForUser(ctx, user, src, out, plistCollectSpec{
		artifactType: ArtifactDownloads,
		category:     categoryBrowser,
		artifactKind: "downloads",
		description:  "Safari download history",
		newRecord:    func() any { return &DownloadRecord{} },
		derivers:     pluginDerivers,
	})
}

// downloadState derives complete/incomplete from a download element's byte
// counts: complete when all expected bytes were received. This cross-field
// logic is the field-level escape hatch that the declarative mapper delegates.
func downloadState(element map[string]any) (any, error) {
	received := plistInt64(element["DownloadEntryProgressBytesSoFar"])
	total := plistInt64(element["DownloadEntryProgressTotalToLoad"])
	if received >= total && total > 0 {
		return string(DownloadStateComplete), nil
	}
	return string(DownloadStateIncomplete), nil
}

// plistInt64 coerces a plist-decoded numeric value to int64, returning 0 for
// absent or non-numeric values.
func plistInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case uint64:
		if n > 1<<62 {
			return 0
		}
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}
