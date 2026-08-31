#!/usr/bin/env sh
# Build the plugin binary, unless the release archive already shipped one.
#
# A release archive contains a binary per platform and Helm selects the right
# one via platformCommand, so there is nothing to do here. Only a source
# checkout reaches the build, and then Go is required.
set -eu

cd "$(dirname "$0")/.."

# Does this install already carry a binary for the running platform?
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) arch=$(uname -m) ;;
esac

if [ -x "bin/get-manifest-${os}-${arch}" ]; then
	echo "helm get-manifest: using prebuilt bin/get-manifest-${os}-${arch}"
	exit 0
fi

if ! command -v go >/dev/null 2>&1; then
	echo "helm get-manifest: no prebuilt binary for ${os}/${arch}, and Go is not installed" >&2
	echo "install Go (https://go.dev/dl/), or install a released version:" >&2
	echo "  helm plugin install https://github.com/xzhou-sc/helm-get-manifest --version vX.Y.Z" >&2
	exit 1
fi

mkdir -p bin
# Build to the platform-suffixed name, because plugin.yaml's platformCommand
# matches on os/arch before reaching its unsuffixed fallback: on any supported
# platform Helm looks for bin/get-manifest-${os}-${arch}, never bin/get-manifest.
out="bin/get-manifest-${os}-${arch}"
# Not `[ ... ] && out=...`: under `set -e` a false test would end the script.
if [ "$os" = windows ]; then
	out="${out}.exe"
fi
# -buildvcs=false because the plugin is often built from an extracted tarball,
# which has no .git directory for the toolchain to stamp from.
go build -trimpath -buildvcs=false -ldflags="-s -w" -o "$out" ./cmd/get-manifest
echo "helm get-manifest: built $out"
