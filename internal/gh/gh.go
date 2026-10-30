// Package gh asks GitHub through the gh CLI: your pull requests, the reviews
// waiting for you and your week. Every call goes through Exec, so tests put
// a fake in its place and never reach GitHub.
package gh

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sort"
	"strings"
)

// Runner runs gh with args and returns what it printed.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Exec is how gh is run; tests replace it.
var Exec Runner = run

// Error is gh's own message for a failed call.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// ErrNoGH and ErrNoAuth are the two states that need you, not a retry: gh is
// not installed, or it has no session.
var (
	ErrNoGH   = errors.New("gh is not installed")
	ErrNoAuth = errors.New("gh is not logged in")
)

func run(ctx context.Context, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, ErrNoGH
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, &Error{msg}
		}
		return out, err
	}
	return out, nil
}

// NoSession says whether err means gh has to be set up: missing or logged out.
func NoSession(err error) bool {
	if errors.Is(err, ErrNoGH) || errors.Is(err, ErrNoAuth) {
		return true
	}
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, w := range []string{"gh auth login", "not logged in", "authentication required", "bad credentials", "http 401"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// Viewer is the login of the account gh is signed in with.
func Viewer(ctx context.Context) (string, error) {
	out, err := Exec(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	login := strings.TrimSpace(string(out))
	if login == "" {
		return "", ErrNoAuth
	}
	return login, nil
}

// graphql runs a query with string variables (-f: never converted to numbers).
func graphql(ctx context.Context, query string, vars map[string]string) ([]byte, error) {
	args := []string{"api", "graphql", "-f", "query=" + query}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-f", k+"="+vars[k])
	}
	return Exec(ctx, args...)
}

// ExecDir is Exec in a folder, for the calls that act on a checkout.
var ExecDir = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, ErrNoGH
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &Error{strings.TrimSpace(string(out))}
	}
	return out, nil
}
