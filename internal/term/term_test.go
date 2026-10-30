package term

import (
	"strings"
	"testing"
	"time"
)

func waitFor(t *testing.T, p *Pane, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.Text(30), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, p.Text(30))
}

func TestShellRoundTrip(t *testing.T) {
	p, err := Start(t.TempDir(), []string{"/bin/sh"}, 60, 10, []string{"PS1=$ "}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.Write([]byte("echo nanny-$((40+2))\r"))
	waitFor(t, p, "nanny-42")
	p.Resize(80, 20)
	p.Write([]byte("tput cols\r"))
	waitFor(t, p, "80")
	if v := p.View(true); len(v) != 20 {
		t.Errorf("view has %d lines, want 20", len(v))
	}
	p.Write([]byte("exit\r"))
	deadline := time.Now().Add(3 * time.Second)
	for !p.Exited() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !p.Exited() {
		t.Error("shell did not exit")
	}
}

func TestBlockedDetection(t *testing.T) {
	p, err := Start(t.TempDir(), []string{"/bin/sh", "-c", "printf 'Do you want to proceed?\\n❯ 1. Yes\\n  2. No\\n'; sleep 5"}, 60, 10, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	waitFor(t, p, "2. No")
	if s := p.AgentState(); s != "blocked" {
		t.Errorf("state = %s, want blocked", s)
	}
}
