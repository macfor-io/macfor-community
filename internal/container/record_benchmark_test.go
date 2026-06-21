package container

import (
	"fmt"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

type benchRecord struct {
	URL       string    `parquet:"url" json:"url"`
	Title     string    `parquet:"title" json:"title"`
	Timestamp time.Time `parquet:"timestamp" json:"timestamp"`
	VisitType int64     `parquet:"visit_type" json:"visit_type"`
	User      string    `parquet:"user" json:"user"`
}

func BenchmarkJSONLWriter(b *testing.B) {
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := newJSONLRecordWriter("browser.safari")
		rec := benchRecord{
			URL:       fmt.Sprintf("http://example.com/%d", i),
			Title:     "Test Page",
			Timestamp: ts,
			VisitType: 1,
			User:      "admin",
		}
		_ = w.WriteRecord(*artifact, rec)
	}
}

func BenchmarkParquetWriter(b *testing.B) {
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := newParquetRecordWriter("browser.safari", "snappy")
		rec := benchRecord{
			URL:       fmt.Sprintf("http://example.com/%d", i),
			Title:     "Test Page",
			Timestamp: ts,
			VisitType: 1,
			User:      "admin",
		}
		_ = w.WriteRecord(*artifact, rec)
	}
}

func BenchmarkParquetWriter_Batch(b *testing.B) {
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("records=%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				w := newParquetRecordWriter("browser.safari", "snappy")
				for j := 0; j < size; j++ {
					rec := benchRecord{
						URL:       fmt.Sprintf("http://example.com/%d", j),
						Title:     "Test Page",
						Timestamp: ts,
						VisitType: 1,
						User:      "admin",
					}
					_ = w.WriteRecord(*artifact, rec)
				}
			}
		})
	}
}

func BenchmarkParquetWriter_Finalize(b *testing.B) {
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("records=%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				tmpDir := b.TempDir()
				c, _ := NewContainer(Config{OutputPath: tmpDir + "/test.zip"})
				w := newParquetRecordWriter("browser.safari", "snappy")
				for j := 0; j < size; j++ {
					rec := benchRecord{
						URL:       fmt.Sprintf("http://example.com/%d", j),
						Title:     "Test Page",
						Timestamp: ts,
						VisitType: 1,
						User:      "admin",
					}
					_ = w.WriteRecord(*artifact, rec)
				}
				_, _ = w.Finalize(c)
				_ = c.Close()
			}
		})
	}
}
