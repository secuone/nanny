package sources

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Docker and Tilt answer over their own APIs: asking them directly costs
// next to nothing, where starting `docker ps` or `tilt get` every few seconds
// cost tens to hundreds of milliseconds of CPU each time.

// dockerSock is the local Docker socket, "" when Docker is somewhere else.
func dockerSock() string {
	h := os.Getenv("DOCKER_HOST")
	switch {
	case h == "":
		h = "/var/run/docker.sock"
	case strings.HasPrefix(h, "unix://"):
		h = strings.TrimPrefix(h, "unix://")
	default:
		return ""
	}
	if os.Getenv("DOCKER_CONTEXT") != "" {
		return ""
	}
	return h
}

var dockerClient = sync.OnceValue(func() *http.Client {
	sock := dockerSock()
	return &http.Client{Timeout: 1500 * time.Millisecond, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
})

func get(c *http.Client, url string, header map[string]string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	r, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, r.Status)
	}
	return io.ReadAll(io.LimitReader(r.Body, 32<<20))
}

// dockerAPI is GET path on the local Docker socket; ok is false when there is
// no socket to ask (Docker elsewhere, no permission), and the CLI is used.
func dockerAPI(path string) ([]byte, bool) {
	sock := dockerSock()
	if sock == "" {
		return nil, false
	}
	if _, err := os.Stat(sock); err != nil {
		return nil, false
	}
	b, err := get(dockerClient(), "http://docker"+path, nil)
	return b, err == nil
}

type dockerPort struct {
	IP          string
	PrivatePort int
	PublicPort  int
	Type        string
}

// dockerPorts writes ports the way `docker ps` does: 0.0.0.0:8080->80/tcp, 5432/tcp.
func dockerPorts(ps []dockerPort) string {
	var out []string
	for _, p := range ps {
		if p.PublicPort == 0 {
			out = append(out, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
			continue
		}
		ip := p.IP
		if strings.Contains(ip, ":") {
			ip = "[" + ip + "]"
		}
		out = append(out, fmt.Sprintf("%s:%d->%d/%s", ip, p.PublicPort, p.PrivatePort, p.Type))
	}
	return strings.Join(out, ", ")
}

// tilt is how to reach the API of the Tilt running on TILT_PORT (10350 by
// default), from the config `tilt get` itself reads.
type tiltConn struct {
	at     time.Time // the config's mtime it was read from
	server string
	token  string
	client *http.Client
}

var (
	tiltMu  sync.Mutex
	tiltCur *tiltConn
)

func tiltConfig() string { return hp(".tilt-dev", "config") }

func tiltConnect() (*tiltConn, error) {
	st, err := os.Stat(tiltConfig())
	if err != nil {
		return nil, err
	}
	tiltMu.Lock()
	defer tiltMu.Unlock()
	if tiltCur != nil && tiltCur.at.Equal(st.ModTime()) {
		return tiltCur, nil
	}
	b, err := os.ReadFile(tiltConfig())
	if err != nil {
		return nil, err
	}
	name := "tilt-default"
	if p := os.Getenv("TILT_PORT"); p != "" && p != "10350" {
		name = "tilt-" + p
	}
	server, ca, token := kubeconfig(string(b), name)
	if server == "" || token == "" {
		return nil, fmt.Errorf("tilt config: no %s", name)
	}
	pool := x509.NewCertPool()
	if pem, err := base64.StdEncoding.DecodeString(ca); err != nil || !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("tilt config: bad certificate")
	}
	tiltCur = &tiltConn{at: st.ModTime(), server: server, token: token, client: &http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}}
	return tiltCur, nil
}

// kubeconfig picks a cluster's server and certificate and its user's token
// out of a kubeconfig (the small subset Tilt writes).
func kubeconfig(s, name string) (server, ca, token string) {
	var section, cur string
	vals, loose := map[string]string{}, map[string]string{} // loose: an entry's fields before its name
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "-") && strings.HasSuffix(t, ":"):
			section = strings.TrimSuffix(t, ":")
			continue
		case strings.HasPrefix(l, "- "):
			cur = ""
			t = strings.TrimSpace(strings.TrimPrefix(t, "- "))
		}
		k, v, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case k == "name":
			cur = v
			for kk, vv := range loose {
				vals[section+"/"+cur+"/"+kk] = vv
			}
			loose = map[string]string{}
		case v == "": // a block (cluster:, user:) whose fields follow
		case cur != "":
			vals[section+"/"+cur+"/"+k] = v
		default:
			loose[k] = v
		}
	}
	return vals["clusters/"+name+"/server"], vals["clusters/"+name+"/certificate-authority-data"], vals["users/"+name+"/token"]
}

// tiltAPI lists Tilt's resources as `tilt get uiresources -o json` would; ok
// is false when there is no config to reach it with (the CLI is used then).
// A Tilt that isn't running answers nothing, at no cost.
func tiltAPI() ([]byte, bool) {
	c, err := tiltConnect()
	if err != nil {
		return nil, false
	}
	b, err := get(c.client, c.server+"/apis/tilt.dev/v1alpha1/uiresources", map[string]string{"Authorization": "Bearer " + c.token})
	if err != nil {
		return nil, true
	}
	return b, true
}
