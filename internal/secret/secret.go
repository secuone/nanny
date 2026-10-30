// Package secret keeps a token out of the config file: in the desktop keyring
// through secret-tool when there is one, else in a file only you can read.
package secret

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/secuone/nanny/internal/config"
)

// ErrNotFound is returned when no backend holds the secret.
var ErrNotFound = errors.New("secret not found")

// JiraService is the service name of the Jira token; NANNY_JIRA_TOKEN beats whatever is stored.
const (
	JiraService = "nanny-jira"
	JiraEnv     = "NANNY_JIRA_TOKEN"
)

// timeout bounds secret-tool: without a session bus it can hang.
var timeout = 3 * time.Second

var lookPath = exec.LookPath

// Get returns the secret of account: the environment first, then the keyring, then the file.
func Get(service, account string) (string, error) {
	if service == JiraService {
		if v := strings.TrimSpace(os.Getenv(JiraEnv)); v != "" {
			return v, nil
		}
	}
	if out, err := run("", "lookup", "service", service, "account", account); err == nil {
		if v := strings.TrimSpace(out); v != "" {
			return v, nil
		}
	}
	b, err := os.ReadFile(path(service, account))
	if err != nil {
		return "", ErrNotFound
	}
	if v := strings.TrimSpace(string(b)); v != "" {
		return v, nil
	}
	return "", ErrNotFound
}

// Set stores the secret in the keyring, or in the file when there is no keyring.
func Set(service, account, value string) error {
	label := "nanny Jira " + host(account)
	if _, err := run(value, "store", "--label", label, "service", service, "account", account); err == nil {
		_ = os.Remove(path(service, account))
		return nil
	}
	p := path(service, account)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(value+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(p, 0o600)
}

// Delete forgets the secret everywhere it may be.
func Delete(service, account string) error {
	_, _ = run("", "clear", "service", service, "account", account)
	if err := os.Remove(path(service, account)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// run starts secret-tool with a time limit; stdin may be empty.
func run(stdin string, args ...string) (string, error) {
	bin, err := lookPath("secret-tool")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = time.Second
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// host is what follows the @ of an account ("me@acme.atlassian.net").
func host(account string) string {
	if i := strings.LastIndex(account, "@"); i >= 0 {
		return account[i+1:]
	}
	return account
}

// path is the fallback file: secrets/jira-<host> under the config folder.
func path(service, account string) string {
	name := strings.TrimPrefix(service, "nanny-") + "-" + unsafe.ReplaceAllString(host(account), "_")
	return filepath.Join(config.Dir(), "secrets", name)
}
