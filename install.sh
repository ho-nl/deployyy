#!/bin/sh
# Installs the newest release of the deployyy CLI in ~/.local/bin.
set -eu

case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "No deployyy release for $(uname -m)" >&2 && exit 1 ;;
esac
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin | linux) ;;
  *) echo "No deployyy release for $os" >&2 && exit 1 ;;
esac

name="deployyy-$os-$arch"
release="https://github.com/ho-nl/deployyy/releases/latest/download"
bin="$HOME/.local/bin"
download=$(mktemp -d)
trap 'rm -rf "$download"' EXIT

curl -fsSL -o "$download/$name" "$release/$name"
curl -fsSL -o "$download/SHA256SUMS" "$release/SHA256SUMS"
if command -v sha256sum >/dev/null; then
  sum=$(sha256sum "$download/$name")
else
  sum=$(shasum -a 256 "$download/$name")
fi
if ! grep -q "^${sum%% *}  $name\$" "$download/SHA256SUMS"; then
  echo "The checksum of $name is wrong" >&2
  exit 1
fi

mkdir -p "$bin"
chmod 755 "$download/$name"
mv "$download/$name" "$bin/deployyy"
echo "Installed $("$bin/deployyy" --version) in $bin"
case ":$PATH:" in
  *":$bin:"*) ;;
  *) echo "Add $bin to PATH: export PATH=\"$bin:\$PATH\"" ;;
esac
