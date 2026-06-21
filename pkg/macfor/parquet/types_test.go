package parquet_test

import (
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor/parquet"
)

func TestParseRecordFormat(t *testing.T) {
	tests := []struct {
		input   string
		want    parquet.RecordFormat
		wantErr bool
	}{
		{"jsonl", parquet.FormatJSONL, false},
		{"parquet", parquet.FormatParquet, false},
		{"both", parquet.FormatBoth, false},
		{"JSONL", parquet.FormatJSONL, false},
		{"Parquet", parquet.FormatParquet, false},
		{"BOTH", parquet.FormatBoth, false},
		{" jsonl ", parquet.FormatJSONL, false},
		{"csv", "", true},
		{"", "", true},
		{"json", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parquet.ParseRecordFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRecordFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseRecordFormat(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseCompression(t *testing.T) {
	tests := []struct {
		input   string
		want    parquet.Compression
		wantErr bool
	}{
		{"snappy", parquet.CompressionSnappy, false},
		{"zstd", parquet.CompressionZstd, false},
		{"gzip", parquet.CompressionGzip, false},
		{"none", parquet.CompressionNone, false},
		{"uncompressed", parquet.CompressionNone, false},
		{"SNAPPY", parquet.CompressionSnappy, false},
		{"Zstd", parquet.CompressionZstd, false},
		{" gzip ", parquet.CompressionGzip, false},
		{"lz4", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parquet.ParseCompression(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCompression(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseCompression(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRecordFormat_String(t *testing.T) {
	if parquet.FormatJSONL.String() != "jsonl" {
		t.Errorf("FormatJSONL.String() = %q, want %q", parquet.FormatJSONL.String(), "jsonl")
	}
	if parquet.FormatParquet.String() != "parquet" {
		t.Errorf("FormatParquet.String() = %q, want %q", parquet.FormatParquet.String(), "parquet")
	}
}

func TestCompression_String(t *testing.T) {
	if parquet.CompressionSnappy.String() != "snappy" {
		t.Errorf("CompressionSnappy.String() = %q, want %q", parquet.CompressionSnappy.String(), "snappy")
	}
}
