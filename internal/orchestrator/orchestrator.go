// Package orchestrator provides collection coordination for forensic artifact collection.
// It orchestrates plugin detection and collection phases, providing a reusable component
// that can be used by CLI, GUI, or API implementations.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/manifestpack"
)

// Config contains configuration for the orchestrator.
type Config struct {
	// Source is the forensic source to collect from.
	Source macfor.Source

	// Container is the evidence container to write to.
	// If nil, detection-only mode is used.
	Container *container.Container

	// PluginIDs filters collection to specific plugin IDs.
	// Empty means all plugins.
	PluginIDs []string

	// Category filters collection to plugins matching this category.
	// Empty means no category filter.
	Category string

	// ToolVersion is the version of the tool for manifest metadata.
	ToolVersion string

	// DryRun indicates detection-only mode (no collection).
	DryRun bool

	// License is the license the build runs under, recorded in the evidence
	// container. Nil for builds without a license.
	License *macfor.LicenseInfo
}

// Result contains the results of an orchestration run.
type Result struct {
	// Success indicates whether the collection completed without critical errors.
	Success bool

	// ContainerPath is the path to the evidence container (if created).
	ContainerPath string

	// PluginResults contains results for each plugin that was processed.
	PluginResults []PluginResult

	// TotalFiles is the total number of files collected.
	TotalFiles int

	// TotalRecords is the total number of records created.
	TotalRecords int

	// TotalErrors is the total number of errors encountered.
	TotalErrors int

	// Duration is how long the orchestration took.
	Duration time.Duration

	// DetectionOnly indicates whether this was a detection-only run.
	DetectionOnly bool

	// Cancelled indicates whether the run was cancelled.
	Cancelled bool
}

// PluginResult contains the result of processing a single plugin.
type PluginResult struct {
	// ID is the plugin identifier.
	ID string

	// Name is the plugin's human-readable name.
	Name string

	// Applicable indicates whether the plugin was applicable to the source.
	Applicable bool

	// Reason explains why the plugin is or is not applicable.
	Reason string

	// Collected indicates whether collection was performed.
	Collected bool

	// FilesCollected is the number of files collected.
	FilesCollected int

	// RecordsCreated is the number of records created.
	RecordsCreated int

	// Errors is the number of errors encountered.
	Errors int

	// Skipped indicates the plugin was applicable but collection was skipped.
	Skipped bool

	// DetectionResult contains the full detection result.
	DetectionResult macfor.DetectionResult
}

// ProgressPhase indicates the current phase of orchestration.
type ProgressPhase string

const (
	// PhaseInit indicates the initialization phase.
	PhaseInit ProgressPhase = "init"

	// PhaseDetection indicates the detection phase.
	PhaseDetection ProgressPhase = "detection"

	// PhaseCollection indicates the collection phase.
	PhaseCollection ProgressPhase = "collection"

	// PhaseFinalize indicates the finalization phase.
	PhaseFinalize ProgressPhase = "finalize"

	// PhaseComplete indicates the orchestration is complete.
	PhaseComplete ProgressPhase = "complete"
)

// Progress contains progress information for callbacks.
type Progress struct {
	// Phase is the current orchestration phase.
	Phase ProgressPhase

	// PluginID is the current plugin being processed (if any).
	PluginID string

	// PluginName is the human-readable name of the current plugin.
	PluginName string

	// PluginsTotal is the total number of plugins to process.
	PluginsTotal int

	// PluginsProcessed is the number of plugins processed so far.
	PluginsProcessed int

	// Message is a human-readable progress message.
	Message string
}

// ProgressCallback is called to report progress during orchestration.
type ProgressCallback func(Progress)

// Orchestrator coordinates forensic artifact collection.
type Orchestrator struct {
	config   Config
	progress ProgressCallback
	mu       sync.RWMutex
}

// New creates a new Orchestrator with the given configuration.
func New(config Config) (*Orchestrator, error) {
	if config.Source == nil {
		return nil, errors.New("source is required")
	}

	return &Orchestrator{
		config: config,
	}, nil
}

// SetProgressCallback sets the progress callback function.
func (o *Orchestrator) SetProgressCallback(cb ProgressCallback) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.progress = cb
}

// reportProgress reports progress if a callback is set.
func (o *Orchestrator) reportProgress(p Progress) {
	o.mu.RLock()
	cb := o.progress
	o.mu.RUnlock()

	if cb != nil {
		cb(p)
	}
}

// Run executes the orchestration workflow.
// It performs the following steps:
//  1. Resolves which plugins to run based on configuration
//  2. Runs detection on all selected plugins
//  3. If not dry-run, runs collection on applicable plugins
//  4. Returns aggregated results
func (o *Orchestrator) Run(ctx context.Context) (*Result, error) {
	startTime := time.Now()

	o.reportProgress(Progress{
		Phase:   PhaseInit,
		Message: "Initializing collection",
	})

	// Resolve plugins.
	plugins, err := o.resolvePlugins()
	if err != nil {
		return nil, fmt.Errorf("resolving plugins: %w", err)
	}

	if len(plugins) == 0 {
		return &Result{
			Success:       true,
			Duration:      time.Since(startTime),
			DetectionOnly: o.config.DryRun,
		}, nil
	}

	// Run detection phase.
	detectionResults := o.runDetection(ctx, plugins)

	// Check for cancellation after detection.
	if ctx.Err() != nil {
		return &Result{
			Success:       false,
			Cancelled:     true,
			Duration:      time.Since(startTime),
			DetectionOnly: o.config.DryRun,
			PluginResults: o.buildPluginResults(plugins, detectionResults, nil),
		}, ctx.Err()
	}

	// If dry-run mode, return detection results only.
	if o.config.DryRun || o.config.Container == nil {
		o.reportProgress(Progress{
			Phase:   PhaseComplete,
			Message: "Detection complete",
		})
		return &Result{
			Success:       true,
			Duration:      time.Since(startTime),
			DetectionOnly: true,
			PluginResults: o.buildPluginResults(plugins, detectionResults, nil),
		}, nil
	}

	// Filter to applicable plugins.
	applicablePlugins := o.filterApplicable(plugins, detectionResults)

	if len(applicablePlugins) == 0 {
		return &Result{
			Success:       true,
			Duration:      time.Since(startTime),
			PluginResults: o.buildPluginResults(plugins, detectionResults, nil),
		}, nil
	}

	// Set up collection metadata.
	o.setupCollectionMetadata()

	// Record the provenance of the manifest definitions driving this collection.
	o.recordManifestProvenance(applicablePlugins)

	// Record the license the build runs under.
	o.config.Container.SetLicense(o.config.License)

	// Run collection phase.
	collectionResults := o.runCollection(ctx, applicablePlugins, detectionResults)

	// Check for cancellation after collection.
	cancelled := ctx.Err() != nil

	// Finalize container.
	o.reportProgress(Progress{
		Phase:   PhaseFinalize,
		Message: "Finalizing evidence container",
	})

	if err := o.config.Container.Finalize(); err != nil {
		return nil, fmt.Errorf("finalizing container: %w", err)
	}

	// Build final result.
	result := o.buildResult(plugins, detectionResults, collectionResults, startTime, cancelled)

	o.reportProgress(Progress{
		Phase:   PhaseComplete,
		Message: "Collection complete",
	})

	return result, nil
}

// resolvePlugins determines which plugins to run based on configuration.
func (o *Orchestrator) resolvePlugins() ([]macfor.Plugin, error) {
	// If specific plugins requested, look them up by ID.
	if len(o.config.PluginIDs) > 0 {
		var plugins []macfor.Plugin
		for _, id := range o.config.PluginIDs {
			p, ok := macfor.ByID(id)
			if !ok {
				return nil, fmt.Errorf("plugin not found: %s", id)
			}
			plugins = append(plugins, p)
		}
		return plugins, nil
	}

	// If category specified, filter by category.
	if o.config.Category != "" {
		return macfor.ByCategory(o.config.Category), nil
	}

	// Otherwise, return all plugins.
	return macfor.All(), nil
}

// runDetection runs detection on all plugins and returns results.
func (o *Orchestrator) runDetection(ctx context.Context, plugins []macfor.Plugin) map[string]macfor.DetectionResult {
	results := make(map[string]macfor.DetectionResult)

	o.reportProgress(Progress{
		Phase:        PhaseDetection,
		PluginsTotal: len(plugins),
		Message:      fmt.Sprintf("Running detection on %d plugins", len(plugins)),
	})

	for i, p := range plugins {
		info := p.Info()

		// Check for cancellation.
		if ctx.Err() != nil {
			break
		}

		o.reportProgress(Progress{
			Phase:            PhaseDetection,
			PluginID:         info.ID,
			PluginName:       info.Name,
			PluginsTotal:     len(plugins),
			PluginsProcessed: i,
			Message:          fmt.Sprintf("Detecting %s", info.Name),
		})

		result, err := p.Detect(ctx, o.config.Source)
		if err != nil {
			result = macfor.DetectionResult{
				Applicable: false,
				Reason:     fmt.Sprintf("detection error: %v", err),
			}
		}

		results[info.ID] = result
	}

	return results
}

// filterApplicable returns only the plugins that are applicable.
func (o *Orchestrator) filterApplicable(plugins []macfor.Plugin, detectionResults map[string]macfor.DetectionResult) []macfor.Plugin {
	var applicable []macfor.Plugin
	for _, p := range plugins {
		if result, ok := detectionResults[p.Info().ID]; ok && result.Applicable {
			applicable = append(applicable, p)
		}
	}
	return applicable
}

// setupCollectionMetadata configures the container with collection metadata.
// If SystemInfo is available, it provides full structured data for the manifest.
// Falls back to individual Hostname/OSVersion calls if SystemInfo fails.
func (o *Orchestrator) setupCollectionMetadata() {
	meta := container.CollectionMeta{
		ToolVersion: o.config.ToolVersion,
		SourceType:  string(o.config.Source.Type()),
	}

	// Try to get full system info for structured manifest data.
	if sysInfo, err := o.config.Source.SystemInfo(); err == nil && sysInfo != nil {
		meta.SystemInfo = sysInfo
		meta.Hostname = sysInfo.Hostname
		meta.OSVersion = sysInfo.OSVersion
		meta.OSName = sysInfo.OSName
		meta.SerialNumber = sysInfo.SerialNumber
	} else {
		// Fallback to individual calls.
		meta.Hostname, _ = o.config.Source.Hostname()
		meta.OSVersion, _ = o.config.Source.OSVersion()
	}

	o.config.Container.SetCollectionMetadata(meta)
}

// recordManifestProvenance records the provenance of the artifact-manifest
// definitions that drove this collection — the canonical embedded manifests of
// the plugins that are about to collect, gathered via the ManifestProvider
// interface — into the evidence container's manifest and chain of custody. The
// plugin remains the source of truth for collection; this only attests to which
// manifest content was used, so a later analyst can verify it. Best-effort:
// failures here never abort a collection.
func (o *Orchestrator) recordManifestProvenance(plugins []macfor.Plugin) {
	manifests := make(map[string]*artifacts.Manifest, len(plugins))
	for _, p := range plugins {
		mp, ok := p.(macfor.ManifestProvider)
		if !ok {
			continue
		}
		if m := mp.Manifest(); m != nil {
			manifests[p.Info().ID] = m
		}
	}
	if len(manifests) == 0 {
		return
	}

	pack, err := manifestpack.FromManifests(o.config.ToolVersion, manifests)
	if err != nil {
		return
	}
	prov := manifestpack.BuildProvenance(pack)
	o.config.Container.SetManifestProvenance(&prov)
}

// runCollection runs collection on applicable plugins.
func (o *Orchestrator) runCollection(ctx context.Context, plugins []macfor.Plugin, detectionResults map[string]macfor.DetectionResult) map[string]container.PluginSummary {
	results := make(map[string]container.PluginSummary)

	if users, err := o.config.Source.Users(); err == nil {
		o.config.Container.SetUsers(users)
	}

	o.reportProgress(Progress{
		Phase:        PhaseCollection,
		PluginsTotal: len(plugins),
		Message:      fmt.Sprintf("Collecting from %d applicable plugins", len(plugins)),
	})

	for i, p := range plugins {
		info := p.Info()

		// Check for cancellation.
		if ctx.Err() != nil {
			o.config.Container.MarkError()
			break
		}

		o.reportProgress(Progress{
			Phase:            PhaseCollection,
			PluginID:         info.ID,
			PluginName:       info.Name,
			PluginsTotal:     len(plugins),
			PluginsProcessed: i,
			Message:          fmt.Sprintf("Collecting %s", info.Name),
		})

		// Create collector for this plugin.
		coll := container.NewCollector(o.config.Container, info.ID)

		// Add plugin started custody entry.
		o.config.Container.AddCustodyEntry(container.ChainOfCustodyEntry{
			Timestamp: time.Now(),
			Action:    container.CustodyActionPluginStarted,
			Actor:     "macfor",
			Target:    info.ID,
			Details: map[string]any{
				"plugin_name":    info.Name,
				"plugin_version": info.Version,
			},
		})

		// Run collection. Errors the plugin returns are recorded before
		// finalizing so they are counted in the summary and manifest.
		if err := p.Collect(ctx, o.config.Source, coll); err != nil {
			coll.RecordCollectError(err)
		}

		// Finalize collector and get summary.
		summary, _ := coll.Finalize()
		summary.Name = info.Name
		summary.Version = info.Version

		// Add detection metadata to summary if available.
		if detection, ok := detectionResults[info.ID]; ok {
			if summary.ID == "" {
				summary.ID = info.ID
			}
			// Detection metadata could be stored if needed.
			_ = detection
		}

		// Add plugin completed custody entry.
		o.config.Container.AddCustodyEntry(container.ChainOfCustodyEntry{
			Timestamp: time.Now(),
			Action:    container.CustodyActionPluginCompleted,
			Actor:     "macfor",
			Target:    info.ID,
			Details: map[string]any{
				"files_collected":    summary.FilesCollected,
				"records_created":    summary.RecordsCreated,
				"errors_encountered": summary.ErrorsEncountered,
			},
		})

		// Add summary to container.
		o.config.Container.AddPluginSummary(summary)
		results[info.ID] = summary
	}

	return results
}

// buildPluginResults builds PluginResult slice from detection and collection results.
func (o *Orchestrator) buildPluginResults(plugins []macfor.Plugin, detectionResults map[string]macfor.DetectionResult, collectionResults map[string]container.PluginSummary) []PluginResult {
	results := make([]PluginResult, 0, len(plugins))

	// Sort plugins by ID for consistent output.
	sortedPlugins := make([]macfor.Plugin, len(plugins))
	copy(sortedPlugins, plugins)
	sort.Slice(sortedPlugins, func(i, j int) bool {
		return sortedPlugins[i].Info().ID < sortedPlugins[j].Info().ID
	})

	for _, p := range sortedPlugins {
		info := p.Info()
		detection := detectionResults[info.ID]

		pr := PluginResult{
			ID:              info.ID,
			Name:            info.Name,
			Applicable:      detection.Applicable,
			Reason:          detection.Reason,
			DetectionResult: detection,
		}

		if collectionResults != nil {
			if summary, ok := collectionResults[info.ID]; ok {
				pr.Collected = true
				pr.FilesCollected = summary.FilesCollected
				pr.RecordsCreated = summary.RecordsCreated
				pr.Errors = summary.ErrorsEncountered
			} else if detection.Applicable {
				// Plugin was applicable but didn't run (e.g., cancelled).
				pr.Skipped = true
			}
		}

		results = append(results, pr)
	}

	return results
}

// buildResult builds the final Result from all gathered data.
func (o *Orchestrator) buildResult(plugins []macfor.Plugin, detectionResults map[string]macfor.DetectionResult, collectionResults map[string]container.PluginSummary, startTime time.Time, cancelled bool) *Result {
	pluginResults := o.buildPluginResults(plugins, detectionResults, collectionResults)

	result := &Result{
		Success:       true,
		Duration:      time.Since(startTime),
		PluginResults: pluginResults,
		Cancelled:     cancelled,
	}

	// Calculate totals.
	for _, pr := range pluginResults {
		result.TotalFiles += pr.FilesCollected
		result.TotalRecords += pr.RecordsCreated
		result.TotalErrors += pr.Errors
	}

	// Mark as failed if there were errors or was cancelled.
	if result.TotalErrors > 0 || cancelled {
		result.Success = false
	}

	return result
}

// ApplicablePlugins returns the count of applicable plugins from the results.
func (r *Result) ApplicablePlugins() int {
	count := 0
	for _, pr := range r.PluginResults {
		if pr.Applicable {
			count++
		}
	}
	return count
}

// CollectedPlugins returns the count of plugins that performed collection.
func (r *Result) CollectedPlugins() int {
	count := 0
	for _, pr := range r.PluginResults {
		if pr.Collected {
			count++
		}
	}
	return count
}
