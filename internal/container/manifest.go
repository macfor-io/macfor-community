// Package container implements the evidence container writer that creates
// ZIP-based forensic containers with integrity verification.
package container

import (
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/manifestpack"
)

// Manifest contains all metadata about a forensic evidence collection.
// It is written as manifest.json in the evidence container.
type Manifest struct {
	// Version is the manifest schema version.
	Version string `json:"version"`

	// CollectionID is the unique identifier for this collection.
	CollectionID string `json:"collection_id"`

	// ToolVersion is the version of the macfor tool that performed the collection.
	ToolVersion string `json:"tool_version"`

	// CollectedAt is when the collection started.
	CollectedAt time.Time `json:"collected_at"`

	// CompletedAt is when the collection finished.
	CompletedAt time.Time `json:"completed_at"`

	// Source contains information about the source system.
	Source SourceInfo `json:"source"`

	// Plugins contains execution summary for each plugin that ran.
	Plugins []PluginSummary `json:"plugins"`

	// Statistics contains aggregate collection statistics.
	Statistics CollectionStats `json:"statistics"`

	// RecordFormat indicates the format used for parsed record files.
	RecordFormat string `json:"record_format"`

	// ManifestProvenance records the artifact-manifest definitions that drove
	// this collection (version + per-plugin content hashes + overall digest),
	// so an analyst can later prove which manifest content the collection used.
	ManifestProvenance *manifestpack.Provenance `json:"manifest_provenance,omitempty"`
}

// SourceInfo contains information about the source system being collected.
type SourceInfo struct {
	// Legacy flat fields (kept for backward compatibility).
	Type         string `json:"type"`
	Hostname     string `json:"hostname"`
	OSVersion    string `json:"os_version"`
	OSName       string `json:"os_name,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	ImagePath    string `json:"image_path,omitempty"`

	// Structured system info (MACFOR-SYS-8).
	OS            *OSInfo            `json:"os,omitempty"`
	Hardware      *HardwareInfo      `json:"hardware,omitempty"`
	Storage       *StorageInfo       `json:"storage,omitempty"`
	Security      *SecurityInfo      `json:"security,omitempty"`
	Network       *NetworkInfo       `json:"network,omitempty"`
	Temporal      *TemporalInfo      `json:"temporal,omitempty"`
	CollectionEnv *CollectionEnvInfo `json:"collection_env,omitempty"`
}

// OSInfo contains operating system details.
type OSInfo struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Build         string `json:"build,omitempty"`
	Codename      string `json:"codename,omitempty"`
	KernelVersion string `json:"kernel_version,omitempty"`
}

// HardwareInfo contains hardware details.
type HardwareInfo struct {
	Model        string `json:"model,omitempty"`
	ModelName    string `json:"model_name,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	Architecture string `json:"architecture"`
	CPUType      string `json:"cpu_type,omitempty"`
	CPUCores     int    `json:"cpu_cores,omitempty"`
	MemoryGB     int    `json:"memory_gb,omitempty"`
}

// StorageInfo contains boot volume details.
type StorageInfo struct {
	BootVolumeName      string `json:"boot_volume_name,omitempty"`
	BootVolumeUUID      string `json:"boot_volume_uuid,omitempty"`
	BootVolumeFS        string `json:"boot_volume_fs,omitempty"`
	BootVolumeEncrypted bool   `json:"boot_volume_encrypted"`
}

// SecurityInfo contains security configuration state.
type SecurityInfo struct {
	SIPEnabled        bool   `json:"sip_enabled"`
	FileVaultEnabled  bool   `json:"filevault_enabled"`
	SecureBootLevel   string `json:"secure_boot_level,omitempty"`
	GatekeeperEnabled bool   `json:"gatekeeper_enabled"`
}

// NetworkInfo contains network identity details.
type NetworkInfo struct {
	Hostname      string `json:"hostname,omitempty"`
	LocalHostname string `json:"local_hostname,omitempty"`
	ComputerName  string `json:"computer_name,omitempty"`
}

// TemporalInfo contains temporal context.
type TemporalInfo struct {
	Timezone       string    `json:"timezone,omitempty"`
	TimezoneOffset string    `json:"timezone_offset,omitempty"`
	BootTime       time.Time `json:"boot_time,omitempty"`
	UptimeSeconds  int64     `json:"uptime_seconds,omitempty"`
	CollectionTime time.Time `json:"collection_time"`
}

// CollectionEnvInfo contains information about the collection environment.
type CollectionEnvInfo struct {
	User   string `json:"user"`
	UID    int    `json:"uid"`
	IsRoot bool   `json:"is_root"`
}

// SourceInfoFromSystemInfo populates a SourceInfo from a SystemInfo struct.
// It sets both legacy flat fields and the new nested structs for backward compatibility.
func SourceInfoFromSystemInfo(si *macfor.SystemInfo) SourceInfo {
	info := SourceInfo{
		// Legacy flat fields.
		Type:         string(si.SourceType),
		Hostname:     si.Hostname,
		OSVersion:    si.OSVersion,
		OSName:       si.OSName,
		SerialNumber: si.SerialNumber,
		ImagePath:    si.ImagePath,

		// Nested structs.
		OS: &OSInfo{
			Name:          si.OSName,
			Version:       si.OSVersion,
			Build:         si.OSBuild,
			Codename:      si.OSCodename,
			KernelVersion: si.KernelVersion,
		},
		Hardware: &HardwareInfo{
			Model:        si.HardwareModel,
			ModelName:    si.HardwareModelName,
			SerialNumber: si.SerialNumber,
			Architecture: si.Architecture,
			CPUType:      si.CPUType,
			CPUCores:     si.CPUCores,
			MemoryGB:     si.MemoryGB,
		},
		Storage: &StorageInfo{
			BootVolumeName:      si.BootVolumeName,
			BootVolumeUUID:      si.BootVolumeUUID,
			BootVolumeFS:        si.BootVolumeFS,
			BootVolumeEncrypted: si.BootVolumeEncrypted,
		},
		Security: &SecurityInfo{
			SIPEnabled:        si.SIPEnabled,
			FileVaultEnabled:  si.FileVaultEnabled,
			SecureBootLevel:   si.SecureBootLevel,
			GatekeeperEnabled: si.GatekeeperEnabled,
		},
		Network: &NetworkInfo{
			Hostname:      si.Hostname,
			LocalHostname: si.LocalHostname,
			ComputerName:  si.ComputerName,
		},
		Temporal: &TemporalInfo{
			Timezone:       si.Timezone,
			TimezoneOffset: si.TimezoneOffset,
			BootTime:       si.BootTime,
			UptimeSeconds:  si.UptimeSeconds,
			CollectionTime: si.CollectionTime,
		},
		CollectionEnv: &CollectionEnvInfo{
			User:   si.CollectionUser,
			UID:    si.CollectionUID,
			IsRoot: si.IsRoot,
		},
	}

	return info
}

// PluginSummary contains the execution summary for a single plugin.
type PluginSummary struct {
	// ID is the plugin identifier (e.g., "shell.history").
	ID string `json:"id"`

	// Name is the human-readable plugin name.
	Name string `json:"name"`

	// Version is the plugin version.
	Version string `json:"version"`

	// StartedAt is when plugin execution started.
	StartedAt time.Time `json:"started_at"`

	// CompletedAt is when plugin execution completed.
	CompletedAt time.Time `json:"completed_at"`

	// FilesCollected is the number of files collected by this plugin.
	FilesCollected int `json:"files_collected"`

	// RecordsCreated is the number of parsed records created.
	RecordsCreated int `json:"records_created"`

	// ErrorsEncountered is the number of errors during collection.
	ErrorsEncountered int `json:"errors_encountered"`

	// Errors contains details of errors encountered.
	Errors []PluginError `json:"errors,omitempty"`
}

// PluginError represents an error that occurred during plugin execution.
type PluginError struct {
	// Artifact is the artifact that caused the error, if applicable.
	Artifact string `json:"artifact,omitempty"`

	// Operation is the operation that failed.
	Operation string `json:"operation"`

	// Message is the error message.
	Message string `json:"message"`

	// Timestamp is when the error occurred.
	Timestamp time.Time `json:"timestamp"`
}

// CollectionStats contains aggregate statistics for the entire collection.
type CollectionStats struct {
	// TotalFiles is the total number of files collected.
	TotalFiles int `json:"total_files"`

	// TotalRecords is the total number of parsed records created.
	TotalRecords int `json:"total_records"`

	// TotalBytes is the total size of collected files in bytes.
	TotalBytes int64 `json:"total_bytes"`

	// TotalErrors is the total number of errors encountered.
	TotalErrors int `json:"total_errors"`

	// PluginsExecuted is the number of plugins that ran.
	PluginsExecuted int `json:"plugins_executed"`

	// UsersProcessed is the number of user accounts processed.
	UsersProcessed int `json:"users_processed"`
}

// ChainOfCustodyLog contains the complete chain of custody record.
// It is written as chain-of-custody.json in the evidence container.
type ChainOfCustodyLog struct {
	// CollectionID links this log to the collection manifest.
	CollectionID string `json:"collection_id"`

	// CreatedAt is when this custody log was created.
	CreatedAt time.Time `json:"created_at"`

	// Entries contains all custody events in chronological order.
	Entries []ChainOfCustodyEntry `json:"entries"`
}

// ChainOfCustodyEntry represents a single event in the chain of custody.
type ChainOfCustodyEntry struct {
	// Timestamp is when this event occurred.
	Timestamp time.Time `json:"timestamp"`

	// Action describes what happened (e.g., "collection_started", "file_added", "collection_completed").
	Action string `json:"action"`

	// Actor identifies who or what performed the action (e.g., tool name, username).
	Actor string `json:"actor"`

	// Target is the subject of the action (e.g., file path, plugin ID).
	Target string `json:"target,omitempty"`

	// Details contains additional action-specific information.
	Details map[string]any `json:"details,omitempty"`

	// Hash is the SHA-256 hash of the target if applicable.
	Hash string `json:"hash,omitempty"`
}

// HashManifest contains SHA-256 hashes for all files in the container.
// It is written as hashes.json in the evidence container.
type HashManifest struct {
	// Algorithm is the hash algorithm used (always "sha256").
	Algorithm string `json:"algorithm"`

	// GeneratedAt is when this manifest was generated.
	GeneratedAt time.Time `json:"generated_at"`

	// Files maps container paths to their SHA-256 hashes.
	Files map[string]string `json:"files"`
}

// CollectionMeta contains metadata to configure the collection manifest.
// This is passed to SetCollectionMetadata before collection begins.
type CollectionMeta struct {
	// CollectionID is the unique identifier for this collection.
	// If empty, a UUID will be generated.
	CollectionID string

	// ToolVersion is the version of the macfor tool.
	ToolVersion string

	// SourceType is "live" or "image".
	SourceType string

	// Hostname is the source system hostname.
	Hostname string

	// OSVersion is the operating system version.
	OSVersion string

	// OSName is the operating system name (optional).
	OSName string

	// SerialNumber is the hardware serial number (optional).
	SerialNumber string

	// ImagePath is the disk image path for image sources (optional).
	ImagePath string

	// SystemInfo is the full system information, if available.
	// When set, it populates both legacy and nested fields in the manifest.
	SystemInfo *macfor.SystemInfo
}

// CustodyAction constants for chain of custody entries.
const (
	CustodyActionCollectionStarted   = "collection_started"
	CustodyActionCollectionCompleted = "collection_completed"
	CustodyActionManifestProvenance  = "manifest_provenance"
	CustodyActionFileAdded           = "file_added"
	CustodyActionRecordAdded         = "record_added"
	CustodyActionPluginStarted       = "plugin_started"
	CustodyActionPluginCompleted     = "plugin_completed"
	CustodyActionError               = "error"
)
