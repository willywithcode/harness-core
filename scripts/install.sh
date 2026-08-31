#!/usr/bin/env bash
# Downloads the latest mustang release for this platform, verifies its
# SHA-256 checksum, and installs it. Delegates all product behavior (init,
# status, update) to the installed binary -- this script only ever fetches
# and verifies bytes.
set -euo pipefail

REPO="willywithcode/harness-core"
API="https://api.github.com/repos/${REPO}/releases/latest"
INSTALL_DIR="${MUSTANG_INSTALL_DIR:-$HOME/.local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  darwin|linux) ;;
  *) echo "error: unsupported OS: $os" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "error: unsupported architecture: $arch" >&2; exit 1 ;;
esac

asset="mustang-${os}-${arch}"

echo "Resolving latest release of ${REPO}..." >&2
release_json="$(curl -fsSL "$API")"

# Minimal, dependency-free JSON field extraction: this script intentionally
# avoids requiring jq. It only needs one field (browser_download_url) for a
# name we already know, so a targeted grep/sed pass is enough; a full JSON
# parser would be overkill for a bootstrap script this small.
download_url="$(printf '%s\n' "$release_json" \
  | grep -o "\"browser_download_url\"[[:space:]]*:[[:space:]]*\"[^\"]*/${asset}\"" \
  | head -n1 \
  | sed -E 's/.*"(https:[^"]+)"/\1/')"

if [ -z "$download_url" ]; then
  echo "error: no release asset named '${asset}' was found. Is a release published yet for this platform?" >&2
  exit 1
fi
checksum_url="${download_url}.sha256"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${asset}..." >&2
curl -fsSL -o "${tmp}/${asset}" "$download_url"
curl -fsSL -o "${tmp}/${asset}.sha256" "$checksum_url"

expected="$(awk '{print $1}' "${tmp}/${asset}.sha256")"
if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "${tmp}/${asset}" | awk '{print $1}')"
else
  actual="$(sha256sum "${tmp}/${asset}" | awk '{print $1}')"
fi

if [ "$expected" != "$actual" ]; then
  echo "error: checksum mismatch for ${asset}: expected ${expected}, got ${actual}" >&2
  exit 1
fi

chmod +x "${tmp}/${asset}"
mkdir -p "$INSTALL_DIR"
mv "${tmp}/${asset}" "${INSTALL_DIR}/mustang"

echo "Installed mustang to ${INSTALL_DIR}/mustang" >&2
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "warning: ${INSTALL_DIR} is not on your PATH. Add it, then run: mustang init" >&2 ;;
esac
