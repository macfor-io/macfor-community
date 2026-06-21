// Package container implements the evidence container writer that creates
// ZIP-based forensic containers with integrity verification.
package container

import "github.com/macfor-io/macfor-community/pkg/macfor"

// RecordWriter serialises structured records to a specific format.
// Implementations buffer records in memory and flush to the container
// on Finalize().
type RecordWriter interface {
	// WriteRecord buffers a single record for the given artifact type.
	// The concrete type of record determines the schema.
	WriteRecord(artifact macfor.Artifact, record any) error

	// Finalize flushes all buffered records to the container and returns
	// the total number of records written.
	Finalize(container *Container) (int, error)
}
