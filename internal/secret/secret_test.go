package secret

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fakeTool(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret-tool"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(JiraEnv, "")
	return dir
}

func noTool(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(JiraEnv, "")
}

func TestFileFallbackIsPrivate(t *testing.T) {
	noTool(t)
	if err := Set(JiraService, "me@acme.atlassian.net", "tok"); err != nil {
		t.Fatal(err)
	}
	p := path(JiraService, "me@acme.atlassian.net")
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", st, err)
	}
	if d, _ := os.Stat(filepath.Dir(p)); d.Mode().Perm() != 0o700 {
		t.Errorf("dir is %v", d.Mode().Perm())
	}
	if got, err := Get(JiraService, "me@acme.atlassian.net"); err != nil || got != "tok" {
		t.Errorf("get = %q, %v", got, err)
	}
	if err := Delete(JiraService, "me@acme.atlassian.net"); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(JiraService, "me@acme.atlassian.net"); err != ErrNotFound {
		t.Errorf("after delete: %v", err)
	}
}

func TestFileTightenedWhenItExisted(t *testing.T) {
	noTool(t)
	p := path(JiraService, "me@h")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("old"), 0o644)
	if err := Set(JiraService, "me@h", "new"); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	d, _ := os.Stat(filepath.Dir(p))
	if st.Mode().Perm() != 0o600 || d.Mode().Perm() != 0o700 {
		t.Errorf("modes %v %v", st.Mode().Perm(), d.Mode().Perm())
	}
}

func TestEnvBeatsEverything(t *testing.T) {
	noTool(t)
	_ = Set(JiraService, "me@h", "stored")
	t.Setenv(JiraEnv, "fromenv")
	if got, _ := Get(JiraService, "me@h"); got != "fromenv" {
		t.Errorf("got %q", got)
	}
}

func TestSecretToolArgumentsAndStdin(t *testing.T) {
	dir := fakeTool(t, `echo "$@" > "$(dirname "$0")/args"; cat > "$(dirname "$0")/stdin"; echo from-keyring`)
	if err := Set(JiraService, "me@acme.atlassian.net", "tok123"); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if want := "store --label nanny Jira acme.atlassian.net service nanny-jira account me@acme.atlassian.net\n"; string(args) != want {
		t.Errorf("args = %q", args)
	}
	if in, _ := os.ReadFile(filepath.Join(dir, "stdin")); string(in) != "tok123" {
		t.Errorf("stdin = %q", in)
	}
	if _, err := os.Stat(path(JiraService, "me@acme.atlassian.net")); err == nil {
		t.Error("with a keyring there should be no file")
	}
	if got, _ := Get(JiraService, "me@acme.atlassian.net"); got != "from-keyring" {
		t.Errorf("get = %q", got)
	}
	args, _ = os.ReadFile(filepath.Join(dir, "args"))
	if want := "lookup service nanny-jira account me@acme.atlassian.net\n"; string(args) != want {
		t.Errorf("lookup args = %q", args)
	}
}

func TestFailingKeyringFallsBackToFile(t *testing.T) {
	fakeTool(t, "exit 1")
	if err := Set(JiraService, "me@h", "tok"); err != nil {
		t.Fatal(err)
	}
	if got, err := Get(JiraService, "me@h"); err != nil || got != "tok" {
		t.Errorf("get = %q, %v", got, err)
	}
}

func TestHangingKeyringTimesOut(t *testing.T) {
	old := timeout
	timeout = 200 * time.Millisecond
	t.Cleanup(func() { timeout = old })
	fakeTool(t, "exec sleep 30")
	if err := Set(JiraService, "me@h", "tok"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(JiraService, "me@h"); got != "tok" {
		t.Errorf("get = %q", got)
	}
}
