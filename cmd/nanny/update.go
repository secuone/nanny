package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// releasesAPI is where the latest release is asked for; tests point it elsewhere.
var releasesAPI = "https://api.github.com/repos/secuone/nanny/releases/latest"

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		URL string `json:"browser_download_url"`
	} `json:"assets"`
}

// cmdUpdate replaces this binary with the latest release (--check only says
// whether there is one).
func cmdUpdate(args []string) {
	check := len(args) > 0 && args[0] == "--check"
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fail("can't find this binary: %v", err)
	}
	msg, err := update(exe, version, check)
	if err != nil {
		fail("%v", err)
	}
	fmt.Println(msg)
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "nanny update: "+f+"\n", a...)
	os.Exit(1)
}

func update(exe, current string, check bool) (string, error) {
	client := &http.Client{Timeout: 2 * time.Minute}
	var rel release
	if err := getJSON(client, releasesAPI, &rel); err != nil {
		return "", fmt.Errorf("can't ask GitHub for the latest release: %w", err)
	}
	latest := strings.TrimPrefix(rel.Tag, "v")
	if latest == "" {
		return "", errors.New("the latest release has no version")
	}
	if !newer(latest, current) {
		return fmt.Sprintf("nanny %s is up to date (latest release: %s)", current, latest), nil
	}
	if check {
		return fmt.Sprintf("nanny %s is out: you have %s. Run `nanny update` to get it.", latest, current), nil
	}
	tarName := fmt.Sprintf("nanny_%s_linux_%s.tar.gz", latest, runtime.GOARCH)
	tarURL, sumsURL := rel.asset(tarName), rel.asset("checksums.txt")
	if tarURL == "" || sumsURL == "" {
		return "", fmt.Errorf("release %s has no %s or checksums.txt", latest, tarName)
	}
	if err := writable(filepath.Dir(exe)); err != nil {
		return "", fmt.Errorf("can't replace %s (%v). If a package installed it, update the package; else run it with the rights to write there", exe, err)
	}
	sums, err := get(client, sumsURL)
	if err != nil {
		return "", err
	}
	archive, err := get(client, tarURL)
	if err != nil {
		return "", err
	}
	if err := verify(archive, tarName, sums); err != nil {
		return "", err
	}
	bin, err := untarBinary(archive)
	if err != nil {
		return "", err
	}
	if err := replace(exe, bin); err != nil {
		return "", err
	}
	return fmt.Sprintf("updated nanny %s → %s. Quit nanny and open it again to use it; running panes keep going.", current, latest), nil
}

func (r release) asset(name string) string {
	for _, a := range r.Assets {
		if filepath.Base(a.URL) == name {
			return a.URL
		}
	}
	return ""
}

func getJSON(c *http.Client, url string, v any) error {
	b, err := get(c, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func get(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// newer says whether release a is newer than build b. A build ahead of a
// release ("0.1.3-dev+abc", from a clone) takes only that release or a later
// one; a plain "dev" takes any release.
func newer(a, b string) bool {
	pa, okA := semver(a)
	base, pre, _ := strings.Cut(b, "-")
	base, _, _ = strings.Cut(base, "+")
	pb, okB := semver(base)
	if !okA {
		return false
	}
	if !okB {
		return true
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return pre != "" // 0.1.3 is newer than 0.1.3-dev
}

func semver(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// verify checks data against its line in checksums.txt.
func verify(data []byte, name string, sums []byte) error {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[1] == name {
			got := sha256.Sum256(data)
			if hex.EncodeToString(got[:]) != f[0] {
				return fmt.Errorf("%s doesn't match its checksum: not installed", name)
			}
			return nil
		}
	}
	return fmt.Errorf("checksums.txt has no line for %s", name)
}

func untarBinary(archive []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("the archive has no nanny binary")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "nanny" {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}

func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".nanny-update-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

// replace swaps exe for bin in one rename: a running nanny and the pane
// holders keep the old file open and go on.
func replace(exe string, bin []byte) error {
	f, err := os.CreateTemp(filepath.Dir(exe), ".nanny-update-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(bin); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, exe)
}
