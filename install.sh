#!/bin/sh
set -eu

repo="WellWells/agentswap"
dir="${AGENTSWAP_INSTALL_DIR:-$HOME/.local/bin}"
version="${AGENTSWAP_VERSION:-latest}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "unsupported OS: $(uname -s); use install.ps1 on Windows" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [ -n "${AGENTSWAP_DOWNLOAD_URL:-}" ]; then
  base="${AGENTSWAP_DOWNLOAD_URL%/}"
elif [ "$version" = latest ]; then
  base="https://github.com/$repo/releases/latest/download"
else
  base="https://github.com/$repo/releases/download/$version"
fi
asset="agentswap_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
expected="$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | cut -d' ' -f1)"
else
  actual="$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "checksum mismatch for $asset" >&2
  exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$dir"
install -m 0755 "$tmp/agentswap" "$dir/agentswap"
if [ "$os" = darwin ]; then
  xattr -d com.apple.quarantine "$dir/agentswap" 2>/dev/null || true
fi
"$dir/agentswap" link

echo "Installed agentswap to $dir"
case ":$PATH:" in
  *":$dir:"*) ;;
  *)
    case "${SHELL##*/}" in
      zsh) rc="$HOME/.zshrc" ;;
      bash) rc="$HOME/.bashrc" ;;
      *) rc="$HOME/.profile" ;;
    esac
    echo "Add $dir to your PATH:"
    echo "  echo 'export PATH=\"$dir:\$PATH\"' >> $rc && . $rc"
    ;;
esac
