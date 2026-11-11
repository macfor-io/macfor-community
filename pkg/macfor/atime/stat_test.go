package atime

import (
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := Stat{Dev: 1, Ino: 2, Size: 10, Atime: t0, Mtime: t0, Ctime: t0}
	later := t0.Add(time.Hour)

	with := func(f func(*Stat)) Stat { s := base; f(&s); return s }

	tests := []struct {
		name        string
		after       Stat
		wantChanged bool
		want        Attribution
	}{
		{"unchanged", base, false, ""},
		{"atime forward only", with(func(s *Stat) { s.Atime = later }), true, AttributionLikelyCollector},
		{"atime and mtime", with(func(s *Stat) { s.Atime = later; s.Mtime = later }), true, AttributionConcurrentActivity},
		{"atime and ctime", with(func(s *Stat) { s.Atime = later; s.Ctime = later }), true, AttributionConcurrentActivity},
		{"atime and size", with(func(s *Stat) { s.Atime = later; s.Size = 11 }), true, AttributionConcurrentActivity},
		{"atime backwards", with(func(s *Stat) { s.Atime = t0.Add(-time.Hour) }), true, AttributionConcurrentActivity},
		{"mtime only", with(func(s *Stat) { s.Mtime = later }), false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed, got := compare(base, tt.after)
			if changed != tt.wantChanged || got != tt.want {
				t.Errorf("compare = %v, %q; want %v, %q", changed, got, tt.wantChanged, tt.want)
			}
		})
	}
}

func TestRecordIgnoresDifferentInodes(t *testing.T) {
	tr := NewTracker()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr.record(Event{
		Path:   "/x",
		Before: Stat{Dev: 1, Ino: 2, Atime: t0},
		After:  Stat{Dev: 1, Ino: 3, Atime: t0.Add(time.Hour)},
	})
	if s := tr.Summary(); s.ObservedReads != 1 || s.Modified != 0 {
		t.Errorf("summary = %+v, want the replaced inode not compared", s)
	}
}

func TestSummaryKeepsFirstBeforePerInode(t *testing.T) {
	tr := NewTracker()
	t0 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t1, t2 := t0.Add(time.Hour), t0.Add(2*time.Hour)
	tr.record(Event{Path: "/a", Before: Stat{Dev: 1, Ino: 9, Atime: t0}, After: Stat{Dev: 1, Ino: 9, Atime: t1}})
	tr.record(Event{Path: "/a-again", Before: Stat{Dev: 1, Ino: 9, Atime: t1}, After: Stat{Dev: 1, Ino: 9, Atime: t2}})
	s := tr.Summary()
	if len(s.Inodes) != 1 {
		t.Fatalf("inodes = %+v", s.Inodes)
	}
	ic := s.Inodes[0]
	if !ic.FirstBefore.Atime.Equal(t0) || !ic.LastAfter.Atime.Equal(t2) || ic.Changes != 2 || ic.Path != "/a" {
		t.Errorf("inode change = %+v", ic)
	}
	if s.Modified != 2 {
		t.Errorf("Modified = %d, want 2", s.Modified)
	}
}
