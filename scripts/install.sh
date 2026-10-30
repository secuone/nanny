#!/usr/bin/env sh
# nanny installer: downloads a release binary, or builds from source with Go and git.
#   curl -fsSL https://raw.githubusercontent.com/secuone/nanny/main/scripts/install.sh | sh
# BIN_DIR (default ~/.local/bin) is where nanny goes; NANNY_REPO (default secuone/nanny) is the GitHub repo.
set -eu

REPO="${NANNY_REPO:-secuone/nanny}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
  linux) ;;
  *) echo "nanny runs on Linux only for now (found $os); see the Requirements section of the README" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch (nanny is built for amd64 and arm64)" >&2; exit 1 ;;
esac

mkdir -p "$BIN_DIR"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

done_msg() {
  echo "✓ installed $BIN_DIR/nanny ($1)"
  case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) echo "  add $BIN_DIR to your PATH to run it" ;;
  esac
}

latest=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1 || true)
if [ -n "${latest:-}" ]; then
  url="https://github.com/$REPO/releases/download/$latest/nanny_${latest#v}_${os}_${arch}.tar.gz"
  echo "→ downloading $url"
  if curl -fsSL "$url" -o "$tmp/s.tgz" && tar -xzf "$tmp/s.tgz" -C "$tmp" nanny; then
    install -m755 "$tmp/nanny" "$BIN_DIR/nanny"
    done_msg "$latest"
    exit 0
  fi
  echo "→ the $latest release has no $os/$arch binary" >&2
else
  echo "→ no release published yet"
fi

# `go install github.com/.../cmd/nanny@latest` can't be used: go.mod has a replace directive (third_party/vt)
for tool in git go; do
  command -v "$tool" >/dev/null 2>&1 || { echo "building from source needs $tool; install it (Go: https://go.dev/dl) or wait for a release" >&2; exit 1; }
done
echo "→ building from source with $(go version)"
git clone --quiet --depth 1 "https://github.com/$REPO.git" "$tmp/src"
rev=$(git -C "$tmp/src" rev-parse --short HEAD)
if ! (cd "$tmp/src" && go build -trimpath -ldflags "-s -w -X main.version=main-$rev" -o "$BIN_DIR/nanny" ./cmd/nanny); then
  echo "build failed: nanny needs Go 1.26 or newer (https://go.dev/dl)" >&2
  exit 1
fi
done_msg "main-$rev"
