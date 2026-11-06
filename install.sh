#!/bin/sh
# Install also from GitHub releases.
#   curl -fsSL https://raw.githubusercontent.com/seinsdieum/also/main/install.sh | sh
# Optional: ALSO_VERSION=1.0.0 ALSO_INSTALL_DIR="$HOME/bin" sh install.sh
set -eu

REPO="seinsdieum/also"
INSTALL_DIR="${ALSO_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${ALSO_VERSION:-}"
BASE="${ALSO_BASE_URL:-https://github.com/$REPO}"

os="$(uname -s)"
case "$os" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW*|MSYS*|CYGWIN*) os=windows ;;
  *) echo "also: unsupported OS: $os" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "also: unsupported architecture: $arch" >&2; exit 1 ;;
esac

ext=""
[ "$os" = windows ] && ext=".exe"
asset="also-${os}-${arch}${ext}"

if [ -n "$VERSION" ]; then
  version="${VERSION#v}"
  url="$BASE/releases/download/v${version}"
else
  url="$BASE/releases/latest/download"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "also: downloading $asset${VERSION:+ v$VERSION}"
curl -fsSL "$url/$asset" -o "$tmp/$asset"
curl -fsSL "$url/SHA256SUMS" -o "$tmp/SHA256SUMS"

expected="$(awk -v name="$asset" '$2 == name { print $1 }' "$tmp/SHA256SUMS")"
if [ -z "$expected" ]; then
  echo "also: SHA256SUMS has no entry for $asset" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
else
  echo "also: need sha256sum or shasum to verify the download" >&2
  exit 1
fi
if [ "$expected" != "$actual" ]; then
  echo "also: checksum mismatch for $asset" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/$asset" "$INSTALL_DIR/also$ext"
echo "also: installed to $INSTALL_DIR/also$ext"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "also: add $INSTALL_DIR to PATH, then restart your shell";;
esac
echo "also: next run: also setup"
