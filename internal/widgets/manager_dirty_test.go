package widgets

import (
	"fmt"
	"sort"
	"testing"
)

func TestNotifyBurstIsNotDropped(t *testing.T) {
	m := &Manager{updates: make(chan struct{}, 1), dirty: map[string]struct{}{}}
	for i := 0; i < 100; i++ {
		m.notify(fmt.Sprintf("w%d", i%50))
	}
	select {
	case <-m.Updates():
	default:
		t.Fatal("expected an update signal")
	}
	ids := m.TakeDirty()
	sort.Strings(ids)
	if len(ids) != 50 {
		t.Fatalf("expected 50 distinct dirty ids, got %d", len(ids))
	}
	if again := m.TakeDirty(); again != nil {
		t.Fatalf("expected dirty set to be cleared, got %v", again)
	}
}
