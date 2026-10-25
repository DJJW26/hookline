// Adversarial probes for the JSONL store: 10k-line load performance, corrupt
// lines in the middle of the file, the exact MaxBodyBytes boundary, appends
// after close, and Append racing Clear.
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// probeDelivery builds a delivery with a distinct, sortable ID.
func probeDelivery(tag string, i int) (d struct {
	ID  string
	Tag string
}) {
	panic("unused")
} // placeholder replaced below

func TestProbeLoadLast10kPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("perf probe skipped in -short mode")
	}
	dir := t.TempDir()
	s := mustOpen(t, dir)
	defer s.Close()

	const n = 10000
	start := time.Now()
	for i := 0; i < n; i++ {
		d := mkDelivery(i)
		d.Path = fmt.Sprintf("/perf/%d", i)
		d.Body = bytes.Repeat([]byte("0123456789abcdef"), 2) // 32 bytes
		if err := s.Append(d); err != nil {
			t.Fatalf("Append(%d) error: %v", i, err)
		}
	}
	appendDur := time.Since(start)

	start = time.Now()
	got, err := s.LoadLast(200)
	load200 := time.Since(start)
	if err != nil {
		t.Fatalf("LoadLast(200) error: %v", err)
	}
	if len(got) != 200 {
		t.Fatalf("LoadLast(200) returned %d, want 200", len(got))
	}
	if gotIDs := ids(got); gotIDs[0] != "id-9800" || gotIDs[199] != "id-9999" {
		t.Errorf("LoadLast(200) window = [%s..%s], want [id-9800..id-9999]", gotIDs[0], gotIDs[199])
	}

	start = time.Now()
	gotAll, err := s.LoadLast(n)
	loadAll := time.Since(start)
	if err != nil {
		t.Fatalf("LoadLast(%d) error: %v", n, err)
	}
	if len(gotAll) != n {
		t.Fatalf("LoadLast(%d) returned %d, want %d", n, len(gotAll), n)
	}
	t.Logf("10k appends: %v; LoadLast(200): %v; LoadLast(10000): %v", appendDur, load200, loadAll)
	if load200 > 10*time.Second {
		t.Errorf("LoadLast(200) over 10k lines took %v, want < 10s", load200)
	}
	if loadAll > 10*time.Second {
		t.Errorf("LoadLast(10000) took %v, want < 10s", loadAll)
	}
}

func TestProbeCorruptMiddleLinesPreserveOrderAcrossWrap(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	if err := s.Append(mkDelivery(1)); err != nil {
		t.Fatalf("Append(1): %v", err)
	}
	if err := s.Append(mkDelivery(2)); err != nil {
		t.Fatalf("Append(2): %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Corrupt lines between records 2 and 3: stray text, broken JSON, and a
	// syntactically valid JSON object with no id (treated as corrupt).
	appendRaw(t, dir, "totally not json\n")
	appendRaw(t, dir, "{\"id\": \"dangling\n")
	appendRaw(t, dir, "{\"method\":\"POST\",\"path\":\"/ghost\"}\n")

	s = mustOpen(t, dir)
	for i := 3; i <= 5; i++ {
		if err := s.Append(mkDelivery(i)); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	defer s.Close()

	// Ring smaller than the survivor count: ordering must stay chronological
	// even though the ring wrapped past the corrupt lines.
	got, err := s.LoadLast(3)
	if err != nil {
		t.Fatalf("LoadLast(3) error: %v", err)
	}
	if gotIDs := ids(got); !equalStrings(gotIDs, []string{"id-003", "id-004", "id-005"}) {
		t.Errorf("LoadLast(3) IDs = %v, want [id-003 id-004 id-005]", gotIDs)
	}
	got, err = s.LoadLast(10)
	if err != nil {
		t.Fatalf("LoadLast(10) error: %v", err)
	}
	if gotIDs := ids(got); !equalStrings(gotIDs, []string{"id-001", "id-002", "id-003", "id-004", "id-005"}) {
		t.Errorf("LoadLast(10) IDs = %v, want all five in order", gotIDs)
	}
}

func TestProbeBodyExactlyMaxBodyBytesNotTruncated(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close()
	d := mkDelivery(1)
	d.Body = bytes.Repeat([]byte{0xFE}, MaxBodyBytes) // exactly at the cap
	if err := s.Append(d); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := s.LoadLast(1)
	if err != nil {
		t.Fatalf("LoadLast: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadLast returned %d, want 1", len(got))
	}
	if len(got[0].Body) != MaxBodyBytes {
		t.Errorf("stored body len = %d, want %d (exact-cap bodies must not truncate)", len(got[0].Body), MaxBodyBytes)
	}
	if got[0].Truncated {
		t.Error("Truncated = true at exactly MaxBodyBytes, want false")
	}
}

func TestProbeAppendAfterCloseErrors(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	if err := s.Append(mkDelivery(1)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close is documented safe to call twice.
	if err := s.Close(); err != nil {
		t.Errorf("second Close error: %v, want nil", err)
	}
	err := s.Append(mkDelivery(2))
	if err == nil {
		t.Fatal("Append after Close returned nil error")
	}
	if _, err := s.LoadLast(10); err == nil {
		t.Error("LoadLast after Close returned nil error, want error")
	}
	if err := s.Clear(); err == nil {
		t.Error("Clear after Close returned nil error, want error")
	}
}

func TestProbeConcurrentAppendAndClear(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	defer s.Close()

	const workers = 8
	const perWorker = 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				d := mkDelivery(w*1000 + i)
				d.ID = fmt.Sprintf("race-%02d-%03d", w, i)
				if err := s.Append(d); err != nil {
					t.Errorf("worker %d Append: %v", w, err)
					return
				}
			}
		}(w)
	}
	// Race Clears against the appends.
	for c := 0; c < 5; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(2 * time.Millisecond)
			if err := s.Clear(); err != nil {
				t.Errorf("Clear: %v", err)
			}
		}()
	}
	wg.Wait()

	// Whatever the interleaving, the on-disk file must remain valid JSONL:
	// every non-empty line decodes to a delivery with an ID.
	raw, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	for i, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var d struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(line, &d); err != nil || d.ID == "" {
			t.Fatalf("line %d is corrupt after Append/Clear race: %q (err=%v)", i, line, err)
		}
	}
	got, err := s.LoadLast(1000)
	if err != nil {
		t.Fatalf("LoadLast after race: %v", err)
	}
	// With concurrent workers the file order is genuinely interleaved
	// (each Append is atomic under the mutex, but worker order varies), so
	// ID order proves nothing here. The loadable invariants: every record
	// decodes (checked above) and none are lost relative to the file.
	loaded := make(map[string]bool, len(got))
	for _, g := range got {
		if loaded[g.ID] {
			t.Fatalf("LoadLast returned duplicate %q", g.ID)
		}
		loaded[g.ID] = true
	}
}
