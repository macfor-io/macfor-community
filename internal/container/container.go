// Package container implements the evidence container writer that creates
// ZIP-based forensic containers with integrity verification.
package container

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/manifestpack"
	macforparquet "github.com/macfor-io/macfor-community/pkg/macfor/parquet"
)

// Container metadata constants.
const (
	// actorMacfor is the actor recorded in chain-of-custody entries.
	actorMacfor = "macfor"

	// custodyKeyPlugin is the chain-of-custody detail key for the plugin ID.
	custodyKeyPlugin = "plugin"

	// containerFormatVersion is the evidence-container format version.
	containerFormatVersion = "1.2"
)

// Config holds configuration for creating an evidence container.
type Config struct {
	// OutputPath is the file system path for the container ZIP file.
	OutputPath string

	// RecordFormat specifies the serialisation format for parsed records.
	// Defaults to FormatJSONL when empty.
	RecordFormat macforparquet.RecordFormat

	// ParquetCompression specifies the compression codec for Parquet files.
	ParquetCompression string
}

// Container manages the creation of a forensic evidence container.
// It writes files to a ZIP archive using the Store method (no compression)
// for forensic integrity, computes SHA-256 hashes for all content, and
// tracks hashes for manifest generation.
//
// The container must be closed with Close() to finalize the archive.
// If an error occurs, the partial container file is automatically removed.
type Container struct {
	outputPath         string
	recordFormat       macforparquet.RecordFormat
	parquetCompression string
	file               *os.File
	zipWriter          *zip.Writer

	// hashes maps file paths within the container to their SHA-256 hashes.
	hashes map[string]string
	mu     sync.Mutex

	// closed tracks whether the container has been finalized.
	closed bool

	// hadError tracks whether any error occurred during operations.
	hadError bool

	// finalized tracks whether Finalize() has been called.
	finalized bool

	// manifest contains the collection manifest data.
	manifest *Manifest

	// custodyLog contains the chain of custody entries.
	custodyLog *ChainOfCustodyLog

	// manifestProvenance records the artifact-manifest definitions that drove
	// the collection. Stored so every manifest-construction path can attach it.
	manifestProvenance *manifestpack.Provenance

	// license records the license the collecting build ran under.
	license *macfor.LicenseInfo

	// collectionStartedAt is when the collection began.
	collectionStartedAt time.Time

	// usersProcessed is the number of user accounts collection covered.
	usersProcessed int
}

// NewContainer creates a new evidence container using the provided configuration.
// The container is created immediately, and all writes are performed directly.
// Returns an error if the file cannot be created.
func NewContainer(cfg Config) (*Container, error) {
	outputPath := cfg.OutputPath

	// Ensure parent directory exists.
	dir := filepath.Dir(outputPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("creating container directory: %w", err)
		}
	}

	// Create the output file.
	// The outputPath is sanitized by the caller (typically from CLI arguments).
	f, err := os.Create(outputPath) //nolint:gosec // outputPath is controlled by the caller
	if err != nil {
		return nil, fmt.Errorf("creating container file: %w", err)
	}

	// Default to JSONL when no format is specified.
	recordFormat := cfg.RecordFormat
	if recordFormat == "" {
		recordFormat = macforparquet.FormatJSONL
	}

	return &Container{
		outputPath:          outputPath,
		recordFormat:        recordFormat,
		parquetCompression:  cfg.ParquetCompression,
		file:                f,
		zipWriter:           zip.NewWriter(f),
		hashes:              make(map[string]string),
		collectionStartedAt: time.Now(),
	}, nil
}

// AddFile adds a file generated during collection (manifest, records, logs)
// to the container. The entry's modified time is the time it is written.
func (c *Container) AddFile(path string, r io.Reader) error {
	return c.AddFileModified(path, time.Now(), r)
}

// AddFileModified adds a file to the container with the specified path,
// stamping the ZIP entry with the given modified time. For collected
// evidence this is the source file's modification time, so extracted files
// carry their original mtime. A zero time leaves the entry undated.
//
// ZIP headers store the modified time to whole-second precision; callers
// needing the exact value must record it separately.
// The content is read from the provided reader, and a SHA-256 hash
// is computed during the write operation using io.TeeReader.
//
// Files are stored without compression (Store method) to preserve
// forensic integrity and allow direct hash verification.
func (c *Container) AddFileModified(path string, modified time.Time, r io.Reader) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("container is closed")
	}

	// Create file header with Store method (no compression)
	header := &zip.FileHeader{
		Name:     path,
		Method:   zip.Store,
		Modified: modified,
	}

	w, err := c.zipWriter.CreateHeader(header)
	if err != nil {
		c.hadError = true
		return fmt.Errorf("creating zip entry %q: %w", path, err)
	}

	// Hash while writing using TeeReader
	hasher := sha256.New()
	tee := io.TeeReader(r, hasher)

	if _, err := io.Copy(w, tee); err != nil {
		c.hadError = true
		return fmt.Errorf("writing to zip entry %q: %w", path, err)
	}

	// Store the hash
	c.hashes[path] = hex.EncodeToString(hasher.Sum(nil))

	return nil
}

// AddJSON adds a JSON-encoded file to the container.
// The value is marshaled with indentation for readability, then
// written to the specified path with hash computation.
func (c *Container) AddJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON for %q: %w", path, err)
	}

	// Create a reader from the marshaled data
	r := &bytesReader{data: data}
	return c.AddFile(path, r)
}

// SetCollectionMetadata configures the collection manifest with source information.
// This should be called once before collection begins. If CollectionID is empty,
// a UUID will be generated automatically.
func (c *Container) SetCollectionMetadata(meta CollectionMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()

	collectionID := meta.CollectionID
	if collectionID == "" {
		collectionID = uuid.New().String()
	}

	var source SourceInfo
	if meta.SystemInfo != nil {
		source = SourceInfoFromSystemInfo(meta.SystemInfo)
	} else {
		source = SourceInfo{
			Type:         meta.SourceType,
			Hostname:     meta.Hostname,
			OSVersion:    meta.OSVersion,
			OSName:       meta.OSName,
			SerialNumber: meta.SerialNumber,
			ImagePath:    meta.ImagePath,
		}
	}

	c.manifest = &Manifest{
		Version:            containerFormatVersion,
		CollectionID:       collectionID,
		ToolVersion:        meta.ToolVersion,
		CollectedAt:        c.collectionStartedAt,
		Source:             source,
		Plugins:            []PluginSummary{},
		Statistics:         CollectionStats{},
		RecordFormat:       string(c.RecordFormat()),
		ManifestProvenance: c.manifestProvenance,
		License:            c.license,
	}

	c.custodyLog = &ChainOfCustodyLog{
		CollectionID: collectionID,
		CreatedAt:    c.collectionStartedAt,
		Entries:      []ChainOfCustodyEntry{},
	}

	// Add initial custody entry
	c.addCustodyEntryLocked(ChainOfCustodyEntry{
		Timestamp: c.collectionStartedAt,
		Action:    CustodyActionCollectionStarted,
		Actor:     actorMacfor,
		Details: map[string]any{
			"tool_version": meta.ToolVersion,
			"source_type":  meta.SourceType,
			"hostname":     meta.Hostname,
		},
	})
}

// SetManifestProvenance records the provenance of the artifact-manifest
// definitions that drove this collection — the canonical embedded manifests, not
// an override — into the manifest and as a chain-of-custody event. Call after
// SetCollectionMetadata. A nil prov is ignored.
func (c *Container) SetManifestProvenance(prov *manifestpack.Provenance) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if prov == nil {
		return
	}
	c.manifestProvenance = prov
	if c.manifest != nil {
		c.manifest.ManifestProvenance = prov
	}

	c.addCustodyEntryLocked(ChainOfCustodyEntry{
		Timestamp: time.Now(),
		Action:    CustodyActionManifestProvenance,
		Actor:     actorMacfor,
		Details: map[string]any{
			"pack_version":   prov.PackVersion,
			"digest":         prov.Digest,
			"manifest_count": len(prov.Entries),
		},
	})
}

// SetLicense records the license the collecting build ran under into the
// manifest and as a chain-of-custody event. Call after SetCollectionMetadata. A
// nil license is ignored.
func (c *Container) SetLicense(license *macfor.LicenseInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if license == nil {
		return
	}
	c.license = license
	if c.manifest != nil {
		c.manifest.License = license
	}

	details := map[string]any{
		"license_id": license.LicenseID,
		"customer":   license.Customer,
		"tier":       license.Tier,
		"key_id":     license.KeyID,
	}
	if !license.ExpiresAt.IsZero() {
		details["expires_at"] = license.ExpiresAt
	}
	c.addCustodyEntryLocked(ChainOfCustodyEntry{
		Timestamp: time.Now(),
		Action:    CustodyActionLicense,
		Actor:     actorMacfor,
		Details:   details,
	})
}

// AddCustodyEntry adds an entry to the chain of custody log.
// This records significant events during the collection process.
func (c *Container) AddCustodyEntry(entry ChainOfCustodyEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.addCustodyEntryLocked(entry)
}

// addCustodyEntryLocked adds a custody entry without acquiring the lock.
// Caller must hold c.mu.
func (c *Container) addCustodyEntryLocked(entry ChainOfCustodyEntry) {
	if c.custodyLog == nil {
		// Initialize with default if SetCollectionMetadata wasn't called
		c.custodyLog = &ChainOfCustodyLog{
			CollectionID: uuid.New().String(),
			CreatedAt:    c.collectionStartedAt,
			Entries:      []ChainOfCustodyEntry{},
		}
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	c.custodyLog.Entries = append(c.custodyLog.Entries, entry)
}

// SetUsersProcessed records the number of user accounts the collection
// covered, reported as users_processed in the manifest statistics.
func (c *Container) SetUsersProcessed(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.usersProcessed = n
}

// AddPluginSummary adds a plugin execution summary to the manifest.
// This should be called after each plugin completes collection.
func (c *Container) AddPluginSummary(summary PluginSummary) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.manifest == nil {
		// Initialize with defaults if SetCollectionMetadata wasn't called.
		c.manifest = &Manifest{
			Version:            containerFormatVersion,
			CollectedAt:        c.collectionStartedAt,
			Plugins:            []PluginSummary{},
			Statistics:         CollectionStats{},
			RecordFormat:       string(c.RecordFormat()),
			ManifestProvenance: c.manifestProvenance,
			License:            c.license,
		}
	}

	c.manifest.Plugins = append(c.manifest.Plugins, summary)

	// Update statistics
	c.manifest.Statistics.PluginsExecuted++
	c.manifest.Statistics.TotalFiles += summary.FilesCollected
	c.manifest.Statistics.TotalRecords += summary.RecordsCreated
	c.manifest.Statistics.TotalErrors += summary.ErrorsEncountered
	c.manifest.Statistics.TotalBytes += summary.BytesCollected
}

// Finalize writes the manifest, chain of custody log, and hash manifest to the container.
// This should be called before Close() to ensure all metadata is written.
// Finalize is idempotent - subsequent calls have no effect.
func (c *Container) Finalize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.finalized {
		return nil
	}
	if c.closed {
		return fmt.Errorf("cannot finalize: container is closed")
	}

	now := time.Now()

	// Ensure manifest exists with defaults.
	if c.manifest == nil {
		c.manifest = &Manifest{
			Version:            containerFormatVersion,
			CollectionID:       uuid.New().String(),
			CollectedAt:        c.collectionStartedAt,
			Plugins:            []PluginSummary{},
			Statistics:         CollectionStats{},
			RecordFormat:       string(c.RecordFormat()),
			ManifestProvenance: c.manifestProvenance,
			License:            c.license,
		}
	}
	c.manifest.CompletedAt = now
	c.manifest.Statistics.UsersProcessed = c.usersProcessed

	// Ensure custody log exists
	if c.custodyLog == nil {
		c.custodyLog = &ChainOfCustodyLog{
			CollectionID: c.manifest.CollectionID,
			CreatedAt:    c.collectionStartedAt,
			Entries:      []ChainOfCustodyEntry{},
		}
	}

	// Add completion custody entry
	c.custodyLog.Entries = append(c.custodyLog.Entries, ChainOfCustodyEntry{
		Timestamp: now,
		Action:    CustodyActionCollectionCompleted,
		Actor:     actorMacfor,
		Details: map[string]any{
			"total_files":   c.manifest.Statistics.TotalFiles,
			"total_records": c.manifest.Statistics.TotalRecords,
			"total_errors":  c.manifest.Statistics.TotalErrors,
		},
	})

	// Create hash manifest
	hashManifest := HashManifest{
		Algorithm:   "sha256",
		GeneratedAt: now,
		Files:       make(map[string]string, len(c.hashes)),
	}
	for k, v := range c.hashes {
		hashManifest.Files[k] = v
	}

	// Write manifest.json (without holding lock for the write itself)
	// We need to temporarily unlock to call AddJSON which acquires the lock
	c.mu.Unlock()

	if err := c.AddJSON("manifest.json", c.manifest); err != nil {
		c.mu.Lock()
		c.hadError = true
		return fmt.Errorf("writing manifest.json: %w", err)
	}

	if err := c.AddJSON("chain-of-custody.json", c.custodyLog); err != nil {
		c.mu.Lock()
		c.hadError = true
		return fmt.Errorf("writing chain-of-custody.json: %w", err)
	}

	if err := c.AddJSON("hashes.json", hashManifest); err != nil {
		c.mu.Lock()
		c.hadError = true
		return fmt.Errorf("writing hashes.json: %w", err)
	}

	c.mu.Lock()
	c.finalized = true
	return nil
}

// Hashes returns a copy of the hash map for all files added to the container.
// The map keys are file paths within the container, and values are hex-encoded
// SHA-256 hashes.
func (c *Container) Hashes() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := make(map[string]string, len(c.hashes))
	for k, v := range c.hashes {
		result[k] = v
	}
	return result
}

// Close finalizes the container and closes all resources.
// If any errors occurred during operations, the container file is removed.
// Close is idempotent and safe to call multiple times.
func (c *Container) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	// Close the zip writer first
	zipErr := c.zipWriter.Close()

	// Close the file
	fileErr := c.file.Close()

	// If there was an error during operations, remove the partial container
	if c.hadError {
		_ = os.Remove(c.outputPath)
		if zipErr != nil {
			return fmt.Errorf("finalizing zip (container removed due to previous errors): %w", zipErr)
		}
		if fileErr != nil {
			return fmt.Errorf("closing file (container removed due to previous errors): %w", fileErr)
		}
		return nil
	}

	if zipErr != nil {
		_ = os.Remove(c.outputPath)
		return fmt.Errorf("finalizing zip: %w", zipErr)
	}
	if fileErr != nil {
		_ = os.Remove(c.outputPath)
		return fmt.Errorf("closing container file: %w", fileErr)
	}

	return nil
}

// Path returns the output path of the container.
func (c *Container) Path() string {
	return c.outputPath
}

// RecordFormat returns the configured record serialisation format.
func (c *Container) RecordFormat() macforparquet.RecordFormat {
	return c.recordFormat
}

// ParquetCompression returns the configured Parquet compression codec.
func (c *Container) ParquetCompression() string {
	return c.parquetCompression
}

// MarkError marks that an error occurred during container operations.
// This causes the container file to be removed on Close().
func (c *Container) MarkError() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hadError = true
}

// bytesReader wraps a byte slice to implement io.Reader.
// This avoids importing bytes package just for bytes.NewReader.
type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
