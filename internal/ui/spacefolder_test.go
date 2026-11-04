package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/ws"
)

// folders makes dirs under the test home and returns the home.
func folders(t *testing.T, m tea.Model, names ...string) string {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(home, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestNewSpaceNeedsAFolderAndNamesItAfterIt(t *testing.T) {
	m := bootEmpty(t, 120, 40)
	folders(t, m, "billing-api")
	m = key(m, "N")
	if m.(Model).fp == nil {
		t.Fatal("N should ask for a folder first")
	}
	m = key(m, "enter") // nothing typed
	if fp := m.(Model).fp; fp == nil || !strings.Contains(fp.err, "choose a folder") {
		t.Fatalf("an empty folder must be refused, got %+v", fp)
	}
	m = typeText(m, "~/billing-api")
	m = key(m, "enter")
	mm := m.(Model)
	if mm.fp != nil || mm.modal == nil || mm.modal.input.Value() != "billing-api" {
		t.Fatalf("the name should default to the folder's: fp=%v modal=%+v", mm.fp, mm.modal)
	}
	m = key(m, "enter")
	mm = m.(Model)
	if len(mm.spaces) != 1 || mm.spaces[0].Name != "billing-api" || mm.spaces[0].Dir() != filepath.Join(home, "billing-api") {
		t.Fatalf("space not created from the folder: %+v", mm.spaces)
	}
}

func TestNewSpaceNameCanBeEdited(t *testing.T) {
	m := bootEmpty(t, 120, 40)
	folders(t, m, "svc")
	m = typeText(key(m, "N"), "~/svc")
	m = key(m, "enter")
	m = typeText(m, "-x")
	m = key(m, "enter")
	if got := m.(Model).spaces[0].Name; got != "svc-x" {
		t.Errorf("name %q", got)
	}
}

func TestNewSpaceRefusesBadFolders(t *testing.T) {
	m := bootEmpty(t, 120, 40)
	folders(t, m)
	if err := os.WriteFile(filepath.Join(home, "afile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m = typeText(key(m, "N"), "~/afile")
	m = key(m, "enter")
	if fp := m.(Model).fp; fp == nil || !strings.Contains(fp.err, "not a folder") {
		t.Fatalf("a file is not a folder: %+v", fp)
	}
	if !strings.Contains(screen(m), "not a folder") {
		t.Error("the error should show")
	}
	if len(m.(Model).spaces) != 0 {
		t.Error("no space may be created")
	}
}

func TestNewSpaceCanCreateTheFolder(t *testing.T) {
	m := bootEmpty(t, 120, 40)
	m = typeText(key(m, "N"), "~/fresh")
	m = key(m, "enter")
	if fp := m.(Model).fp; fp == nil || fp.create == "" {
		t.Fatal("a missing folder should offer to create it")
	}
	if !strings.Contains(screen(m), "does not exist") {
		t.Error("the question should show")
	}
	m = key(m, "n")
	if _, err := os.Stat(filepath.Join(home, "fresh")); err == nil {
		t.Fatal("n must not create it")
	}
	m = key(m, "enter")
	m = key(m, "y")
	if st, err := os.Stat(filepath.Join(home, "fresh")); err != nil || !st.IsDir() {
		t.Fatalf("y should create it: %v", err)
	}
	if m.(Model).modal == nil {
		t.Error("then the name is asked")
	}
}

func TestFolderPickerTabCompletes(t *testing.T) {
	m := bootEmpty(t, 120, 40)
	folders(t, m, "alpha", "alps", "beta")
	m = typeText(key(m, "N"), "~/al")
	m = key(m, "tab")
	if v := m.(Model).fp.in.Value(); v != "~/alp" {
		t.Errorf("common prefix expected, got %q", v)
	}
	m = typeText(m, "h")
	m = key(m, "tab")
	if v := m.(Model).fp.in.Value(); v != "~/alpha/" {
		t.Errorf("single match expected, got %q", v)
	}
	m = typeText(key(m, "ctrl+u"), "") // no-op keys must not break it
	fp := m.(Model).fp
	fp.in.SetValue("~/b")
	fp.refresh()
	m = key(m, "tab")
	if v := m.(Model).fp.in.Value(); v != "~/beta/" {
		t.Errorf("got %q", v)
	}
}

func TestFolderPickerFiltersKnownFolders(t *testing.T) {
	m := bootEmpty(t, 120, 40)
	m = key(m, "N")
	fp := m.(Model).fp
	fp.known = []string{"/work/billing-api", "/work/blog", "/work/zeta"}
	fp.in.SetValue("bil")
	fp.refresh()
	if len(fp.cands) != 1 || fp.cands[0] != "/work/billing-api" {
		t.Fatalf("fuzzy: %v", fp.cands)
	}
	m = key(m, "tab")
	if v := m.(Model).fp.in.Value(); v != "/work/billing-api/" {
		t.Errorf("tab on a known folder: %q", v)
	}
}

func TestLegacySpaceWithoutFolderShowsTheMarker(t *testing.T) {
	m := bootEmpty(t, 140, 40)
	mm := m.(Model)
	mm.spaces = append(mm.spaces, ws.NewSpace("legacy"))
	mm.cur = 0
	mm.layoutPanes()
	out := screen(mm)
	if strings.Count(out, "no folder · set") < 2 { // sidebar and the space itself
		t.Errorf("marker missing:\n%s", out)
	}
	if mm.spaces[0].HasFolder() || mm.spaces[0].Dir() != osHome() {
		t.Error("a legacy space keeps $HOME as its working folder")
	}
	mm.mgr = &manager{}
	if !strings.Contains(screen(mm), "no folder · set") {
		t.Error("the manager should say it too")
	}
}

func TestSetFolderChangesWhereNewPanesStart(t *testing.T) {
	m := bootEmpty(t, 140, 40)
	folders(t, m, "one", "two")
	one, two := filepath.Join(home, "one"), filepath.Join(home, "two")
	mm := m.(Model)
	sp := ws.NewSpace("proj", one)
	mm.spaces = append(mm.spaces, sp)
	mm.cur = 0
	mm.openTabIn(sp, "shell", []string{"/bin/sh"})
	old := sp.Tabs[0].Panes[0]

	mm.openManager()
	m = key(mm, "f")
	if m.(Model).fp == nil {
		t.Fatal("f in the manager should ask for the folder")
	}
	m.(Model).fp.in.SetValue("~/two")
	m = key(m, "enter")
	mm = m.(Model)
	if sp.Dir() != two {
		t.Fatalf("folder %q", sp.Dir())
	}
	if old.Cwd != one {
		t.Errorf("the open pane must stay in %s, is in %s", one, old.Cwd)
	}
	mm.openTabIn(sp, "shell", []string{"/bin/sh"})
	if got := sp.Tabs[1].Panes[0].Cwd; got != two {
		t.Errorf("new tab started in %s", got)
	}
	mm.split(true, []string{"/bin/sh"})
	if got := sp.Tabs[1].Panes[1].Cwd; got != two {
		t.Errorf("split started in %s", got)
	}
}

func TestSetFolderOnALegacySpace(t *testing.T) {
	m := bootEmpty(t, 140, 40)
	folders(t, m, "proj")
	mm := m.(Model)
	sp := ws.NewSpace("legacy")
	mm.spaces = append(mm.spaces, sp)
	mm.cur = 0
	m = mm
	m = key(m, "ctrl+b")
	m = key(m, "F")
	m.(Model).fp.in.SetValue("~/proj")
	m = key(m, "enter")
	if !sp.HasFolder() || sp.Dir() != filepath.Join(home, "proj") {
		t.Fatalf("folder not set: %v", sp.Paths)
	}
	if strings.Contains(screen(m), "no folder · set") {
		t.Error("the marker should be gone")
	}
}

func TestNewPanesStartInTheSpaceFolder(t *testing.T) {
	m := bootEmpty(t, 140, 40)
	folders(t, m, "proj", "proj/sub")
	proj := filepath.Join(home, "proj")
	mm := m.(Model)
	sp := ws.NewSpace("proj", proj)
	mm.spaces = append(mm.spaces, sp)
	mm.cur = 0

	mm.openTabIn(sp, "shell", []string{"/bin/sh"}) // the first tab of a new space
	if got := sp.Tabs[0].Panes[0].Cwd; got != proj {
		t.Errorf("first tab in %s", got)
	}
	// a shell that was sent somewhere else does not drag the next tab along
	sp.Tabs[0].Panes[0].Cwd = filepath.Join(proj, "sub")
	mm.openTabIn(sp, "claude", []string{"/bin/sh"})
	if got := sp.Tabs[1].Panes[0].Cwd; got != proj {
		t.Errorf("second tab in %s", got)
	}
	mm.split(false, []string{"/bin/sh"})
	if got := sp.Tabs[1].Panes[1].Cwd; got != proj {
		t.Errorf("split in %s", got)
	}
	// the shell of a view (o) starts in the folder it was asked for, inside its space
	mm.shell(filepath.Join(proj, "sub"))
	if n := len(mm.spaces); n != 1 {
		t.Errorf("a folder of the space must not make another space (%d)", n)
	}
	last := sp.Tabs[len(sp.Tabs)-1].Panes[0]
	if last.Cwd != filepath.Join(proj, "sub") {
		t.Errorf("shell in its folder: %s", last.Cwd)
	}
}

func TestShellOutsideEverySpaceMakesOneWithThatFolder(t *testing.T) {
	m := bootEmpty(t, 140, 40)
	folders(t, m, "elsewhere")
	mm := m.(Model)
	mm.shell(filepath.Join(home, "elsewhere"))
	if len(mm.spaces) != 1 || !mm.spaces[0].HasFolder() || mm.spaces[0].Dir() != filepath.Join(home, "elsewhere") {
		t.Fatalf("spaces: %+v", mm.spaces)
	}
}

func TestPaneWithoutADirectoryStartsInHome(t *testing.T) {
	m := bootEmpty(t, 140, 40)
	mm := m.(Model)
	p, err := mm.spawn("", []string{"/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Cwd != home {
		t.Errorf("cwd %q", p.Cwd)
	}
}

func osHome() string { h, _ := os.UserHomeDir(); return h }
