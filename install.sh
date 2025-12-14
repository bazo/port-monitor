#!/bin/bash
set -e

# --- Configuration ---
# REPLACE THESE WITH YOUR REPOSITORY DETAILS IF NOT DETECTED
REPO_OWNER="bazo" # Placeholder, assuming local user, change if different
REPO_NAME="port-monitor"
BINARY_NAME="ports"
INSTALL_DIR="/usr/local/bin"

# --- Hardware Detection ---
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Darwin)
        if [ "$ARCH" == "arm64" ]; then
            ASSET_SUFFIX="darwin_arm64"
        else
            echo "Unsupported MacOS architecture: $ARCH (Only Silicon/ARM64 is supported by this build)"
            exit 1
        fi
        ;;
    Linux)
        if [ "$ARCH" == "x86_64" ]; then
            ASSET_SUFFIX="linux_amd64"
        elif [ "$ARCH" == "aarch64" ]; then
            ASSET_SUFFIX="linux_arm64"
        else
            echo "Unsupported Linux architecture: $ARCH"
            exit 1
        fi
        ;;
    MINGW*|MSYS*|CYGWIN*)
        ASSET_SUFFIX="windows_amd64.exe"
        BINARY_NAME="ports.exe"
        ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

echo "Detected Platform: $OS $ARCH"
echo "Asset Suffix: $ASSET_SUFFIX"

# --- Get Latest Release ---
echo "Fetching latest release info..."
LATEST_RELEASE_URL="https://api.github.com/repos/$REPO_OWNER/$REPO_NAME/releases/latest"
DOWNLOAD_URL=$(curl -s $LATEST_RELEASE_URL | grep "browser_download_url.*$ASSET_SUFFIX" | cut -d : -f 2,3 | tr -d \")

if [ -z "$DOWNLOAD_URL" ]; then
    echo "Error: Could not find a release asset for $ASSET_SUFFIX in $REPO_OWNER/$REPO_NAME"
    echo "Check if a release exists and if the asset names match the workflow pattern."
    exit 1
fi

echo "Downloading $DOWNLOAD_URL ..."
curl -L -o "$BINARY_NAME" "$DOWNLOAD_URL"

chmod +x "$BINARY_NAME"

echo "Installing to $INSTALL_DIR (requires sudo)..."
sudo mv "$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"

echo "Success! You can now run '$BINARY_NAME'"
