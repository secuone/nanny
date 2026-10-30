package sources

import "testing"

// The bits of a kubeconfig Tilt writes, wherever the entry's name comes.
func TestKubeconfig(t *testing.T) {
	cfg := `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Q0E=
    server: https://127.0.0.1:34751
  name: tilt-default
- cluster:
    server: https://127.0.0.1:40000
  name: tilt-10351
contexts:
- context:
    cluster: tilt-default
    user: tilt-default
  name: tilt-default
current-context: ""
kind: Config
users:
- name: tilt-default
  user:
    token: secret
`
	server, ca, token := kubeconfig(cfg, "tilt-default")
	if server != "https://127.0.0.1:34751" || ca != "Q0E=" || token != "secret" {
		t.Errorf("got %q %q %q", server, ca, token)
	}
	if s, _, tok := kubeconfig(cfg, "tilt-10351"); s != "https://127.0.0.1:40000" || tok != "" {
		t.Errorf("another Tilt: %q %q", s, tok)
	}
}

// Ports read like `docker ps` writes them.
func TestDockerPorts(t *testing.T) {
	got := dockerPorts([]dockerPort{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}, {PrivatePort: 5432, Type: "tcp"}, {IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}})
	if want := "0.0.0.0:8080->80/tcp, 5432/tcp, [::]:8080->80/tcp"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
	if firstPort(got) != "8080" {
		t.Errorf("first port of %q", got)
	}
}
