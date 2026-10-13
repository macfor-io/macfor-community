package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/internal/orchestrator"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	macforparquet "github.com/macfor-io/macfor-community/pkg/macfor/parquet"
	"github.com/macfor-io/macfor-community/sources/directory"
	"github.com/macfor-io/macfor-community/sources/live"
	"github.com/spf13/cobra"
)

// Collect command flags.
var (
	collectOutput             string
	collectPlugins            []string
	collectCategory           string
	collectDryRun             bool
	collectJSON               bool
	collectFrom               string
	collectRecordFormat       string
	collectParquetCompression string
)

// CollectResult represents the result of a collection run.
type CollectResult struct {
	Success       bool              `json:"success"`
	ContainerPath string            `json:"container_path,omitempty"`
	PluginsRun    []PluginRunResult `json:"plugins_run"`
	TotalFiles    int               `json:"total_files"`
	TotalRecords  int               `json:"total_records"`
	TotalErrors   int               `json:"total_errors"`
	Duration      string            `json:"duration"`
	DurationMs    int64             `json:"duration_ms"`
	DetectionOnly bool              `json:"detection_only,omitempty"`
}

// PluginRunResult represents the result of a single plugin execution.
type PluginRunResult struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Applicable     bool   `json:"applicable"`
	Reason         string `json:"reason,omitempty"`
	FilesCollected int    `json:"files_collected,omitempty"`
	RecordsCreated int    `json:"records_created,omitempty"`
	Errors         int    `json:"errors,omitempty"`
	Skipped        bool   `json:"skipped,omitempty"`
}

// CLI command-name constants.
const (
	cmdCollect  = "collect"
	cmdList     = "list"
	cmdPlugins  = "plugins"
	cmdManifest = "manifest"
	cmdPaths    = "paths"
	cmdDocs     = "docs"

	// shortCollect is the collect command's short description.
	shortCollect = "Collect forensic artifacts"
)

// newCollectCmd creates and returns the collect command.
func newCollectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdCollect,
		Short: shortCollect,
		Long: `Collect forensic artifacts from the current macOS system.

The collect command discovers applicable plugins, runs detection to determine
which artifacts are available, and collects them into an evidence container.

The evidence container is a ZIP file containing:
  - manifest.json: Collection metadata and statistics
  - chain-of-custody.json: Audit log of all collection actions
  - hashes.json: SHA-256 hashes for all collected files
  - artifacts/: Raw collected files organized by plugin and user
  - records/: Parsed structured data in JSONL format
  - logs/: Plugin-specific collection logs

Examples:
  # Collect all artifacts to an evidence container
  macfor collect --output ./evidence.zip

  # Collect only shell history artifacts
  macfor collect --plugin shell.history --output ./evidence.zip

  # Collect artifacts from a specific category
  macfor collect --category shell --output ./evidence.zip

  # Preview what would be collected (dry run)
  macfor collect --dry-run --output ./evidence.zip

  # Output results in JSON format
  macfor collect --output ./evidence.zip --json

  # Collect from a mounted disk image or snapshot directory
  macfor collect --from /Volumes/Macintosh\ HD --output ./evidence.zip
  macfor collect --from /tmp/macos-snapshot --dry-run

Exit codes:
  0  Collection completed without errors
  1  Collection failed
  2  Collection completed with recovered errors (partial); the evidence
     container was written and the errors are recorded in it`,
		RunE: runCollect,
	}

	// Add command flags.
	cmd.Flags().StringVarP(&collectOutput, "output", "o", "", "Output path for evidence container (required)")
	cmd.Flags().StringSliceVarP(&collectPlugins, "plugin", "p", nil, "Specific plugin(s) to run (can be specified multiple times)")
	cmd.Flags().StringVarP(&collectCategory, "category", "c", "", "Filter plugins by category")
	cmd.Flags().BoolVar(&collectDryRun, "dry-run", false, "Show what would be collected without collecting")
	cmd.Flags().BoolVar(&collectJSON, "json", false, "Output results in JSON format")
	cmd.Flags().StringVarP(&collectFrom, "from", "f", "", "Collect from a directory tree instead of the live system (e.g. a mounted disk image)")
	cmd.Flags().StringVar(&collectRecordFormat, "record-format", "jsonl",
		`Record output format: "jsonl" (default), "parquet", or "both"`)
	cmd.Flags().StringVar(&collectParquetCompression, "parquet-compression", "snappy",
		`Parquet compression codec: "snappy" (default), "zstd", "gzip", or "none"`)

	// Mark output as required for non-dry-run mode.
	// We handle this in runCollect to allow --dry-run without --output.

	return cmd
}

// runCollect executes the collect command.
func runCollect(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	// Set up signal handling for graceful shutdown.
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Validate flags.
	if !collectDryRun && collectOutput == "" {
		return fmt.Errorf("--output is required (or use --dry-run to preview)")
	}

	// Validate record format and compression flags.
	if _, err := macforparquet.ParseRecordFormat(collectRecordFormat); err != nil {
		return fmt.Errorf("invalid --record-format: %w", err)
	}
	if _, err := macforparquet.ParseCompression(collectParquetCompression); err != nil {
		return fmt.Errorf("invalid --parquet-compression: %w", err)
	}

	// Create source (live or directory).
	src, err := createSource(collectFrom)
	if err != nil {
		return err
	}
	if collectFrom == "" {
		warnIfNoFullDiskAccess(cmd)
	}

	// Run detection first to check if we have applicable plugins.
	detectionResult, err := runDetectionPhase(ctx, src)
	if err != nil {
		return err
	}

	// Handle early exit conditions.
	if done, exitErr := handleDetectionResult(cmd, detectionResult); done {
		return exitErr
	}

	// Run collection phase.
	return runCollectionPhase(ctx, cmd, src, detectionResult)
}

// fullDiskAccess reports whether the process has Full Disk Access. It is a
// variable so tests can replace the probe.
var fullDiskAccess = live.FullDiskAccess

// warnIfNoFullDiskAccess warns on stderr when collecting from the live system
// without Full Disk Access, since macOS will deny reads of protected
// artifacts such as Safari data. Collection continues; the denials are
// recorded as errors in the container. It is silent with --quiet or --json;
// the manifest records full_disk_access either way.
func warnIfNoFullDiskAccess(cmd *cobra.Command) {
	if quiet || collectJSON {
		return
	}
	if granted, known := fullDiskAccess(); !known || granted {
		return
	}
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), `Warning: macfor does not have Full Disk Access.
  macOS will block access to protected artifacts (e.g. Safari history,
  cookies and downloads), and those artifacts will be recorded as errors.
  Grant access in System Settings > Privacy & Security > Full Disk Access
  to the app running macfor (e.g. Terminal), then restart that app.

`)
}

// createSource creates the appropriate Source based on the --from flag.
// If fromPath is empty, returns a live system source.
// If fromPath is set, validates the path and returns a directory source.
func createSource(fromPath string) (macfor.Source, error) {
	if fromPath == "" {
		src, err := live.New()
		if err != nil {
			return nil, fmt.Errorf("creating live source: %w", err)
		}
		return src, nil
	}

	// Validate path exists before instantiating.
	info, err := os.Stat(fromPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("path does not exist: %s", fromPath)
		}
		return nil, fmt.Errorf("checking path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", fromPath)
	}

	src, err := directory.New(fromPath)
	if err != nil {
		return nil, fmt.Errorf("creating directory source: %w", err)
	}
	return src, nil
}

// runDetectionPhase runs the detection phase to identify applicable plugins.
func runDetectionPhase(ctx context.Context, src macfor.Source) (*orchestrator.Result, error) {
	config := orchestrator.Config{
		Source:      src,
		PluginIDs:   collectPlugins,
		Category:    collectCategory,
		ToolVersion: Version,
		DryRun:      true,
	}

	orch, err := orchestrator.New(config)
	if err != nil {
		return nil, fmt.Errorf("creating orchestrator: %w", err)
	}

	orch.SetProgressCallback(makeProgressCallback())
	return orch.Run(ctx)
}

// handleDetectionResult handles early exit conditions after detection.
// Returns true if we should exit (done), along with any error.
func handleDetectionResult(cmd *cobra.Command, result *orchestrator.Result) (bool, error) {
	if result.Cancelled {
		logger.Warn().Msg("Collection cancelled by user")
		return true, context.Canceled
	}

	if len(result.PluginResults) == 0 {
		return true, outputNoPluginsMatched(cmd, result)
	}

	if collectDryRun {
		return true, outputDryRunResultsFromOrchestrator(cmd, result)
	}

	if result.ApplicablePlugins() == 0 {
		return true, outputNoApplicablePlugins(cmd, result)
	}

	return false, nil
}

// outputNoPluginsMatched outputs the "no plugins matched" message.
func outputNoPluginsMatched(cmd *cobra.Command, result *orchestrator.Result) error {
	if collectJSON {
		return outputCollectResultJSON(cmd, convertOrchestratorResult(result, ""))
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No plugins matched the specified criteria")
	return nil
}

// outputNoApplicablePlugins outputs the "no applicable plugins" message.
func outputNoApplicablePlugins(cmd *cobra.Command, result *orchestrator.Result) error {
	if collectJSON {
		return outputCollectResultJSON(cmd, convertOrchestratorResult(result, ""))
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No applicable plugins found for this system")
	return nil
}

// runCollectionPhase runs the collection phase with a container.
func runCollectionPhase(ctx context.Context, cmd *cobra.Command, src macfor.Source, _ *orchestrator.Result) error {
	// Prepare output path.
	outputPath := collectOutput
	if !strings.HasSuffix(strings.ToLower(outputPath), ".zip") {
		outputPath += ".zip"
	}

	// Parse record format (already validated in runCollect).
	recordFormat, _ := macforparquet.ParseRecordFormat(collectRecordFormat)
	compression, _ := macforparquet.ParseCompression(collectParquetCompression)

	// Create evidence container.
	cont, err := container.NewContainer(container.Config{
		OutputPath:         outputPath,
		RecordFormat:       recordFormat,
		ParquetCompression: string(compression),
	})
	if err != nil {
		return fmt.Errorf("creating evidence container: %w", err)
	}
	defer func() { _ = cont.Close() }()

	// Create and run collection orchestrator.
	config := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   collectPlugins,
		Category:    collectCategory,
		ToolVersion: Version,
		DryRun:      false,
	}

	orch, err := orchestrator.New(config)
	if err != nil {
		return fmt.Errorf("creating orchestrator: %w", err)
	}

	orch.SetProgressCallback(makeProgressCallback())

	result, err := orch.Run(ctx)
	if result != nil && result.Cancelled {
		logger.Warn().Msg("Collection cancelled by user")
	}
	if err != nil {
		return err
	}

	// Log completion summaries.
	logPluginSummaries(result)

	// Output final results.
	return outputCollectionResults(cmd, result, outputPath)
}

// logPluginSummaries logs the summary for each collected plugin.
func logPluginSummaries(result *orchestrator.Result) {
	if quiet {
		return
	}
	for _, pr := range result.PluginResults {
		if pr.Collected {
			logger.Info().
				Str("plugin", pr.ID).
				Int("files", pr.FilesCollected).
				Int("records", pr.RecordsCreated).
				Int("errors", pr.Errors).
				Msg("Plugin collection completed")
		}
	}
}

// outputCollectionResults outputs the collection results. It returns
// ErrPartialCollection when collection recovered from errors.
func outputCollectionResults(cmd *cobra.Command, result *orchestrator.Result, outputPath string) error {
	collectResult := convertOrchestratorResult(result, outputPath)
	if collectJSON {
		if err := outputCollectResultJSON(cmd, collectResult); err != nil {
			return err
		}
	} else {
		outputCollectResultTable(cmd, collectResult)
	}

	if collectResult.TotalErrors > 0 {
		if !collectJSON && !quiet {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"\nCompleted with %d recovered error(s) (exit code %d). See manifest.json and logs/ in the evidence container.\n",
				collectResult.TotalErrors, ExitPartial)
		}
		return ErrPartialCollection
	}
	return nil
}

// convertOrchestratorResult converts an orchestrator.Result to a CollectResult.
func convertOrchestratorResult(result *orchestrator.Result, containerPath string) CollectResult {
	collectResult := CollectResult{
		Success:       result.Success,
		ContainerPath: containerPath,
		PluginsRun:    make([]PluginRunResult, 0, len(result.PluginResults)),
		TotalFiles:    result.TotalFiles,
		TotalRecords:  result.TotalRecords,
		TotalErrors:   result.TotalErrors,
		Duration:      result.Duration.Round(time.Millisecond).String(),
		DurationMs:    result.Duration.Milliseconds(),
		DetectionOnly: result.DetectionOnly,
	}

	for _, pr := range result.PluginResults {
		collectResult.PluginsRun = append(collectResult.PluginsRun, PluginRunResult{
			ID:             pr.ID,
			Name:           pr.Name,
			Applicable:     pr.Applicable,
			Reason:         pr.Reason,
			FilesCollected: pr.FilesCollected,
			RecordsCreated: pr.RecordsCreated,
			Errors:         pr.Errors,
			Skipped:        pr.Skipped,
		})
	}

	return collectResult
}

// outputDryRunResultsFromOrchestrator outputs dry run results from orchestrator result.
func outputDryRunResultsFromOrchestrator(cmd *cobra.Command, result *orchestrator.Result) error {
	if collectJSON {
		return outputCollectResultJSON(cmd, convertOrchestratorResult(result, ""))
	}

	if collectFrom != "" {
		absFrom, _ := filepath.Abs(collectFrom)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Source: directory (%s)\n", absFrom)
	}

	// Table output for dry run.
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Dry Run - Detection Results")
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "============================")
	_, _ = fmt.Fprintln(cmd.OutOrStdout())

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PLUGIN\tAPPLICABLE\tREASON")

	applicableCount := 0
	for _, pr := range result.PluginResults {
		applicable := "No"
		if pr.Applicable {
			applicable = "Yes"
			applicableCount++
		}

		reason := pr.Reason
		if reason == "" {
			reason = "-"
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", pr.ID, applicable, reason)
	}

	_ = w.Flush()

	_, _ = fmt.Fprintln(cmd.OutOrStdout())
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Summary: %d of %d plugins applicable\n", applicableCount, len(result.PluginResults))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Duration: %s\n", result.Duration.Round(time.Millisecond))

	return nil
}

// resolvePlugins determines which plugins to run based on flags.
// Kept for backward compatibility with tests.
func resolvePlugins(pluginIDs []string, category string) ([]macfor.Plugin, error) {
	// If specific plugins requested, look them up by ID.
	if len(pluginIDs) > 0 {
		var plugins []macfor.Plugin
		for _, id := range pluginIDs {
			p, ok := macfor.ByID(id)
			if !ok {
				return nil, fmt.Errorf("plugin not found: %s", id)
			}
			plugins = append(plugins, p)
		}
		return plugins, nil
	}

	// If category specified, filter by category.
	if category != "" {
		plugins := macfor.ByCategory(category)
		return plugins, nil
	}

	// Otherwise, return all plugins.
	return macfor.All(), nil
}

// runDetection runs detection on all plugins and returns results.
// Kept for backward compatibility with tests.
func runDetection(ctx context.Context, plugins []macfor.Plugin, src macfor.Source, _ *cobra.Command) map[string]macfor.DetectionResult {
	results := make(map[string]macfor.DetectionResult)

	for _, p := range plugins {
		info := p.Info()

		// Check for cancellation.
		if ctx.Err() != nil {
			break
		}

		if verbose {
			logger.Debug().Str("plugin", info.ID).Msg("Running detection")
		}

		result, err := p.Detect(ctx, src)
		if err != nil {
			logger.Warn().Str("plugin", info.ID).Err(err).Msg("Detection failed")
			result = macfor.DetectionResult{
				Applicable: false,
				Reason:     fmt.Sprintf("detection error: %v", err),
			}
		}

		results[info.ID] = result

		if verbose {
			logger.Debug().
				Str("plugin", info.ID).
				Bool("applicable", result.Applicable).
				Str("reason", result.Reason).
				Msg("Detection complete")
		}
	}

	return results
}

// filterApplicable returns only the plugins that are applicable.
// Kept for backward compatibility with tests.
func filterApplicable(plugins []macfor.Plugin, detectionResults map[string]macfor.DetectionResult) []macfor.Plugin {
	var applicable []macfor.Plugin
	for _, p := range plugins {
		if result, ok := detectionResults[p.Info().ID]; ok && result.Applicable {
			applicable = append(applicable, p)
		}
	}
	return applicable
}

// buildCollectResult builds a CollectResult from collection data.
// Kept for backward compatibility with tests.
func buildCollectResult(plugins []macfor.Plugin, detectionResults map[string]macfor.DetectionResult, pluginResults map[string]container.PluginSummary, containerPath string, startTime time.Time) CollectResult {
	result := CollectResult{
		Success:       true,
		ContainerPath: containerPath,
		PluginsRun:    make([]PluginRunResult, 0, len(plugins)),
		Duration:      time.Since(startTime).Round(time.Millisecond).String(),
		DurationMs:    time.Since(startTime).Milliseconds(),
	}

	for _, p := range plugins {
		info := p.Info()
		detection := detectionResults[info.ID]

		pluginResult := PluginRunResult{
			ID:         info.ID,
			Name:       info.Name,
			Applicable: detection.Applicable,
			Reason:     detection.Reason,
		}

		if summary, ok := pluginResults[info.ID]; ok {
			pluginResult.FilesCollected = summary.FilesCollected
			pluginResult.RecordsCreated = summary.RecordsCreated
			pluginResult.Errors = summary.ErrorsEncountered

			result.TotalFiles += summary.FilesCollected
			result.TotalRecords += summary.RecordsCreated
			result.TotalErrors += summary.ErrorsEncountered
		} else if detection.Applicable {
			// Plugin was applicable but didn't run (e.g., cancelled).
			pluginResult.Skipped = true
		}

		result.PluginsRun = append(result.PluginsRun, pluginResult)
	}

	// Mark as failed if there were errors.
	if result.TotalErrors > 0 {
		result.Success = false
	}

	return result
}

// outputCollectResultJSON outputs the collect result as JSON.
func outputCollectResultJSON(cmd *cobra.Command, result CollectResult) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// makeProgressCallback creates a progress callback for the orchestrator.
func makeProgressCallback() orchestrator.ProgressCallback {
	return func(p orchestrator.Progress) {
		if quiet {
			return
		}
		if p.Phase == orchestrator.PhaseDetection && verbose && p.PluginID != "" {
			logger.Debug().Str("plugin", p.PluginID).Msg("Running detection")
		}
		if p.Phase == orchestrator.PhaseCollection && p.PluginID != "" {
			logger.Info().Str("plugin", p.PluginID).Msg("Collecting artifacts")
		}
	}
}

// outputCollectResultTable outputs the collect result as a table.
func outputCollectResultTable(cmd *cobra.Command, result CollectResult) {
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Collection Complete")
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "===================")
	_, _ = fmt.Fprintln(cmd.OutOrStdout())

	// Output path.
	if result.ContainerPath != "" {
		absPath, _ := filepath.Abs(result.ContainerPath)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Evidence container: %s\n", absPath)
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	// Plugin results table.
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PLUGIN\tFILES\tRECORDS\tERRORS")

	for _, pr := range result.PluginsRun {
		if !pr.Applicable {
			continue
		}
		_, _ = fmt.Fprintf(w, "%s\t%d\t%d\t%d\n", pr.ID, pr.FilesCollected, pr.RecordsCreated, pr.Errors)
	}

	_ = w.Flush()

	// Summary.
	_, _ = fmt.Fprintln(cmd.OutOrStdout())
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Total: %d files, %d records, %d errors\n", result.TotalFiles, result.TotalRecords, result.TotalErrors)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Duration: %s\n", result.Duration)
}
