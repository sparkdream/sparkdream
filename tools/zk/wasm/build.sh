#!/usr/bin/env bash
# Builds the browser shield client (shield.wasm + Go's wasm_exec.js) into OUT.
# Web clients also need proving_key.raw.bin (uncompressed, so loading skips
# point decompression) and circuit.r1cs from the ceremony (tools/zk/cmd/ceremony)
# whose verifying key is on chain. Pass that ceremony directory as KEYS to copy
# them into OUT and print their sha256: loadKeys skips the proving key's point
# checks, so clients should pin these digests and check them before loading.
#
#   tools/zk/wasm/build.sh [OUT] [KEYS]    (default OUT: build/zk)
set -euo pipefail
cd "$(dirname "$0")/../../.."
OUT="${1:-build/zk}"
KEYS="${2:-}"
mkdir -p "$OUT"
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o "$OUT/shield.wasm" ./tools/zk/wasm
GOROOT="$(go env GOROOT)"
cp "$GOROOT/lib/wasm/wasm_exec.js" "$OUT/" 2>/dev/null || cp "$GOROOT/misc/wasm/wasm_exec.js" "$OUT/"
if [ -n "$KEYS" ]; then
	cp "$KEYS/proving_key.raw.bin" "$KEYS/circuit.r1cs" "$OUT/"
	echo "Pin these digests in the client:"
	sha256sum "$OUT/proving_key.raw.bin" "$OUT/circuit.r1cs"
fi
ls -la "$OUT"
