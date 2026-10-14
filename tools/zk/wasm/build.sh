#!/usr/bin/env bash
# Builds the browser shield client (shield.wasm + Go's wasm_exec.js) into OUT.
# Web clients also need proving_key.raw.bin (uncompressed, so loading skips
# point decompression) and circuit.r1cs from the ceremony (tools/zk/cmd/ceremony)
# whose verifying key is on chain. Pass that ceremony directory as KEYS to copy
# them into OUT.
#
# The wasm is stamped with the chain release it matches (VERSION and COMMIT,
# resolved like the Makefile's: VERSION from the environment, else an exact
# tag, else <branch>-<commit>), which clients compare against the node's
# application_version before proving. OUT/manifest.json records that version
# and the sha256 of every file; loadKeys skips the proving key's point checks,
# so clients should pin these digests and check them before loading.
#
#   tools/zk/wasm/build.sh [OUT] [KEYS]    (default OUT: build/zk)
#   VERSION=v1.0.54 tools/zk/wasm/build.sh build/zk/v1.0.54 build/ceremony
set -euo pipefail
cd "$(dirname "$0")/../../.."
OUT="${1:-build/zk}"
KEYS="${2:-}"
COMMIT="${COMMIT:-$(git log -1 --format='%H')}"
VERSION="${VERSION:-$(git describe --exact-match 2>/dev/null || echo "$(git rev-parse --abbrev-ref HEAD)-$COMMIT")}"
mkdir -p "$OUT"
GOOS=js GOARCH=wasm go build -trimpath \
	-ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT" \
	-o "$OUT/shield.wasm" ./tools/zk/wasm
GOROOT="$(go env GOROOT)"
cp "$GOROOT/lib/wasm/wasm_exec.js" "$OUT/" 2>/dev/null || cp "$GOROOT/misc/wasm/wasm_exec.js" "$OUT/"
FILES=(shield.wasm wasm_exec.js)
if [ -n "$KEYS" ]; then
	cp "$KEYS/proving_key.raw.bin" "$KEYS/circuit.r1cs" "$OUT/"
	FILES+=(proving_key.raw.bin circuit.r1cs)
fi

{
	printf '{\n  "version": "%s",\n  "commit": "%s",\n  "files": {' "$VERSION" "$COMMIT"
	sep=""
	for f in "${FILES[@]}"; do
		printf '%s\n    "%s": "%s"' "$sep" "$f" "$(sha256sum "$OUT/$f" | cut -d' ' -f1)"
		sep=","
	done
	printf '\n  }\n}\n'
} >"$OUT/manifest.json"

echo "Built for $VERSION ($COMMIT). Pin these digests in the client:"
cat "$OUT/manifest.json"
ls -la "$OUT"
