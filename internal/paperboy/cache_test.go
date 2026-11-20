package paperboy

import (
	"fmt"
	"testing"
	"time"
)

// TestHintsAreBoundedLRU keeps the notice cache small and drops the page used
// longest ago (ADR 0138).
func TestHintsAreBoundedLRU(t *testing.T) {
	s, _ := testServer(t)
	now := time.Now()
	s.mu.Lock()
	for i := range maxHints + 10 {
		key := fmt.Sprintf("%03d", i)
		s.hints[key] = hintPage{data: []byte("png"), at: now, used: now.Add(time.Duration(i) * time.Second)}
	}
	s.pruneHintsLocked()
	s.mu.Unlock()
	if len(s.hints) != maxHints {
		t.Fatalf("hints after the prune: %d, want %d", len(s.hints), maxHints)
	}
	if _, ok := s.hints["000"]; ok {
		t.Error("the least recently used hint survived")
	}
	if _, ok := s.hints[fmt.Sprintf("%03d", maxHints+9)]; !ok {
		t.Error("the most recently used hint was dropped")
	}
}

// TestRenderedCacheIsBounded drops stale renders and the oldest above the cap
// (ADR 0138); the hash index follows.
func TestRenderedCacheIsBounded(t *testing.T) {
	s, _ := testServer(t)
	now := time.Now()
	s.mu.Lock()
	for i := range maxRendered + 10 {
		key := fmt.Sprintf("small/%03d", i)
		rp := &renderedPage{data: []byte{byte(i)}, hash: fmt.Sprintf("%03d", i),
			at: now.Add(time.Duration(i) * time.Second)}
		s.rendered[key] = rp
		s.renderedHash[rp.hash] = rp
	}
	stale := &renderedPage{data: []byte("old"), hash: "stale", at: now.Add(-renderedTTL - time.Hour)}
	s.rendered["small/stale"] = stale
	s.renderedHash[stale.hash] = stale
	s.pruneRenderedLocked(now)
	rendered, hashes := len(s.rendered), len(s.renderedHash)
	_, staleLeft := s.renderedHash["stale"]
	s.mu.Unlock()
	if rendered != maxRendered || hashes != maxRendered {
		t.Fatalf("rendered %d, hashes %d, want %d", rendered, hashes, maxRendered)
	}
	if staleLeft {
		t.Error("a render older than the TTL survived")
	}
}

// TestPageKeyNotesAreBounded caps the map that suppresses repeated refusal
// logs (ADR 0138).
func TestPageKeyNotesAreBounded(t *testing.T) {
	s, _ := testServer(t)
	for i := range maxPageKeyNotes + 20 {
		s.notePageKey(fmt.Sprintf("/pages/small/%d.png", i), "no valid device key")
	}
	s.mu.Lock()
	notes := len(s.pageKeyNotes)
	s.mu.Unlock()
	if notes > maxPageKeyNotes {
		t.Fatalf("page key notes: %d, cap %d", notes, maxPageKeyNotes)
	}
}
