#!/bin/sh
# Install the latest lintel release for the current user.
# No sudo. Downloads one binary from GitHub Releases and checks its SHA-256.
#
#   curl -fsSL https://raw.githubusercontent.com/Capzay/Lintel/main/install.sh | sh
#
# LINTEL_BIN_DIR picks the install folder (default ~/.local/bin).
# LINTEL_VERSION picks a release tag, such as v0.1.0 (default: latest).
set -eu

repo="Capzay/Lintel"
bin_dir="${LINTEL_BIN_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) echo "Unsupported OS $(uname -s). On Windows, use install.ps1." >&2; exit 1 ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) echo "Unsupported CPU $(uname -m)." >&2; exit 1 ;;
esac

if [ -n "${LINTEL_VERSION:-}" ]; then
	base="https://github.com/$repo/releases/download/$LINTEL_VERSION"
else
	base="https://github.com/$repo/releases/latest/download"
fi
asset="lintel-$os-$arch"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "Downloading $asset"
curl -fsSL -o "$tmp/$asset" "$base/$asset"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"

expected="$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/SHA256SUMS")"
if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$tmp/$asset" | awk '{ print $1 }')"
else
	actual="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
	echo "Checksum mismatch for $asset. Nothing was installed." >&2
	exit 1
fi

mkdir -p "$bin_dir"
install -m 0755 "$tmp/$asset" "$bin_dir/lintel"
if [ "$os" = darwin ]; then
	xattr -d com.apple.quarantine "$bin_dir/lintel" 2>/dev/null || true
fi
echo "Installed $bin_dir/lintel"

case ":$PATH:" in
*":$bin_dir:"*) ;;
*)
	echo
	echo "$bin_dir is not on your PATH. Add this line to your shell profile, then open a new terminal:"
	echo "  export PATH=\"$bin_dir:\$PATH\""
	;;
esac

echo
echo "Next: run 'lintel login' to store your TypeSafe key, then 'lintel init' in a project."
