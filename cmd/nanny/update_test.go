package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func tarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	tw := tar.NewWriter(zw)
	for _, f := range []struct {
		n string
		b []byte
	}{{"LICENSE", []byte("MIT")}, {name, body}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.n, Mode: 0o755, Size: int64(len(f.b)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(f.b)
	}
	tw.Close()
	zw.Close()
	return b.Bytes()
}

// fakeRelease serves a release v0.9.0 whose checksum is right or not.
func fakeRelease(t *testing.T, goodSum bool) {
	t.Helper()
	name := fmt.Sprintf("nanny_0.9.0_linux_%s.tar.gz", runtime.GOARCH)
	archive := tarGz(t, "nanny", []byte("new binary"))
	sum := sha256.Sum256(archive)
	if !goodSum {
		sum[0]++
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v0.9.0","assets":[{"browser_download_url":"%[1]s/dl/%[2]s"},{"browser_download_url":"%[1]s/dl/checksums.txt"}]}`, srv.URL, name)
	})
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  other.tar.gz\n%s  %s\n", strings.Repeat("0", 64), hex.EncodeToString(sum[:]), name)
	})
	was := releasesAPI
	releasesAPI = srv.URL + "/latest"
	t.Cleanup(func() { releasesAPI = was })
}

func oldBinary(t *testing.T) string {
	exe := filepath.Join(t.TempDir(), "nanny")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestUpdateReplacesTheBinary(t *testing.T) {
	fakeRelease(t, true)
	exe := oldBinary(t)
	msg, err := update(exe, "0.1.3", false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "new binary" || !strings.Contains(msg, "0.1.3 → 0.9.0") {
		t.Errorf("binary %q, message %q", got, msg)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode %v", fi.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".nanny-update-*")); len(left) > 0 {
		t.Errorf("left behind %v", left)
	}
}

func TestUpdateRefusesABadChecksum(t *testing.T) {
	fakeRelease(t, false)
	exe := oldBinary(t)
	if _, err := update(exe, "0.1.3", false); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("binary replaced: %q", got)
	}
}

func TestUpdateCheckAndUpToDate(t *testing.T) {
	fakeRelease(t, true)
	exe := oldBinary(t)
	for _, c := range []struct {
		have  string
		check bool
		want  string
	}{
		{"0.1.3", true, "0.9.0 is out"},
		{"0.9.0", false, "up to date"},
		{"1.0.0", false, "up to date"},
	} {
		msg, err := update(exe, c.have, c.check)
		if err != nil || !strings.Contains(msg, c.want) {
			t.Errorf("%s check=%v: %q %v", c.have, c.check, msg, err)
		}
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("binary replaced: %q", got)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.3", "0.1.2", true},
		{"0.2.0", "0.1.10", true},
		{"0.1.10", "0.2.0", false},
		{"0.1.3", "0.1.3", false},
		{"0.1.3", "dev", true},
		{"0.1.3", "0.1.3-dev+83eaf8d", true},
		{"0.1.2", "0.1.3-dev+83eaf8d", false},
		{"0.1.3", "0.1.3+dirty", false},
		{"junk", "0.1.0", false},
	} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
