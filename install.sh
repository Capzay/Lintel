#!/bin/sh
# Build lintel from this checkout and install it for the current user.
# No sudo, no downloads beyond the Go toolchain you already have.
#
#   ./install.sh                     installs to ~/.local/bin
#   LINTEL_BIN_DIR=/some/dir ./install.sh
set -eu

cd "$(dirname "$0")"
bin_dir="${LINTEL_BIN_DIR:-$HOME/.local/bin}"

if ! command -v go >/dev/null 2>&1; then
	echo "Go is not installed. Get Go 1.22 or later from https://go.dev/dl/ and run this again." >&2
	exit 1
fi

echo "Building lintel with $(go version)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
CGO_ENABLED=0 go build -trimpath -o "$tmp/lintel" ./cmd/lintel

mkdir -p "$bin_dir"
install -m 0755 "$tmp/lintel" "$bin_dir/lintel"
if [ "$(uname -s)" = Darwin ]; then
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
