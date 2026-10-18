#!/bin/bash
set -euo pipefail

REPO="confighub/sdk"
# Each release attaches a copy of this script with its own tag filled in here, so
# the script always names a release whose binaries are attached. Run from a
# checkout, it installs the latest release.
VERSION="${VERSION:-latest}"

# --- Detect OS and ARCH ---
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

# Normalize arch naming
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

# Normalize OS naming
case "$OS" in
  darwin | linux) ;;  # supported
  *) echo "Unsupported OS: $OS" && exit 1 ;;
esac

# Never ask api.github.com for the latest release: unauthenticated it allows 60
# requests an hour per IP, and CI runners share their IPs. The download URL for
# the latest release is a redirect that is not counted against that limit.
if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/$REPO/releases/latest/download"
else
  BASE_URL="https://github.com/$REPO/releases/download/$VERSION"
fi

mkdir -p "$HOME/.confighub/bin"

echo "Installing ConfigHub CLI $VERSION ($OS/$ARCH)..."

echo "Downloading cub..."
curl -fsSL "$BASE_URL/cub-$OS-$ARCH" -o "$HOME/.confighub/bin/cub"
chmod +x "$HOME/.confighub/bin/cub"

echo ""
echo "Installation complete ($VERSION). Add cub to your PATH using your preferred method. E.g:"
echo "  sudo ln -sf $HOME/.confighub/bin/cub /usr/local/bin/cub"
echo "  ln -sf $HOME/.confighub/bin/cub \$HOME/bin/cub"
echo "  export PATH=\$HOME/.confighub/bin:\$PATH"
echo ""
echo "Then run 'cub --help' to get started"
