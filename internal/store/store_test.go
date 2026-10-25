package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DJJW26/hookline/internal/delivery"
)

// mkDelivery builds a uniquely identifiable delivery for ordering checks.
func mkDelivery(i int) delivery.Delivery {
	return delivery.Delivery{
		ID:     fmt.Sprintf("id-%03d", i),
		TS:     time.Date(2026, 1, 1, 12, 0, i, 0, time.UTC),
		Method: "POST",
		Path:   fmt.Sprintf("/hook/%d", i),
		Body:   []byte(fmt.Sprintf("body-%d", i)),
	}
}

// mustOpen opens a store in dir or fails the test.
func mustOpen(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q) error: %v", dir, err)
	}
	return s
}

// ids extracts the delivery IDs in order.
func ids(ds []delivery.Delivery) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.ID
	}
	return out
}

func TestOpenCreatesMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep")
	s := mustOpen(t, dir)
	defer s.Close()
	if _, err := os.Stat(filepath.Join(dir, fileName)); err != nil {
		t.Fatalf("JSONL file not created: %v", err)
	}
}

func TestLoadLast(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	defer s.Close()
	for i := 1; i <= 3; i++ {
		if err := s.Append(mkDelivery(i)); err != nil {
			t.Fatalf("Append(%d) error: %v", i, err)
		}
	}
	tests := []struct {
		n    int
		want []string
	}{
		{n: 0, want: []string{}},
		{n: -1, want: []string{}},
		{n: 2, want: []string{"id-002", "id-003"}},
		{n: 3, want: []string{"id-001", "id-002", "id-003"}},
		{n: 100, want: []string{"id-001", "id-002", "id-003"}},
	}
	for _, tc := range tests {
		got, err := s.LoadLast(tc.n)
		if err != nil {
			t.Fatalf("LoadLast(%d) error: %v", tc.n, err)
		}
		want := tc.want
		if len(want) == 0 {
			want = []string{}
		}
		if gotIDs := ids(got); !equalStrings(gotIDs, want) {
			t.Errorf("LoadLast(%d) IDs = %v, want %v", tc.n, gotIDs, want)
		}
	}
}

func TestAppendTruncatesLargeBody(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close()
	big := bytes.Repeat([]byte{0xAB}, 3<<20) // 3 MiB, above MaxBodyBytes
	d := mkDelivery(1)
	d.Body = big
	if err := s.Append(d); err != nil {
		t.Fatalf("Append error: %v", err)
	}
	if len(d.Body) != 3<<20 {
		t.Errorf("Append modified the caller's delivery body: len=%d", len(d.Body))
	}
	got, err := s.LoadLast(1)
	if err != nil {
		t.Fatalf("LoadLast error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadLast returned %d deliveries, want 1", len(got))
	}
	if len(got[0].Body) != MaxBodyBytes {
		t.Errorf("stored body len = %d, want %d", len(got[0].Body), MaxBodyBytes)
	}
	if !got[0].Truncated {
		t.Error("stored delivery Truncated = false, want true")
	}
	if !bytes.Equal(got[0].Body, big[:MaxBodyBytes]) {
		t.Error("stored body is not the truncated prefix of the original")
	}
}

// appendRaw appends a raw line to the store file, bypassing the store, to
// simulate corruption.
func appendRaw(t *testing.T, dir, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, fileName), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open raw file: %v", err)
	}
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("write raw line: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close raw file: %v", err)
	}
}

func TestLoadLastSkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	if err := s.Append(mkDelivery(1)); err != nil {
		t.Fatalf("Append(1) error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	// Interior corruption: stray text and invalid JSON between records.
	appendRaw(t, dir, "not json\n")
	appendRaw(t, dir, "{bad json}\n")

	s = mustOpen(t, dir)
	if err := s.Append(mkDelivery(2)); err != nil {
		t.Fatalf("Append(2) error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	// Trailing corruption: crash right after a partial write.
	appendRaw(t, dir, "{\"id\":\"partial\"")

	s = mustOpen(t, dir)
	defer s.Close()
	got, err := s.LoadLast(10)
	if err != nil {
		t.Fatalf("LoadLast error: %v", err)
	}
	if gotIDs := ids(got); !equalStrings(gotIDs, []string{"id-001", "id-002"}) {
		t.Errorf("LoadLast IDs = %v, want [id-001 id-002]", gotIDs)
	}
}

func TestClear(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close()
	for i := 1; i <= 2; i++ {
		if err := s.Append(mkDelivery(i)); err != nil {
			t.Fatalf("Append(%d) error: %v", i, err)
		}
	}
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear error: %v", err)
	}
	got, err := s.LoadLast(5)
	if err != nil {
		t.Fatalf("LoadLast after Clear error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadLast after Clear returned %d deliveries, want 0", len(got))
	}
	// The store stays usable after Clear.
	if err := s.Append(mkDelivery(9)); err != nil {
		t.Fatalf("Append after Clear error: %v", err)
	}
	got, err = s.LoadLast(5)
	if err != nil {
		t.Fatalf("LoadLast error: %v", err)
	}
	if gotIDs := ids(got); !equalStrings(gotIDs, []string{"id-009"}) {
		t.Errorf("LoadLast after re-append IDs = %v, want [id-009]", gotIDs)
	}
}

// equalStrings compares two string slices.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
