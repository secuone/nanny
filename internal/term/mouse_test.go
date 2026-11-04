package term

import (
	"strings"
	"testing"
	"time"
)

func TestSendMouseReachesProgram(t *testing.T) {
	p, err := Start(t.TempDir(), []string{"sh", "-c", `stty -icanon -echo; printf '\033[?1000h\033[?1006h'; exec cat -v`}, 60, 10, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.WantsMouse() {
		// may already be on; fine
	}
	waitUntil(t, p.WantsMouse)
	// answers are dropped while a freshly attached pane settles: keep trying
	waitUntil(t, func() bool {
		p.SendMouse(4, 3, false)
		return strings.Contains(p.Text(10), "^[[<2;5;4M")
	})
	p.SendMouse(4, 3, true)
	waitFor(t, p, "^[[<2;5;4m")
}

func TestPlainProgramDoesNotWantMouse(t *testing.T) {
	p, err := Start(t.TempDir(), []string{"sh", "-c", "exec cat -v"}, 60, 10, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.WantsMouse() {
		t.Fatal("no mouse mode was asked for")
	}
	p.SendMouse(1, 1, false)
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 250; i++ {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition never held")
}
