package ui

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type byteCounter struct{ bytes, writes atomic.Int64 }

func (c *byteCounter) Write(b []byte) (int, error) {
	c.bytes.Add(int64(len(b)))
	c.writes.Add(1)
	return len(b), nil
}

// NANNY_BYTES=1 go test ./internal/ui -run OutBytes -v: what the screen writes with 10 panes and one printing.
func TestOutBytes(t *testing.T) {
	if os.Getenv("NANNY_BYTES") != "1" {
		t.Skip("set NANNY_BYTES=1")
	}
	m := manyPanes(t, 10)
	out := &byteCounter{}
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(out), tea.WithoutSignalHandler())
	done := make(chan struct{})
	go func() { _, _ = p.Run(); close(done) }()
	p.Send(tea.WindowSizeMsg{Width: 200, Height: 40})
	time.Sleep(time.Second)
	pane := m.space().Tab().Panes[0]
	b0, w0 := out.bytes.Load(), out.writes.Load()
	const secs = 5
	for i := 0; i < secs*10; i++ {
		pane.Note(fmt.Sprintf("line %d\r\n", i))
		time.Sleep(100 * time.Millisecond)
	}
	b, w := out.bytes.Load()-b0, out.writes.Load()-w0
	p.Quit()
	<-done
	t.Logf("bytes/s=%d writes/s=%d bytes/write=%d", b/secs, w/secs, b/max(w, 1))
}
