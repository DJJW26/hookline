// Command hookline runs the webhook inspector TUI: a local HTTP capture
// listener plus a terminal interface for inspecting, replaying and editing
// inbound deliveries. See docs/ARCHITECTURE.md for how the pieces fit.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DJJW26/hookline/internal/delivery"
	"github.com/DJJW26/hookline/internal/listener"
	"github.com/DJJW26/hookline/internal/store"
	"github.com/DJJW26/hookline/internal/style"
	"github.com/DJJW26/hookline/internal/ui"
)

// version is set at build time via -ldflags "-X main.version=…". When the
// binary was built by `go install module@version` (no ldflags), it falls
// back to the version Go recorded in the build info.
var version = "dev"

func main() {
	if version == "dev" || version == "" {
		if info, ok := debug.ReadBuildInfo(); ok &&
			info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hookline:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		port    = flag.Int("port", 4040, "port to listen on")
		bind    = flag.String("bind", "127.0.0.1", "address to bind")
		status  = flag.Int("status", 200, "status code the listener replies with")
		target  = flag.String("target", "", "default replay target, e.g. http://localhost:3000")
		themeID = flag.String("theme", "auto", "theme: auto, "+strings.Join(style.IDs(), ", "))
		fresh   = flag.Bool("fresh", false, "start with an empty history")
		showVer = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("hookline", version)
		return nil
	}

	theme, err := style.Detect(*themeID)
	if err != nil {
		return fmt.Errorf("%w (available: %s)", err, strings.Join(style.IDs(), ", "))
	}

	dataDir, err := resolveDataDir()
	if err != nil {
		return err
	}
	st, err := store.Open(dataDir)
	if err != nil {
		return fmt.Errorf("opening store in %s: %w", dataDir, err)
	}
	defer st.Close()
	if *fresh {
		// Rotate, never destroy: the previous history is kept aside as
		// hookline-<timestamp>.jsonl (5 most recent backups retained).
		if _, err := st.Rotate(5); err != nil {
			return fmt.Errorf("rotating history: %w", err)
		}
	}
	history, err := st.LoadLast(200)
	if err != nil {
		// History is a convenience, never fatal: start empty instead.
		history = nil
	}

	// Replay target: --target wins, otherwise the last one used (persisted
	// in the data dir) so a new terminal never starts targetless.
	cfgFile := loadConfig(dataDir)
	targetURL := strings.TrimSuffix(*target, "/")
	if targetURL == "" {
		targetURL = cfgFile.Target
	}
	if targetURL != cfgFile.Target {
		cfgFile.Target = targetURL
		_ = saveConfig(dataDir, cfgFile) // best effort; a read-only dir is not fatal
	}

	events := make(chan delivery.Delivery, 1024)
	var dropped atomic.Int64
	capture := func(d delivery.Delivery) {
		// Persist first so a crash never loses a delivery; then hand it to
		// the UI, dropping rather than blocking if the UI has exited. Drops
		// are counted and surfaced by the UI's status line — never silent.
		_ = st.Append(d)
		select {
		case events <- d:
		default:
			dropped.Add(1)
		}
	}

	ln, err := listener.Start(listener.Config{
		Host:       *bind,
		Port:       *port,
		Status:     *status,
		OnDelivery: capture,
	})
	if err != nil {
		return fmt.Errorf("listening on %s:%d: %w (is the port taken? try --port)", *bind, *port, err)
	}

	model := ui.New(ui.Config{
		Version:   version,
		ListenURL: ln.URL(),
		Events:    events,
		Initial:   history,
		Target:    targetURL,
		Dropped:   &dropped,
		OnClear:   func() { _ = st.Clear() },
		TargetSave: func(t string) {
			cfgFile.Target = t
			_ = saveConfig(dataDir, cfgFile)
		},
	}, style.New(theme), theme)

	// Bubble Tea installs its own SIGINT/SIGTERM handlers that restore the
	// terminal; nothing extra is needed here.
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := prog.Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		return fmt.Errorf("running interface: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ln.Stop(ctx); err != nil {
		return fmt.Errorf("stopping listener: %w", err)
	}
	close(events)
	return nil
}

// appConfig is the small persisted settings file in the data dir. Fields
// are optional; a missing or corrupt file means defaults.
type appConfig struct {
	Target string `json:"target,omitempty"`
}

func loadConfig(dataDir string) appConfig {
	raw, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return appConfig{}
	}
	var c appConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return appConfig{}
	}
	return c
}

func saveConfig(dataDir string, c appConfig) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, "config.json"), raw, 0o644)
}

// resolveDataDir honours HOOKLINE_DATA_DIR, then falls back to the
// platform data location (LOCALAPPDATA on Windows, XDG_DATA_HOME elsewhere).
func resolveDataDir() (string, error) {
	if dir := os.Getenv("HOOKLINE_DATA_DIR"); dir != "" {
		return dir, nil
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "hookline"), nil
		}
	}
	if base := os.Getenv("XDG_DATA_HOME"); base != "" {
		return filepath.Join(base, "hookline"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving data directory: %w", err)
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "AppData", "Local", "hookline"), nil
	}
	return filepath.Join(home, ".local", "share", "hookline"), nil
}
