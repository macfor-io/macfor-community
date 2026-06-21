package asl

import (
	"errors"
	"fmt"
	"io"
)

// Reader reads ASL binary files.
type Reader struct {
	r      io.ReadSeeker
	header *fileHeader
	curOff uint64 // Current record offset (0 = not started).
	done   bool
}

// NewReader creates a reader for an ASL binary file.
// It validates the magic bytes and parses the file header.
func NewReader(r io.ReadSeeker) (*Reader, error) {
	h, err := parseHeader(r)
	if err != nil {
		return nil, fmt.Errorf("parsing ASL header: %w", err)
	}

	return &Reader{
		r:      r,
		header: h,
		curOff: 0,
	}, nil
}

// Next returns the next record from the ASL file.
// Returns io.EOF when all records have been read.
func (rd *Reader) Next() (*Record, error) {
	if rd.done {
		return nil, io.EOF
	}

	// Determine which offset to read from.
	offset := rd.curOff
	if offset == 0 {
		offset = rd.header.FirstRecordOff
	}

	if offset == 0 {
		rd.done = true
		return nil, io.EOF
	}

	rec, nextOff, err := parseRecord(rd.r, offset)
	if err != nil {
		rd.done = true
		return nil, fmt.Errorf("parsing record at offset %d: %w", offset, err)
	}

	if nextOff == 0 {
		rd.done = true
	} else {
		rd.curOff = nextOff
	}

	return rec, nil
}

// ReadAll reads all records from the ASL file.
func (rd *Reader) ReadAll() ([]Record, error) {
	var records []Record
	for {
		rec, err := rd.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return records, err
		}
		records = append(records, *rec)
	}
	return records, nil
}
