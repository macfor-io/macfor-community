package atime

import (
	"sync"
	"time"
)

// Phase is the collection phase during which a read was made.
type Phase string

const (
	// PhaseDetect marks reads made by a plugin's Detect.
	PhaseDetect Phase = "detect"

	// PhaseCollect marks reads made by a plugin's Collect.
	PhaseCollect Phase = "collect"
)

// Operation is the kind of read that was observed.
type Operation string

const (
	// OperationRead is a file read through Source.Open or Source.ReadPlist.
	OperationRead Operation = "read"

	// OperationReadDir is a directory listing through Source.ReadDir,
	// Source.Glob or Source.Walk.
	OperationReadDir Operation = "readdir"
)

// Event records one observed read that changed an access time.
type Event struct {
	// ObservedAt is when the after-stat was taken.
	ObservedAt time.Time `json:"observed_at"`

	// PluginID is the plugin that made the read.
	PluginID string `json:"plugin_id,omitempty"`

	// Phase is the collection phase the read was made in.
	Phase Phase `json:"phase,omitempty"`

	// Operation is the kind of read.
	Operation Operation `json:"operation"`

	// Path is the path the plugin asked the source for.
	Path string `json:"path"`

	// ResolvedPath is the path the source resolved it to (for example with
	// ~ expanded), when it differs from Path.
	ResolvedPath string `json:"resolved_path,omitempty"`

	// Attribution states what most likely changed the access time.
	Attribution Attribution `json:"attribution"`

	// Before is the stat taken before the first read.
	Before Stat `json:"before"`

	// After is the stat taken after the last read.
	After Stat `json:"after"`
}

// InodeChange summarises the access-time changes observed on one inode over
// the whole collection.
type InodeChange struct {
	// Dev and Ino identify the inode.
	Dev uint64 `json:"dev"`
	Ino uint64 `json:"ino"`

	// Path is the path of the first read that changed the inode's access time.
	Path string `json:"path"`

	// Changes is the number of observed reads that changed the access time.
	Changes int `json:"changes"`

	// FirstBefore is the before-stat of the first read that changed the
	// access time. Its atime is the access time the inode had before the
	// collector changed it.
	FirstBefore Stat `json:"first_before"`

	// LastAfter is the after-stat of the last read that changed the access
	// time.
	LastAfter Stat `json:"last_after"`
}

// Summary totals the observed reads of a collection.
type Summary struct {
	// ObservedReads counts file reads and directory listings that were
	// stat-ed before and after.
	ObservedReads int `json:"observed_reads"`

	// Modified counts observed reads after which the access time differed.
	Modified int `json:"modified"`

	// LikelyCollector counts modified reads attributed to the collector.
	LikelyCollector int `json:"likely_collector"`

	// ConcurrentActivity counts modified reads during which another process
	// was also active on the file.
	ConcurrentActivity int `json:"concurrent_activity"`

	// Unclosed counts files opened through the source but never closed by
	// the time the summary was taken; they were not compared.
	Unclosed int `json:"unclosed,omitempty"`

	// Inodes lists each inode whose access time changed, in first-change
	// order.
	Inodes []InodeChange `json:"inodes"`

	// Unobserved lists the kinds of reads the observer cannot see.
	Unobserved []string `json:"unobserved"`
}

// Unobserved lists the kinds of reads that do not pass through an observed
// source and so are never compared. It is recorded in every Summary so the
// gaps are stated in the evidence rather than left implicit.
var Unobserved = []string{
	"reads made by external commands the collector runs (for example sw_vers, system_profiler, dscl, csrutil, fdesetup and log)",
	"reads a source makes internally for system information and user-account enumeration",
	"reads a plugin makes directly on host paths instead of through the source",
}

// inodeKey identifies an inode.
type inodeKey struct{ dev, ino uint64 }

// Tracker accumulates the reads observed through its sources. It is safe for
// concurrent use. Events are buffered until a sink is set, so reads made
// before the evidence container's chain of custody exists (for example during
// detection) are delivered once it does.
type Tracker struct {
	mu  sync.Mutex
	now func() time.Time

	observed, modified, likely, concurrent, open int

	inodes map[inodeKey]*InodeChange
	order  []inodeKey

	pending []Event
	sink    func(Event)
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{now: time.Now, inodes: make(map[inodeKey]*InodeChange)}
}

// SetSink delivers every buffered event to fn, in observation order, and
// then every new event as it is observed. A nil fn buffers events again.
func (t *Tracker) SetSink(fn func(Event)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sink = fn
	if fn == nil {
		return
	}
	for _, e := range t.pending {
		fn(e)
	}
	t.pending = nil
}

// Summary returns the totals so far.
func (t *Tracker) Summary() Summary {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Summary{
		ObservedReads:      t.observed,
		Modified:           t.modified,
		LikelyCollector:    t.likely,
		ConcurrentActivity: t.concurrent,
		Unclosed:           t.open,
		Inodes:             make([]InodeChange, 0, len(t.order)),
		Unobserved:         append([]string(nil), Unobserved...),
	}
	for _, k := range t.order {
		s.Inodes = append(s.Inodes, *t.inodes[k])
	}
	return s
}

// opened counts a file handle the observer is waiting to compare.
func (t *Tracker) opened() {
	t.mu.Lock()
	t.open++
	t.mu.Unlock()
}

// closed counts a compared file handle as no longer open.
func (t *Tracker) closed() {
	t.mu.Lock()
	t.open--
	t.mu.Unlock()
}

// record counts one observed read and, when its access time changed,
// records e. The caller fills every field but ObservedAt and Attribution.
func (t *Tracker) record(e Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.observed++
	if !sameInode(e.Before, e.After) {
		return
	}
	changed, attribution := compare(e.Before, e.After)
	if !changed {
		return
	}
	e.ObservedAt = t.now()
	e.Attribution = attribution

	t.modified++
	if attribution == AttributionLikelyCollector {
		t.likely++
	} else {
		t.concurrent++
	}

	k := inodeKey{e.Before.Dev, e.Before.Ino}
	ic, ok := t.inodes[k]
	if !ok {
		ic = &InodeChange{Dev: k.dev, Ino: k.ino, Path: e.Path, FirstBefore: e.Before}
		t.inodes[k] = ic
		t.order = append(t.order, k)
	}
	ic.Changes++
	ic.LastAfter = e.After

	if t.sink != nil {
		t.sink(e)
		return
	}
	t.pending = append(t.pending, e)
}
