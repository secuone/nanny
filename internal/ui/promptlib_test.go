package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/config"
)

// P opens the prompt library: saved prompts and the most used ones from history.
func TestPromptLibrary(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := key(boot(t, 150, 42), "P").(Model)
	if m.lib == nil || !m.lib.loaded {
		t.Fatal("P should open the prompt library")
	}
	for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
		m.w, m.h = size[0], size[1]
		out := fits(t, m, size[0], size[1], "prompts")
		for _, want := range []string{"Prompts", "SAVED", "tests", "⏎ insert"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d library lacks %q:\n%s", size[0], size[1], want, out)
			}
		}
	}
	m.w, m.h = 150, 42
	out := fits(t, m, 150, 42, "prompts")
	for _, want := range []string{"MOST USED", "×9 Run the test suite and fix", "/clear ×7"} {
		if !strings.Contains(out, want) {
			t.Errorf("library lacks %q:\n%s", want, out)
		}
	}
	// filter, then save the top most-used prompt under a name
	m = key(m, "/").(Model)
	m.lib.filter.SetValue("show me the diff")
	m.lib.rebuild()
	if r := m.lib.sel(); r == nil || r.u == nil || r.u.Count != 9 {
		t.Fatalf("filter picked %+v", r)
	}
	m = key(m, "enter").(Model)
	m = key(m, "s").(Model)
	if m.modal == nil {
		t.Fatal("s should ask for a name")
	}
	m.modal.input.SetValue("tests-diff")
	m = key(m, "enter").(Model)
	m = key(m, "esc").(Model) // clears the filter
	m.lib.selectSaved("tests-diff")
	if r := m.lib.sel(); r == nil || r.s == nil || !strings.HasPrefix(r.s.Text, "Run the test suite and fix") {
		t.Fatalf("saved prompt not selected: %+v", r)
	}
	m = key(m, "p").(Model)
	if r := m.lib.sel(); r == nil || r.s == nil || !r.s.Pinned || m.lib.cur != 0 {
		t.Errorf("p should pin it to the top: %+v at %d", r, m.lib.cur)
	}
	// n: the multi-line editor
	m = key(m, "n").(Model)
	if m.lib.ed == nil {
		t.Fatal("n should open the editor")
	}
	for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
		m.w, m.h = size[0], size[1]
		if out := fits(t, m, size[0], size[1], "prompt editor"); !strings.Contains(out, "New prompt") || !strings.Contains(out, "ctrl+s save") {
			t.Errorf("%dx%d editor incomplete:\n%s", size[0], size[1], out)
		}
	}
	m.w, m.h = 150, 42
	m.lib.ed.name.SetValue("deploy")
	m.lib.ed.tags.SetValue("ops, ci")
	m.lib.ed.text.SetValue("Deploy {branch} to staging\nand watch the logs.")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = mm.(Model)
	if m.lib.ed != nil {
		t.Fatalf("ctrl+s should save: %s", m.status)
	}
	var got *config.Snippet
	for _, s := range config.LoadSnippets() {
		if s.Name == "deploy" {
			got = &s
		}
	}
	if got == nil || got.Text != "Deploy {branch} to staging\nand watch the logs." || strings.Join(got.Tags, ",") != "ops,ci" {
		t.Fatalf("saved %+v", got)
	}
	// d deletes after a confirm
	m.lib.selectSaved("deploy")
	m = key(m, "d").(Model)
	m = key(m, "y").(Model)
	for _, s := range config.LoadSnippets() {
		if s.Name == "deploy" {
			t.Error("d then y should delete the prompt")
		}
	}
	if m = key(m, "esc").(Model); m.libShown() {
		t.Error("esc should close the library")
	}
}

// ⏎ on a saved prompt asks for its {placeholders}, types it into the pane and counts the use.
func TestPromptIntoPane(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := withAgents(t, 150, 42, demoIdle)
	m.cur = len(m.spaces) - 1
	m.layoutPanes()
	p := m.space().Tab().Pane()
	screenHas(t, p, "────")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = key(mm, "P").(Model)
	if m.lib == nil {
		t.Fatal("prefix P should open the prompt library")
	}
	if out := fits(t, m, 150, 42, "prompts in a space"); !strings.Contains(out, "⏎ insert") {
		t.Errorf("footer lacks the library keys:\n%s", out)
	}
	m.lib.selectSaved("ticket")
	m = key(m, "enter").(Model)
	if m.modal == nil || !strings.Contains(m.modal.title, "{ticket}") {
		t.Fatal("a prompt with {ticket} should ask for it")
	}
	m.modal.input.SetValue("PAY-412")
	m = key(m, "enter").(Model)
	if m.libShown() || m.dash {
		t.Error("inserting should go back to the pane")
	}
	screenHas(t, p, "Read PAY-412 and plan the change")
	for _, s := range config.LoadSnippets() {
		if s.Name == "ticket" && s.Uses != 1 {
			t.Errorf("ticket used %d times", s.Uses)
		}
	}
	// the palette ranks it first now
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = key(mm, ":").(Model)
	m.pal.input.SetValue("snippet")
	m.pal.filter()
	for _, i := range m.pal.shown {
		if e := m.pal.entries[i]; e.kind == "snippet" {
			if e.label != "Snippet: ticket ×1" {
				t.Errorf("palette top snippet %q", e.label)
			}
			break
		}
	}
}
