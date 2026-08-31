#!/usr/bin/env sh
# Build the plugin binary at install time.
#
# Building from source keeps the plugin a single small repository with no
# release artifacts to publish or verify. Go is the only requirement.
set -eu

cd "$(dirname "$0")/.."

if ! command -v go >/dev/null 2>&1; then
	echo "helm get-manifest: Go is required to build this plugin" >&2
	echo "install Go (https://go.dev/dl/) and run: helm plugin update get-manifest" >&2
	exit 1
fi

mkdir -p bin
go build -trimpath -ldflags="-s -w" -o bin/get-manifest ./cmd/get-manifest
echo "helm get-manifest: built bin/get-manifest"
