#!/bin/bash
# ------------------------------------------------------------------
# P2 determinism harness — prove ap-canonical hash stability (v1 or v2)
# off-chain BEFORE any transaction is signed.
#
# The cost of discovering non-determinism on-chain is a verifier's
# committed bond and a DISPUTED record; off-chain it is a rerun
# (phase P2 in test/federation/mastodon/README.md).
#
# Proves four things against a live Mastodon instance you control
# (see README.md in this directory for instance setup):
#
#   1. Stable across repeated fetches — same URI, N fetches spread
#      over several hours, identical hash every time.
#   2. Stable across vantage points — optional; set REMOTE_VANTAGE
#      to "user@host" and the script re-runs the fetch loop over SSH
#      (catches CDN/media-URL variance between networks).
#   3. Changes on every edit class — body edit, CW add/remove, media
#      add/remove each flip the hash (edit classes are driven by hand
#      or via the instance admin API between phases).
#   4. Survives an instance upgrade — run with --phase upgrade after
#      bumping the instance a minor version.
#
# Usage:
#   determinism_check.sh --phase baseline URIS...   # repeated fetches
#   determinism_check.sh --phase edits URIS...      # hash + diff vs baseline
#   determinism_check.sh --phase upgrade URIS...    # after instance upgrade
#
# Env:
#   APCANON          path to the apcanon CLI (default: go run from repo)
#   HASH_RULE        ap-canonical-v2 (default; also digests every
#                    attachment's file) or ap-canonical-v1
#   ALLOW_PRIVATE=1  let apcanon fetch a LOCAL instance (http://, loopback,
#                    private addresses). Bring-up only -- the guard exists
#                    because the daemons fetch attacker-influenced URIs.
#   FETCH_INTERVAL   seconds between fetches (default 1800 = 30 min)
#   FETCH_COUNT      number of fetches per phase (default 8 ≈ 4 hours)
#   REMOTE_VANTAGE   "user@host" — repeat each fetch over ssh elsewhere
#   KEY_ID / KEY     optional HTTP-Signature credentials (secure mode)
#   OUT_DIR          where run logs + fixtures land
#                    (default: <script dir>/runs/<timestamp>)
#
# Exit 0 = every observed hash agreed with the baseline for that URI
# (or, for --phase edits, changed as expected). Non-zero = STOP: the
# rule needs revision before any on-chain work.
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$( cd "$SCRIPT_DIR/../../.." && pwd )"

PHASE="baseline"
case "${1:-}" in
  --phase) PHASE="$2"; shift 2 ;;
  *) ;;
esac
(( $# >= 1 )) || { echo "usage: $0 [--phase baseline|edits|upgrade] URI [URI...]" >&2; exit 2; }
URIS=("$@")

FETCH_INTERVAL="${FETCH_INTERVAL:-1800}"
FETCH_COUNT="${FETCH_COUNT:-8}"
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/runs/$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$OUT_DIR/fixtures"

if [[ -n "${APCANON:-}" ]]; then
  APCANON_BIN="$APCANON"
else
  APCANON_BIN="go run $PROJECT_DIR/tools/apcanon/cmd/apcanon"
fi

# apcanon flags must precede the target: Go's flag package stops parsing at
# the first non-flag argument, so `hash <uri> --key-id ...` leaves the flags
# unparsed and exits on the usage check. This used to pass them last, which
# silently broke every signed fetch.
APCANON_FLAGS=(--rule "${HASH_RULE:-ap-canonical-v2}")
[[ -n "${KEY_ID:-}" && -n "${KEY:-}" ]] && APCANON_FLAGS+=(--key-id "$KEY_ID" --key "$KEY")
# ALLOW_PRIVATE=1 lets apcanon reach a LOCAL test instance (http:// and
# loopback/private addresses), which its SSRF guard refuses by default
# because the daemons fetch attacker-influenced URIs. Local bring-up only.
[[ -n "${ALLOW_PRIVATE:-}" ]] && APCANON_FLAGS+=(--allow-private)

hash_uri() {
  local uri="$1"; shift
  $APCANON_BIN hash "${APCANON_FLAGS[@]+"${APCANON_FLAGS[@]}"}" "$uri"
}

# rel <path>: how a path appears in run.log. Logs are committed as P2
# evidence, so they must not carry the machine's home directory: paths in
# the repo are shown repo-relative, anything else as <external>/<name>.
rel() {
  case "$1" in
    "$PROJECT_DIR"/*) printf '%s' "${1#"$PROJECT_DIR"/}" ;;
    *) printf '<external>/%s' "$(basename "$1")" ;;
  esac
}

log()  { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" | tee -a "$OUT_DIR/run.log"; }
fail() { log "FAIL: $*"; exit 1; }

slug() { # URI -> filesystem-safe tag
  printf '%s' "$1" | tr '/:' '__' | tail -c 80
}

# --- Phase: baseline / repeat ----------------------------------------------
if [[ "$PHASE" == "baseline" ]]; then
  log "phase=baseline interval=${FETCH_INTERVAL}s count=$FETCH_COUNT uris=${URIS[*]}"
  declare -A BASELINE
  for uri in "${URIS[@]}"; do
    BASELINE["$uri"]=""
  done
  for i in $(seq 1 "$FETCH_COUNT"); do
    for uri in "${URIS[@]}"; do
      h="$(hash_uri "$uri" || true)"
      [[ -n "$h" ]] || fail "fetch $uri returned nothing"
      if [[ -z "${BASELINE[$uri]}" ]]; then
        BASELINE[$uri]="$h"
        log "baseline $uri -> $h"
        # Save the fixture for P1's golden tests and the audit trail.
        curl -sSfL -H 'Accept: application/activity+json' "$uri" \
          -o "$OUT_DIR/fixtures/$(slug "$uri").as2.json" || true
      elif [[ "${BASELINE[$uri]}" != "$h" ]]; then
        fail "hash drift on $uri between fetches: ${BASELINE[$uri]} -> $h"
      else
        log "fetch $i/$FETCH_COUNT $uri stable ($h)"
      fi
      # Optional second vantage point over SSH. The remote host must
      # have the repo at the same commit — the comparison is only
      # meaningful against the same rule implementation.
      if [[ -n "${REMOTE_VANTAGE:-}" ]]; then
        rh="$(ssh -o BatchMode=yes "$REMOTE_VANTAGE" \
          "cd '$PROJECT_DIR' && go run ./tools/apcanon/cmd/apcanon hash --rule ${HASH_RULE:-ap-canonical-v2} ${ALLOW_PRIVATE:+--allow-private} '$uri'" 2>/dev/null || true)"
        if [[ -n "$rh" && "$rh" != "${BASELINE[$uri]}" ]]; then
          fail "vantage-point drift on $uri: ${BASELINE[$uri]} (local) vs $rh ($REMOTE_VANTAGE)"
        fi
        [[ -n "$rh" ]] && log "fetch $i/$FETCH_COUNT $uri stable from $REMOTE_VANTAGE"
      fi
    done
    (( i < FETCH_COUNT )) && sleep "$FETCH_INTERVAL"
  done
  # Persist baselines for the edits/upgrade phases.
  for uri in "${URIS[@]}"; do
    printf '%s %s\n' "${BASELINE[$uri]}" "$uri" >> "$OUT_DIR/baseline.txt"
  done
  log "baseline complete — $(rel "$OUT_DIR/baseline.txt")"

# --- Phase: edits -----------------------------------------------------------
elif [[ "$PHASE" == "edits" ]]; then
  # Run an edit class against the status between the two hashes; the
  # hash MUST change and `updated` MUST be populated afterward.
  BASELINE_FILE="${BASELINE_FILE:-$(ls -1t "$SCRIPT_DIR"/runs/*/baseline.txt 2>/dev/null | head -1 || true)}"
  [[ -n "$BASELINE_FILE" ]] || fail "no baseline.txt found; run --phase baseline first"
  log "phase=edits against $(rel "$BASELINE_FILE")"
  for uri in "${URIS[@]}"; do
    base="$(grep -F " $uri" "$BASELINE_FILE" | awk '{print $1}' | head -1)"
    [[ -n "$base" ]] || fail "no baseline hash for $uri"
    h="$(hash_uri "$uri")"
    if [[ "$h" == "$base" ]]; then
      fail "edit class did not change the hash of $uri ($h) — edit detection is broken for this class"
    fi
    # `updated` must now be populated (it is in the hashed set).
    canon="$($APCANON_BIN canon "${APCANON_FLAGS[@]+"${APCANON_FLAGS[@]}"}" "$uri")"
    if printf '%s' "$canon" | grep -q '"updated":null'; then
      fail "hash changed but updated is still null for $uri — the instance did not stamp the edit"
    fi
    log "edit detected on $uri: $base -> $h (updated populated)"
  done
  log "edits phase complete"

# --- Phase: upgrade ---------------------------------------------------------
elif [[ "$PHASE" == "upgrade" ]]; then
  BASELINE_FILE="${BASELINE_FILE:-$(ls -1t "$SCRIPT_DIR"/runs/*/baseline.txt 2>/dev/null | head -1 || true)}"
  [[ -n "$BASELINE_FILE" ]] || fail "no baseline.txt found; run --phase baseline first"
  log "phase=upgrade against $(rel "$BASELINE_FILE")"
  rc=0
  for uri in "${URIS[@]}"; do
    base="$(grep -F " $uri" "$BASELINE_FILE" | awk '{print $1}' | head -1)"
    [[ -n "$base" ]] || fail "no baseline hash for $uri"
    h="$(hash_uri "$uri")"
    if [[ "$h" != "$base" ]]; then
      log "WARN: $uri hash changed across the instance upgrade: $base -> $h"
      log "      this is not a bug in the rule, but it is a fact operators must"
      log "      know — record it and expect hash_rule versioning to earn its keep."
      rc=1
    else
      log "$uri stable across upgrade ($h)"
    fi
  done
  exit $rc

else
  echo "unknown phase '$PHASE' (baseline|edits|upgrade)" >&2
  exit 2
fi

log "PASS"
