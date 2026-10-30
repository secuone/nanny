package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/secuone/nanny/internal/term"
)

// TestMain doubles as the holder: panes re-exec the test binary as "nanny __hold".
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__hold" {
		term.HoldMain(os.Args[2:])
	}
	dir, err := os.MkdirTemp("", "nanny-sock")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("NANNY_SOCK_DIR", dir)
	_ = os.Setenv("NANNY_NO_WELCOME", "1")
	// never open the user's browser, touch the clipboard or the keyring, or ask GitHub:
	// the programs nanny starts are stand-ins here
	stubs := map[string]string{
		"xdg-open": "exit 0", "open": "exit 0",
		"wl-copy": "cat >/dev/null", "xclip": "cat >/dev/null", "xsel": "cat >/dev/null",
		"secret-tool": "exit 1",
		"gh":          "echo 'not logged in' >&2; exit 1",
	}
	for c, body := range stubs {
		_ = os.WriteFile(filepath.Join(dir, c), []byte("#!/bin/sh\n"+body+"\n"), 0o755)
	}
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	coalesce = false // tests read the screen right after each message
	code := m.Run()
	term.KillAll()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
