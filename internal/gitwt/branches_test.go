package gitwt

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commit(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", file)
}

// repo has main, a merged branch, a branch with its own commit, one with a
// worktree and one pushed to a bare origin.
func repo(t *testing.T) (root, origin string) {
	t.Helper()
	base := t.TempDir()
	root, origin = filepath.Join(base, "work"), filepath.Join(base, "origin.git")
	run(t, base, "init", "-q", "-b", "main", root)
	run(t, base, "init", "-q", "--bare", "-b", "main", origin)
	commit(t, root, "a")
	run(t, root, "remote", "add", "origin", origin)
	run(t, root, "push", "-q", "-u", "origin", "main")
	run(t, root, "branch", "done")
	run(t, root, "branch", "inwt")
	run(t, root, "worktree", "add", "-q", filepath.Join(base, "wt"), "inwt")
	run(t, root, "checkout", "-q", "-b", "work")
	commit(t, root, "b")
	run(t, root, "checkout", "-q", "-b", "pushed")
	run(t, root, "push", "-q", "-u", "origin", "pushed")
	commit(t, root, "c")
	run(t, root, "checkout", "-q", "main")
	return root, origin
}

func byName(bs []Branch) map[string]Branch {
	m := map[string]Branch{}
	for _, b := range bs {
		m[b.Name] = b
	}
	return m
}

func TestBranchesMergedAndUnpushed(t *testing.T) {
	root, _ := repo(t)
	bs := byName(Branches(root))
	if !bs["done"].Merged || bs["done"].Upstream != "" {
		t.Errorf("done is merged: %+v", bs["done"])
	}
	if bs["work"].Merged || bs["work"].Ahead != 0 || bs["work"].Upstream != "" {
		t.Errorf("work has a commit of its own and no upstream: %+v", bs["work"])
	}
	if bs["pushed"].Upstream != "origin/pushed" || bs["pushed"].Ahead != 1 || bs["pushed"].Merged {
		t.Errorf("pushed is one ahead: %+v", bs["pushed"])
	}
	if bs["main"].Merged {
		t.Error("the default branch is never merged into itself")
	}
	if bs["main"].Worktree != root {
		t.Errorf("main is checked out in the repo: %q", bs["main"].Worktree)
	}
	if bs["done"].When.IsZero() {
		t.Error("a branch has a date")
	}
}

func TestBranchesWithWorktreeAreMarked(t *testing.T) {
	root, _ := repo(t)
	bs := byName(Branches(root))
	if bs["inwt"].Worktree == "" || !bs["inwt"].Merged {
		t.Errorf("inwt is merged but checked out: %+v", bs["inwt"])
	}
	if bs["done"].Worktree != "" {
		t.Errorf("done is nowhere: %q", bs["done"].Worktree)
	}
}

func TestDeleteMergedBranch(t *testing.T) {
	root, _ := repo(t)
	if err := DeleteBranch(root, "done", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := byName(Branches(root))["done"]; ok {
		t.Error("done should be gone")
	}
	if err := DeleteBranch(root, "work", false); err == nil {
		t.Error("git refuses an unmerged branch without force")
	}
	if err := DeleteBranch(root, "work", true); err != nil {
		t.Errorf("force deletes it: %v", err)
	}
}

func TestPushSetsUpstreamAndGoneIsSeen(t *testing.T) {
	root, origin := repo(t)
	run(t, root, "checkout", "-q", "work")
	if err := Push(root, "work"); err != nil {
		t.Fatal(err)
	}
	if b := byName(Branches(root))["work"]; b.Upstream != "origin/work" || b.Ahead != 0 {
		t.Errorf("work is pushed: %+v", b)
	}
	run(t, root, "checkout", "-q", "main")
	run(t, origin, "branch", "-q", "-D", "work")
	run(t, root, "fetch", "-q", "--prune")
	if b := byName(Branches(root))["work"]; !b.Gone {
		t.Errorf("its upstream is gone: %+v", b)
	}
}

func TestDefaultBranchAndSlug(t *testing.T) {
	root, _ := repo(t)
	if d := DefaultBranch(root); d != "main" {
		t.Errorf("default %q", d)
	}
	for url, want := range map[string]string{
		"git@github.com:civitatis/nanny.git":       "civitatis/nanny",
		"https://github.com/civitatis/nanny":       "civitatis/nanny",
		"ssh://git@github.com/civitatis/nanny.git": "civitatis/nanny",
		"git@gitlab.com:a/b.git":                   "",
	} {
		run(t, root, "remote", "set-url", "origin", url)
		if got := Slug(root); got != want {
			t.Errorf("%s: %q, want %q", url, got, want)
		}
	}
}
