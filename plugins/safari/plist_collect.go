package safari

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/plistmapper"
)

// plistCollectSpec describes a plist artifact whose records are produced by the
// declarative plist mapper (its manifest entry carries an `extract:` block with
// a `records:` key). The collector handles the common file acquisition; the
// mapper handles field mapping. Cross-field logic is supplied via derivers.
type plistCollectSpec struct {
	artifactType ArtifactType
	category     string
	artifactKind string // macfor artifact Type, e.g. "extensions".
	description  string
	newRecord    func() any
	derivers     map[string]plistmapper.Deriver
}

// collectPlistForUser resolves a plist artifact's path from the manifest, stores
// the raw file, and extracts records via the declarative plist mapper.
func collectPlistForUser(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector, spec plistCollectSpec) error {
	path, err := locator.Path(src, user, string(spec.artifactType))
	if err != nil {
		return err
	}
	if path == "" {
		out.Log(macfor.LogDebug, "%s not found for user %s", spec.artifactKind, user.Username)
		return nil
	}

	fileInfo, err := src.Stat(path)
	if err != nil {
		if errors.Is(err, macfor.ErrNotFound) || errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			out.Log(macfor.LogDebug, "%s not found for user %s", spec.artifactKind, user.Username)
			return nil
		}
		return fmt.Errorf("checking %s: %w", spec.artifactKind, err)
	}

	artifact, err := macfor.NewArtifact(PluginID, spec.category, spec.artifactKind)
	if err != nil {
		return fmt.Errorf("creating artifact: %w", err)
	}
	artifact.SourcePath = path
	artifact.Username = user.Username
	artifact.Description = spec.description
	artifact.OriginalModTime = fileInfo.ModTime()
	artifact.OriginalSize = fileInfo.Size()

	rawFile, err := src.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", spec.artifactKind, err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rawFile); err != nil {
		_ = rawFile.Close()
		return fmt.Errorf("reading %s: %w", spec.artifactKind, err)
	}
	_ = rawFile.Close()
	data := buf.Bytes()

	if err := out.AddFile(ctx, *artifact, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("adding %s file: %w", spec.artifactKind, err)
	}

	def := artifacts.GetArtifactByID(manifest, string(spec.artifactType))
	if def == nil || def.Extract == nil {
		return fmt.Errorf("%s artifact missing extract spec in manifest", spec.artifactKind)
	}

	count, err := plistmapper.Each(ctx, data, *def.Extract, plistmapper.Options{
		NewRecord: spec.newRecord,
		Context: map[string]string{
			ctxUsername:   user.Username,
			ctxSourceFile: path,
		},
		Derivers: spec.derivers,
		Warnf:    func(format string, args ...any) { out.Log(macfor.LogWarn, format, args...) },
	}, func(rec any) error {
		return out.AddRecord(ctx, *artifact, rec)
	})
	if err != nil {
		out.Log(macfor.LogWarn, "Error parsing %s for user %s: %v", spec.artifactKind, user.Username, err)
		return fmt.Errorf("parsing %s: %w", spec.artifactKind, err)
	}

	if count == 0 {
		out.Log(macfor.LogInfo, "No %s records found for user %s", spec.artifactKind, user.Username)
	} else {
		out.Log(macfor.LogInfo, "Collected %d %s records for user %s", count, spec.artifactKind, user.Username)
	}
	return nil
}
