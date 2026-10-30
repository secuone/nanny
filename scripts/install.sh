#!/usr/bin/env sh
# nanny installer: downloads a release binary, or builds from source with Go.
#   curl -fsSL https://raw.githubusercontent.com/secuone/nanny/main/scripts/install.sh | sh
set -eu

REPO="${NANNY_REPO:-secuone/nanny}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
mkdir -p "$BIN_DIR"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

latest=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1 || true)
if [ -n "${latest:-}" ]; then
  url="https://github.com/$REPO/releases/download/$latest/nanny_${latest#v}_${os}_${arch}.tar.gz"
  echo "→ downloading $url"
  if curl -fsSL "$url" -o "$tmp/s.tgz"; then
    tar -xzf "$tmp/s.tgz" -C "$tmp" nanny
    install -m755 "$tmp/nanny" "$BIN_DIR/nanny"
    echo "✓ installed $BIN_DIR/nanny ($latest)"
    exit 0
  fi
fi

if command -v go >/dev/null 2>&1; then
  echo "→ no release found, building from source with $(go version)"
  GOBIN="$BIN_DIR" go install "github.com/$REPO/cmd/nanny@latest"
  echo "✓ installed $BIN_DIR/nanny"
else
  echo "no release binary for $os/$arch and Go is not installed (https://go.dev/dl)" >&2
  exit 1
fi
