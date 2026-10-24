// Package store persists deliveries as one JSON object per line in a single
// append-only JSONL file. Appends are crash-safe: a torn or corrupt line is
// skipped when the file is read back.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"hookline/internal/delivery"
)

// MaxBodyBytes is the body size limit enforced by Append; larger bodies are
// stored truncated with Truncated set to true.
const MaxBodyBytes = 2 << 20

// fileName is the JSONL file managed inside the store directory.
const fileName = "hookline.jsonl"

// Store is a JSONL-backed persistence for deliveries.
type Store struct {
	mu   sync.Mutex
	path string
	f    *os.File
	w    *bufio.Writer
}

// Open creates dir if needed and opens (or creates) hookline.jsonl inside it
// for both appending and reading.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("store: create directory %s: %w", dir, err)
	}
	path := filepath.Join(dir, fileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	return &Store{path: path, f: f, w: bufio.NewWriter(f)}, nil
}

// Append writes d as a single JSON line. Bodies larger than MaxBodyBytes are
// stored truncated with Truncated set; d itself is not modified. The line is
// flushed to disk before Append returns.
func (s *Store) Append(d delivery.Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return fmt.Errorf("store: %s is closed", s.path)
	}
	if len(d.Body) > MaxBodyBytes {
		d.Body = d.Body[:MaxBodyBytes]
		d.Truncated = true
	}
	line, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("store: encode delivery %s: %w", d.ID, err)
	}
	line = append(line, '\n')
	if _, err := s.w.Write(line); err != nil {
		return fmt.Errorf("store: write %s: %w", s.path, err)
	}
	if err := s.w.Flush(); err != nil {
		return fmt.Errorf("store: flush %s: %w", s.path, err)
	}
	return nil
}

// LoadLast returns up to n most recent deliveries in chronological order
// (oldest first). Corrupt or partial lines, such as a torn write from a
// crash mid-append, are skipped rather than reported as errors.
func (s *Store) LoadLast(n int) ([]delivery.Delivery, error) {
	if n <= 0 {
		return []delivery.Delivery{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil, fmt.Errorf("store: %s is closed", s.path)
	}
	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("store: seek %s: %w", s.path, err)
	}
	ring := make([]delivery.Delivery, n)
	count := 0
	r := bufio.NewReader(s.f)
	for {
		line, rerr := r.ReadBytes('\n')
		// A well-formed line decodes to a record carrying an id; anything
		// else (torn write, stray text) is treated as corrupt and skipped.
		if len(line) > 0 {
			var d delivery.Delivery
			if err := json.Unmarshal(trimEOL(line), &d); err == nil && d.ID != "" {
				ring[count%n] = d
				count++
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("store: read %s: %w", s.path, rerr)
		}
	}
	m := count
	if m > n {
		m = n
	}
	out := make([]delivery.Delivery, 0, m)
	for i := 0; i < m; i++ {
		out = append(out, ring[(count-m+i)%n])
	}
	return out, nil
}

// Clear removes every stored delivery by truncating the file. The store
// remains open for further appends.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return fmt.Errorf("store: %s is closed", s.path)
	}
	if err := s.w.Flush(); err != nil {
		return fmt.Errorf("store: clear %s: %w", s.path, err)
	}
	// Windows denies truncating through an append-mode handle, so close,
	// truncate by path, and reopen.
	closeErr := s.f.Close()
	s.f = nil
	truncErr := os.Truncate(s.path, 0)
	f, openErr := os.OpenFile(s.path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if openErr == nil {
		s.f = f
		s.w = bufio.NewWriter(f)
	}
	for _, err := range []error{closeErr, truncErr, openErr} {
		if err != nil {
			return fmt.Errorf("store: clear %s: %w", s.path, err)
		}
	}
	return nil
}

// Close flushes and closes the underlying file. It is safe to call twice.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	werr := s.w.Flush()
	ferr := s.f.Close()
	s.f = nil
	if werr != nil {
		return fmt.Errorf("store: flush %s: %w", s.path, werr)
	}
	if ferr != nil {
		return fmt.Errorf("store: close %s: %w", s.path, ferr)
	}
	return nil
}

// trimEOL strips a trailing \n and any \r preceding it from a raw line.
func trimEOL(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}
