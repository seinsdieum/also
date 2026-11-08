#!/bin/sh
# Install also and start the interactive setup.
#   curl -fsSL https://raw.githubusercontent.com/seinsdieum/also/main/install.sh | sh
# Optional: ALSO_VERSION=0.1.0 ALSO_INSTALL_DIR="$HOME/bin" sh install.sh
# Set ALSO_NO_SETUP=1 for unattended installs.
set -eu

REPO="seinsdieum/also"
INSTALL_DIR="${ALSO_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${ALSO_VERSION:-}"
BASE="${ALSO_BASE_URL:-https://github.com/$REPO}"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  BLUE="$(printf '\033[38;2;59;130;246m')"
  DIM="$(printf '\033[2m')"
  BOLD="$(printf '\033[1m')"
  RESET="$(printf '\033[0m')"
else
  BLUE=""; DIM=""; BOLD=""; RESET=""
fi

os="$(uname -s)"
case "$os" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW*|MSYS*|CYGWIN*) os=windows ;;
  *) echo "also: unsupported operating system: $os" >&2; exit 1 ;;
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

printf '\n  %s◆%s %salso%s %sinstaller%s\n\n' "$BLUE" "$RESET" "$BOLD" "$RESET" "$DIM" "$RESET"
echo "  downloading $asset${VERSION:+ v$VERSION}"
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

# shortcut: SHA256SUMS comes from the same release as the binary, so a
# compromised release can serve both. Verify the published build attestation
# when the GitHub CLI is installed and authenticated; upgrade to a signed
# SHA256SUMS kept outside the release when a signing key exists.
if [ "$BASE" = "https://github.com/$REPO" ]; then
  if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    if ! gh attestation verify "$tmp/$asset" -R "$REPO" >/dev/null 2>&1; then
      echo "also: build attestation verification failed; refusing to install" >&2
      exit 1
    fi
    printf '  ✓ attestation verified\n'
  else
    echo "  also: gh not authenticated; only the release checksum was verified" >&2
  fi
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/$asset" "$INSTALL_DIR/also$ext"
printf '  %s✓%s installed %s\n' "$BLUE" "$RESET" "$INSTALL_DIR/also$ext"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "  add $INSTALL_DIR to PATH, then restart your shell";;
esac

if [ "${ALSO_NO_SETUP:-0}" = "1" ]; then
  echo "  next: also setup"
elif [ -r /dev/tty ]; then
  echo
  "$INSTALL_DIR/also$ext" setup || echo "also: setup skipped; run: also setup"
else
  echo "  next: also setup"
fi
