package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DJJW26/hookline/internal/delivery"
)

// Edge-case probes: corrupt interior lines, append-after-clear, LoadLast
// bounds, and concurrent Append racing Clear.

func TestClearRecoversFromFailedAppend(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Append(delivery.Delivery{ID: "old"}); err != nil {
		t.Fatal(err)
	}
	readOnly, err := os.Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	s.w.Reset(readOnly)
	if err := s.Append(delivery.Delivery{ID: "failed"}); err == nil {
		t.Fatal("expected an error writing to the read-only handle")
	}
	if err := s.Clear(); err != nil {
		t.Fatalf("clear should discard the failed write: %v", err)
	}
	if err := s.Append(delivery.Delivery{ID: "new"}); err != nil {
		t.Fatalf("append after clear should work: %v", err)
	}
	got, err := s.LoadLast(2)
	if err != nil || len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("history after clear and new append = %v, error %v", got, err)
	}
}

func TestClearRefusesToTruncateDifferentFile(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Append(delivery.Delivery{ID: "original"}); err != nil {
		t.Fatal(err)
	}
	// Point the path at another file while the append handle remains open.
	// This exercises a replaced path on Windows too, where the OS can deny
	// renaming the open history file itself.
	s.path = filepath.Join(dir, "replacement.jsonl")
	if err := os.WriteFile(s.path, []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err == nil || !strings.Contains(err.Error(), "history file was replaced") {
		t.Fatalf("clear with different on-disk file = %v, want file replacement error", err)
	}
	replacement, err := os.ReadFile(s.path)
	if err != nil || string(replacement) != "replacement" {
		t.Fatalf("replacement should be unchanged, got %q, error %v", replacement, err)
	}
	got, err := s.LoadLast(1)
	if err != nil || len(got) != 1 || got[0].ID != "original" {
		t.Fatalf("original history = %v, error %v", got, err)
	}
}

func TestLoadLastSkipsCorruptInteriorLines(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 3; i++ {
		if err := s.Append(delivery.Delivery{ID: strings.Repeat("a", i+1), ResponseStatus: 200}); err != nil {
			t.Fatal(err)
		}
	}
	// Corrupt the middle record by rewriting the file with garbage
	// injected between valid lines.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, fileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitN(string(raw), "\n", 3)
	lines[1] = "{corrupt middle"
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.LoadLast(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadLast = %d records, want 2 (corrupt middle skipped): %+v", len(got), got)
	}
	if got[0].ID != "a" || got[1].ID != "aaa" {
		t.Errorf("survivors = %q, %q", got[0].ID, got[1].ID)
	}
}

func TestAppendImmediatelyAfterClear(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Append(delivery.Delivery{ID: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(delivery.Delivery{ID: "two"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadLast(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "two" {
		t.Fatalf("after clear+append: %+v", got)
	}
}

func TestLoadLastBounds(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"1", "2", "3"} {
		if err := s.Append(delivery.Delivery{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.LoadLast(0); len(got) != 0 {
		t.Errorf("LoadLast(0) = %d records", len(got))
	}
	if got, _ := s.LoadLast(-5); len(got) != 0 {
		t.Errorf("LoadLast(-5) = %d records", len(got))
	}
	got, _ := s.LoadLast(99)
	if len(got) != 3 || got[0].ID != "1" || got[2].ID != "3" {
		t.Errorf("LoadLast(99) = %+v", got)
	}
}

func TestConcurrentAppendDuringClear(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	// Writer goroutine keeps appending.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = s.Append(delivery.Delivery{ID: "c"})
		}
	}()
	for i := 0; i < 5; i++ {
		if err := s.Clear(); err != nil {
			t.Errorf("Clear under concurrent appends: %v", err)
		}
	}
	close(stop)
	wg.Wait()
	// Store must still be usable afterwards.
	if err := s.Append(delivery.Delivery{ID: "final"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadLast(1)
	if err != nil || len(got) != 1 || got[0].ID != "final" {
		t.Fatalf("post-race store unusable: %v %+v", err, got)
	}
}

func TestAppendClosesGracefully(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(delivery.Delivery{ID: "x"}); err == nil {
		t.Error("Append after Close should error")
	}
	if err := s.Close(); err != nil {
		t.Errorf("double Close should be a no-op: %v", err)
	}
}
