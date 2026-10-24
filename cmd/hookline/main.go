// Command hookline runs the webhook inspector TUI: a local HTTP capture
// listener plus a terminal interface for inspecting, replaying and editing
// inbound deliveries. See docs/ARCHITECTURE.md for how the pieces fit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hookline/internal/delivery"
	"hookline/internal/listener"
	"hookline/internal/store"
	"hookline/internal/style"
	"hookline/internal/ui"
)

// version is set at build time via -ldflags "-X main.version=…".
var version = "dev"

func main() {
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
		if err := st.Clear(); err != nil {
			return fmt.Errorf("clearing store: %w", err)
		}
	}
	history, err := st.LoadLast(200)
	if err != nil {
		// History is a convenience, never fatal: start empty instead.
		history = nil
	}

	events := make(chan delivery.Delivery, 1024)
	capture := func(d delivery.Delivery) {
		// Persist first so a crash never loses a delivery; then hand it to
		// the UI, dropping rather than blocking if the UI has exited.
		_ = st.Append(d)
		select {
		case events <- d:
		default:
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
		Target:    strings.TrimSuffix(*target, "/"),
		OnClear:   func() { _ = st.Clear() },
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
