#!/bin/sh
# Installs the latest deck release: curl -fsSL https://raw.githubusercontent.com/kloppers-labs/deck/main/install.sh | sh
# DECK_VERSION=0.1.0 pins a release; DECK_INSTALL_DIR overrides ~/.local/bin.
set -eu

repo=kloppers-labs/deck
bindir=${DECK_INSTALL_DIR:-$HOME/.local/bin}

fail() {
	echo "deck: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

need curl
need tar
need uname

os=$(uname -s)
case $os in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) fail "unsupported OS $os (deck runs on macOS and Linux; on Windows use WSL)" ;;
esac

arch=$(uname -m)
case $arch in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "unsupported architecture $arch" ;;
esac

if [ -n "${DECK_VERSION:-}" ]; then
	tag=v${DECK_VERSION#v}
else
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		fail "could not find the latest release"
	tag=${latest##*/}
	case $tag in v*) ;; *) fail "no release published yet" ;; esac
fi

version=${tag#v}
archive="deck_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading deck $tag for $os/$arch"
curl -fsSL -o "$tmp/$archive" "$base/$archive" || fail "download failed: $base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "could not download checksums.txt"

expected=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$expected" ] || fail "$archive is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$archive" | awk '{ print $1 }')
else
	actual=$(shasum -a 256 "$tmp/$archive" | awk '{ print $1 }')
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" deck
mkdir -p "$bindir"
install -m 0755 "$tmp/deck" "$bindir/deck"

echo "Installed $("$bindir/deck" --version) to $bindir/deck"
case ":$PATH:" in
*":$bindir:"*) ;;
*) echo "Add $bindir to your PATH, e.g. echo 'export PATH=\"$bindir:\$PATH\"' >> ~/.zshrc" ;;
esac
