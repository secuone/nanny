package classroom

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func get(t *testing.T, url, host string) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if host != "" {
		req.Host = host
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, r.Header.Get("Content-Type"), string(b)
}

// The page, three.js and the snapshot are served under a random token, and
// only to requests for this very address.
func TestServer(t *testing.T) {
	s, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	if code, _, body := get(t, s.URL(), ""); code != 200 || !strings.Contains(body, "importmap") {
		t.Fatalf("page: %d", code)
	}
	for _, f := range []string{"app.js", "three/three.module.min.js", "three/three.core.min.js", "three/addons/controls/OrbitControls.js", "three/addons/renderers/CSS2DRenderer.js"} {
		if code, ct, _ := get(t, s.URL()+f, ""); code != 200 || !strings.Contains(ct, "javascript") {
			t.Errorf("%s: %d %s", f, code, ct)
		}
	}
	s.Set(Snapshot{Classes: []Class{{ID: "a", Title: "Refunds", Kids: []Kid{{ID: "k", Type: "Explore"}}}}})
	if code, ct, body := get(t, s.URL()+"state.json", ""); code != 200 || ct != "application/json" || !strings.Contains(body, `"Refunds"`) || !strings.Contains(body, `"Explore"`) {
		t.Errorf("state: %d %s %s", code, ct, body)
	}
	root := strings.TrimSuffix(s.URL(), "/")
	root = root[:strings.LastIndex(root, "/")+1]
	if code, _, _ := get(t, root+"state.json", ""); code != 404 {
		t.Errorf("without the token: %d", code)
	}
	if code, _, _ := get(t, s.URL()+"state.json", "evil.example:80"); code != 403 {
		t.Errorf("another host name (DNS rebinding): %d", code)
	}
}
