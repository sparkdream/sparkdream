#!/bin/bash
#
# sparkdream-archive-mirror.sh
#
# Keeps a complete, verified copy of a chain's block archives on a machine
# that is NOT part of the fleet, and regularly proves that copy can rebuild
# the chain from genesis.
#
# Why off the fleet: archives written by the node-side cron live on that
# node's volume. When a provider disappears and the node is redeployed, the
# files, the progress marker and the cron job go with it. This script needs
# nothing from any node except a public RPC endpoint: it runs the same
# block-archiver against that endpoint, so a redeployed, replaced or renamed
# node changes nothing here. Every node returns identical /block,
# /block_results and /commit data for a height, so any endpoint will do.
#
# The vault directory holds everything a rebuild needs:
#
#   blocks_<from>_to_<to>.jsonl.gz           complete ranges (immutable)
#   blocks_<from>_to_<to>.partial.jsonl.gz   the tail up to the last run
#   genesis.json                             fetched from the chain once
#   SHA256SUMS                               one line per complete range
#   status.json / rebuild-status.json        result of the last sync / rebuild
#   bin/sparkdreamd-<version>                binaries built for rebuild checks
#   logs/                                    one log per rebuild check
#   mirror.env                               configuration (see below)
#
# Commands:
#   sync      Fetch new blocks (complete ranges plus the tail), then verify.
#             Run from a timer, hourly by default.
#   verify    Check the vault only: checksums, gzip integrity, line counts,
#             block heights, no gaps from block 1. No network.
#   rebuild   Replay every archived block from genesis into a throwaway
#             home with app-hash validation, then compare the final app hash
#             with the live chain. Run from a timer, weekly by default.
#   status    Print the last sync and rebuild results.
#
# Usage:
#   archive-mirror.sh <command> [-c <mirror.env>]
#
# Configuration, from the environment or the env file (default:
# $VAULT_DIR/mirror.env, or the path given with -c):
#   VAULT_DIR        - The vault directory (required)
#   CHAIN_ID         - Chain id the endpoints must report (required)
#   RPC_URLS         - Space-separated RPC endpoints, tried in order (required)
#   NETWORK          - Build tag for the rebuild binary: testnet, mainnet,
#                      devnet (default: testnet)
#   SRC_REPO         - Chain repo clone holding the release tags; the rebuild
#                      check builds the running version from it with
#                      `git archive`, never touching the working tree
#                      (default: $HOME/cosmos/sparkdream/sparkdream)
#   SPARKDREAMD_BIN  - Use this binary for rebuild checks instead of building
#   TAIL             - "true" keeps a tail of the blocks after the last
#                      complete range, so at most one sync interval of blocks
#                      is ever unarchived (default: true)
#   REBUILD_MAX_LAG  - Fail a rebuild check when the vault is more than this
#                      many blocks behind the chain (default: 5000)
#   NTFY_URL         - ntfy topic URL to POST failures to (optional)
#   NTFY_TOKEN       - Bearer token for NTFY_URL (optional)
#   NOTIFY_SUCCESS   - "true" also posts successful rebuild checks (default: false)
#
# A chain reset keeps the chain id but starts a different chain. sync notices
# (the endpoint's block hash at the vault's last height no longer matches) and
# refuses to continue, so two chains are never mixed in one vault: move the
# old vault aside and start an empty one.
#
set -eo pipefail

# ---------------------------------------------------------------------------
# Arguments and configuration
# ---------------------------------------------------------------------------
usage() {
    sed -n '2,/^set -eo/{ /^#/s/^# \?//p }' "$0"
    exit "${1:-0}"
}

COMMAND=""
CONFIG_FILE=""
while [ $# -gt 0 ]; do
    case "$1" in
        sync|verify|rebuild|status) COMMAND="$1"; shift ;;
        -c) CONFIG_FILE="$2"; shift 2 ;;
        -h|--help) usage 0 ;;
        *) echo "ERROR: Unknown argument '$1' (try --help)." >&2; exit 1 ;;
    esac
done
[ -n "$COMMAND" ] || usage 1

if [ -z "$CONFIG_FILE" ] && [ -n "$VAULT_DIR" ] && [ -f "$VAULT_DIR/mirror.env" ]; then
    CONFIG_FILE="$VAULT_DIR/mirror.env"
fi
if [ -n "$CONFIG_FILE" ]; then
    [ -f "$CONFIG_FILE" ] || { echo "ERROR: Config file '$CONFIG_FILE' not found." >&2; exit 1; }
    # shellcheck disable=SC1090
    . "$CONFIG_FILE"
fi

NETWORK="${NETWORK:-testnet}"
SRC_REPO="${SRC_REPO:-$HOME/cosmos/sparkdream/sparkdream}"
TAIL="${TAIL:-true}"
REBUILD_MAX_LAG="${REBUILD_MAX_LAG:-5000}"
NOTIFY_SUCCESS="${NOTIFY_SUCCESS:-false}"

for var in VAULT_DIR CHAIN_ID RPC_URLS; do
    if [ -z "${!var}" ]; then
        echo "ERROR: $var is not set (environment or mirror.env)." >&2
        exit 1
    fi
done
[ -d "$VAULT_DIR" ] || { echo "ERROR: Vault directory '$VAULT_DIR' does not exist." >&2; exit 1; }
VAULT_DIR="$(cd "$VAULT_DIR" && pwd)"

for cmd in curl jq gzip sha256sum flock base64; do
    command -v "$cmd" >/dev/null 2>&1 || { echo "ERROR: '$cmd' is required but not installed." >&2; exit 1; }
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ARCHIVER="$SCRIPT_DIR/block-archiver"
[ -x "$ARCHIVER" ] || ARCHIVER="$SCRIPT_DIR/block-archiver.sh"

SUMS_FILE="$VAULT_DIR/SHA256SUMS"
STATUS_FILE="$VAULT_DIR/status.json"
REBUILD_STATUS_FILE="$VAULT_DIR/rebuild-status.json"
GENESIS_FILE="$VAULT_DIR/genesis.json"
LOG_DIR="$VAULT_DIR/logs"
BIN_DIR="$VAULT_DIR/bin"
WORK_ROOT="$VAULT_DIR/rebuild"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
now_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

rpc() {
    curl -s --connect-timeout 10 --max-time 30 --retry 2 --retry-delay 2 --retry-all-errors "$1"
}

notify() {
    # notify <title> <message> — best effort, never fails the run
    [ -n "$NTFY_URL" ] || return 0
    local auth=()
    [ -n "$NTFY_TOKEN" ] && auth=(-H "Authorization: Bearer $NTFY_TOKEN")
    curl -s --max-time 20 "${auth[@]}" -H "Title: $1" -d "$2" "$NTFY_URL" >/dev/null 2>&1 || true
}

fail() {
    # fail <what> <message> — record, notify, exit non-zero
    echo "ERROR: $2" >&2
    notify "$CHAIN_ID archive $1 FAILED" "$2"
    exit 1
}

# Complete ranges as "from to path" lines, sorted by starting height.
complete_ranges() {
    local f base from to
    for f in "$VAULT_DIR"/blocks_*_to_*.jsonl.gz; do
        [ -f "$f" ] || continue
        base="$(basename "$f")"
        case "$base" in *.partial.jsonl.gz) continue ;; esac
        from="${base#blocks_}"; from="${from%%_to_*}"
        to="${base##*_to_}"; to="${to%.jsonl.gz}"
        case "$from$to" in ''|*[!0-9]*) continue ;; esac
        echo "$from $to $f"
    done | sort -n
}

# Tails as "from to path" lines.
partial_ranges() {
    local f base from to
    for f in "$VAULT_DIR"/blocks_*_to_*.partial.jsonl.gz; do
        [ -f "$f" ] || continue
        base="$(basename "$f")"
        from="${base#blocks_}"; from="${from%%_to_*}"
        to="${base##*_to_}"; to="${to%.partial.jsonl.gz}"
        case "$from$to" in ''|*[!0-9]*) continue ;; esac
        echo "$from $to $f"
    done | sort -n
}

# check_content <from> <to> <path> — gzip integrity, one line per height,
# first and last heights as named. Prints the problem and returns 1 if not.
check_content() {
    local from="$1" to="$2" path="$3" lines first last
    if ! gzip -t "$path" 2>/dev/null; then
        echo "$(basename "$path"): gzip integrity check failed"; return 1
    fi
    lines=$(gzip -dc "$path" | wc -l)
    if [ "$lines" -ne $(( to - from + 1 )) ]; then
        echo "$(basename "$path"): $lines lines, expected $(( to - from + 1 ))"; return 1
    fi
    first=$(gzip -dc "$path" 2>/dev/null | head -n 1 | jq -r '.block.header.height' 2>/dev/null || true)
    last=$(gzip -dc "$path" | tail -n 1 | jq -r '.block.header.height' 2>/dev/null)
    if [ "$first" != "$from" ] || [ "$last" != "$to" ]; then
        echo "$(basename "$path"): holds blocks ${first:-?} to ${last:-?}"; return 1
    fi
    return 0
}

# Sets VAULT_COMPLETE_TO (last block covered without a gap from block 1) and
# VAULT_TIP (that, or the end of a tail continuing it). Returns 1 and prints
# every problem found when the vault is not sound.
verify_vault() {
    local problems=0 expected=1 from to path base sum recorded msg
    VAULT_COMPLETE_TO=0
    VAULT_TIP=0
    touch "$SUMS_FILE"

    while read -r from to path; do
        [ -n "$path" ] || continue
        base="$(basename "$path")"
        if [ "$from" -ne "$expected" ]; then
            echo "  GAP: blocks ${expected} to $(( from - 1 )) are missing before ${base}"
            problems=$(( problems + 1 ))
        fi
        expected=$(( to + 1 ))

        sum=$(sha256sum "$path" | cut -d' ' -f1)
        recorded=$(awk -v f="$base" '$2 == f { print $1 }' "$SUMS_FILE")
        if [ -n "$recorded" ]; then
            if [ "$recorded" != "$sum" ]; then
                echo "  CORRUPT: ${base} no longer matches SHA256SUMS"
                problems=$(( problems + 1 ))
            fi
        elif msg=$(check_content "$from" "$to" "$path"); then
            # First sight of a complete range: checked once, then pinned.
            echo "$sum  $base" >> "$SUMS_FILE"
            echo "  Recorded ${base}"
        else
            echo "  BAD: $msg"
            problems=$(( problems + 1 ))
        fi
    done < <(complete_ranges)
    [ "$problems" -eq 0 ] && VAULT_COMPLETE_TO=$(( expected - 1 ))

    while read -r recorded base; do
        [ -n "$base" ] || continue
        if [ ! -f "$VAULT_DIR/$base" ]; then
            echo "  MISSING: ${base} is in SHA256SUMS but not in the vault"
            problems=$(( problems + 1 ))
        fi
    done < "$SUMS_FILE"

    VAULT_TIP=$VAULT_COMPLETE_TO
    while read -r from to path; do
        [ -n "$path" ] || continue
        if ! msg=$(check_content "$from" "$to" "$path"); then
            echo "  BAD: $msg"
            problems=$(( problems + 1 ))
        elif [ "$from" -le $(( VAULT_TIP + 1 )) ] && [ "$to" -gt "$VAULT_TIP" ]; then
            VAULT_TIP=$to
        fi
    done < <(partial_ranges)

    [ "$problems" -eq 0 ]
}

# Runs verify_vault in this shell (it sets variables, so no command
# substitution) and leaves what it printed in VERIFY_OUTPUT.
run_verify() {
    local out="$VAULT_DIR/.verify.out" rc=0
    verify_vault > "$out" 2>&1 || rc=1
    VERIFY_OUTPUT=$(cat "$out")
    rm -f "$out"
    return $rc
}

# First few real problems from the last run_verify, on one line.
verify_summary() {
    echo "$VERIFY_OUTPUT" | grep -v '^  Recorded' | head -n 3 | tr '\n' ' '
}

# Block hash the vault holds for its last complete block (empty if none).
vault_last_hash() {
    local last
    last=$(complete_ranges | tail -n 1)
    [ -n "$last" ] || return 0
    gzip -dc "${last##* }" | tail -n 1 | jq -r '.block_id.hash'
}

# pick_rpc <height> <hash> — the first endpoint that is on CHAIN_ID, in sync,
# still holds block <height>+1, and (when <hash> is given) reports that hash
# for block <height>. Sets RPC_URL, CHAIN_HEIGHT and APP_VERSION. An endpoint
# on a different chain under the same id sets RPC_FOREIGN.
pick_rpc() {
    local height="$1" hash="$2" url status net earliest catching their
    RPC_URL=""; RPC_FOREIGN=""
    for url in $RPC_URLS; do
        url="${url%/}"
        status=$(rpc "$url/status" || true)
        net=$(echo "$status" | jq -r '.result.node_info.network // empty' 2>/dev/null || true)
        if [ "$net" != "$CHAIN_ID" ]; then
            echo "  skip $url: reports chain '${net:-none}'"; continue
        fi
        catching=$(echo "$status" | jq -r '.result.sync_info.catching_up')
        earliest=$(echo "$status" | jq -r '.result.sync_info.earliest_block_height')
        if [ "$catching" != "false" ]; then
            echo "  skip $url: still catching up"; continue
        fi
        if [ "$earliest" -gt $(( height + 1 )) ]; then
            echo "  skip $url: history starts at block $earliest, block $(( height + 1 )) is needed"; continue
        fi
        if [ -n "$hash" ] && [ "$earliest" -le "$height" ]; then
            their=$(rpc "$url/block?height=$height" | jq -r '.result.block_id.hash // empty' 2>/dev/null || true)
            if [ "$their" != "$hash" ]; then
                echo "  skip $url: block $height is ${their:-unavailable}, the vault holds $hash"
                [ -n "$their" ] && RPC_FOREIGN="$url"
                continue
            fi
        fi
        RPC_URL="$url"
        CHAIN_HEIGHT=$(echo "$status" | jq -r '.result.sync_info.latest_block_height')
        APP_VERSION=$(rpc "$url/abci_info" | jq -r '.result.response.version // empty' 2>/dev/null || true)
        return 0
    done
    return 1
}

fetch_genesis() {
    local total chunk=0 tmp="$GENESIS_FILE.tmp" data
    : > "$tmp"
    total=$(rpc "$RPC_URL/genesis_chunked?chunk=0" | jq -r '.result.total // empty')
    [ -n "$total" ] || { rm -f "$tmp"; return 1; }
    while [ "$chunk" -lt "$total" ]; do
        data=$(rpc "$RPC_URL/genesis_chunked?chunk=$chunk" | jq -r '.result.data // empty')
        [ -n "$data" ] || { rm -f "$tmp"; return 1; }
        echo "$data" | base64 -d >> "$tmp"
        chunk=$(( chunk + 1 ))
    done
    if [ "$(jq -r '.chain_id' "$tmp" 2>/dev/null)" != "$CHAIN_ID" ]; then
        rm -f "$tmp"; return 1
    fi
    mv "$tmp" "$GENESIS_FILE"
}

write_json() {
    # write_json <file> <jq args...> — atomic
    local file="$1"; shift
    jq -n "$@" > "$file.tmp" && mv "$file.tmp" "$file"
}

# ---------------------------------------------------------------------------
# sync
# ---------------------------------------------------------------------------
cmd_sync() {
    exec 9> "$VAULT_DIR/.mirror.lock"
    flock -n 9 || { echo "Another sync is running. Exiting."; exit 0; }

    local started hash archiver_ok=true vault_ok=true lag
    started=$(now_utc)
    echo "[$started] sync $CHAIN_ID -> $VAULT_DIR"

    # The archiver resumes from its state file; rebuild it from the files if
    # it is missing so a restored or hand-filled vault continues in place.
    local last_line last_to=0
    last_line=$(complete_ranges | tail -n 1)
    if [ -n "$last_line" ]; then
        last_to=$(echo "$last_line" | cut -d' ' -f2)
    fi
    if [ ! -f "$VAULT_DIR/.last_archived_height" ] && [ "$last_to" -gt 0 ]; then
        echo "$last_to" > "$VAULT_DIR/.last_archived_height"
        echo "  Seeded the archiver's progress marker at block $last_to."
    fi
    last_to=$(cat "$VAULT_DIR/.last_archived_height" 2>/dev/null || echo 0)

    hash=$(vault_last_hash)
    if ! pick_rpc "$last_to" "$hash"; then
        if [ -n "$RPC_FOREIGN" ]; then
            fail sync "$RPC_FOREIGN serves a different chain than this vault (block $last_to differs). The network was probably reset: move this vault aside and start an empty one."
        fi
        fail sync "No usable RPC endpoint among: $RPC_URLS"
    fi
    echo "  Using $RPC_URL (height $CHAIN_HEIGHT, app ${APP_VERSION:-unknown})"

    if [ ! -f "$GENESIS_FILE" ]; then
        fetch_genesis || fail sync "Could not fetch genesis from $RPC_URL"
        echo "  Saved genesis.json ($(sha256sum "$GENESIS_FILE" | cut -c1-16)...)"
    fi

    local finalize=()
    [ "$TAIL" = "true" ] && finalize=(--finalize)
    if ! RPC_URL="$RPC_URL" OUTPUT_DIR="$VAULT_DIR" "$ARCHIVER" "${finalize[@]}" > "$VAULT_DIR/.last-archiver.log" 2>&1; then
        archiver_ok=false
        echo "  Archiver failed:"
        tail -n 5 "$VAULT_DIR/.last-archiver.log" | sed 's/^/    /'
    fi

    echo "  Verifying the vault..."
    run_verify || vault_ok=false
    [ -n "$VERIFY_OUTPUT" ] && echo "$VERIFY_OUTPUT"

    lag=$(( CHAIN_HEIGHT - VAULT_TIP ))
    write_json "$STATUS_FILE" \
        --arg at "$(now_utc)" --arg started "$started" --arg chain "$CHAIN_ID" \
        --arg rpc "$RPC_URL" --arg app "$APP_VERSION" \
        --argjson height "$CHAIN_HEIGHT" --argjson complete "$VAULT_COMPLETE_TO" \
        --argjson tip "$VAULT_TIP" --argjson lag "$lag" \
        --argjson archiver_ok "$archiver_ok" --argjson vault_ok "$vault_ok" \
        --arg genesis "$(sha256sum "$GENESIS_FILE" | cut -d' ' -f1)" \
        '{finished_at: $at, started_at: $started, chain_id: $chain, rpc: $rpc,
          app_version: $app, chain_height: $height, complete_to: $complete,
          tip: $tip, lag_blocks: $lag, archiver_ok: $archiver_ok,
          vault_ok: $vault_ok, genesis_sha256: $genesis}'

    [ "$vault_ok" = true ] || fail sync "Vault check failed: $(verify_summary)"
    [ "$archiver_ok" = true ] || fail sync "Archiver failed against $RPC_URL: $(tail -n 2 "$VAULT_DIR/.last-archiver.log" | tr '\n' ' ')"
    echo "  OK: complete to block $VAULT_COMPLETE_TO, tail to $VAULT_TIP, chain at $CHAIN_HEIGHT."
}

# ---------------------------------------------------------------------------
# verify
# ---------------------------------------------------------------------------
cmd_verify() {
    echo "Verifying $VAULT_DIR ..."
    if run_verify; then
        [ -n "$VERIFY_OUTPUT" ] && echo "$VERIFY_OUTPUT"
        echo "OK: complete from block 1 to $VAULT_COMPLETE_TO, tail to $VAULT_TIP."
    else
        echo "$VERIFY_OUTPUT"
        fail verify "Vault check failed: $(verify_summary)"
    fi
}

# ---------------------------------------------------------------------------
# rebuild
# ---------------------------------------------------------------------------

# Removes a rebuild work directory. Refuses anything outside WORK_ROOT.
remove_work_dir() {
    local dir="$1"
    case "$dir" in
        "$WORK_ROOT"/run-*) [ -d "$dir" ] && find "$dir" -depth -delete ;;
        *) echo "WARNING: refusing to remove '$dir'" >&2 ;;
    esac
}

# Sets BINARY to a sparkdreamd of the version the chain runs, building it
# from the release tag in SRC_REPO when the vault does not hold it yet.
ensure_binary() {
    local version="$1" build commit
    if [ -n "$SPARKDREAMD_BIN" ]; then
        BINARY="$SPARKDREAMD_BIN"
        [ -x "$BINARY" ] || fail rebuild "SPARKDREAMD_BIN '$BINARY' is not executable"
        return 0
    fi
    [ -n "$version" ] || fail rebuild "The chain's app version is unknown (no endpoint reachable and no earlier sync status)"
    BINARY="$BIN_DIR/sparkdreamd-$version"
    [ -x "$BINARY" ] && return 0

    command -v go >/dev/null 2>&1 || fail rebuild "No binary for $version in $BIN_DIR and 'go' is not installed to build one"
    commit=$(git -C "$SRC_REPO" rev-parse -q --verify "refs/tags/$version^{commit}" 2>/dev/null) \
        || fail rebuild "Tag $version is not in $SRC_REPO: fetch the release tags there, or put the binary at $BINARY"
    echo "  Building sparkdreamd $version ($NETWORK) from $SRC_REPO ..."
    mkdir -p "$BIN_DIR" "$WORK_ROOT"
    build="$WORK_ROOT/run-build-$$"
    mkdir "$build"
    git -C "$SRC_REPO" archive "$commit" | tar -x -C "$build"
    if ! ( cd "$build" && CGO_ENABLED=0 go build -mod=readonly -tags "$NETWORK" \
            -ldflags "-X github.com/cosmos/cosmos-sdk/version.Name=sparkdream -X github.com/cosmos/cosmos-sdk/version.AppName=sparkdreamd -X github.com/cosmos/cosmos-sdk/version.Version=$version -X github.com/cosmos/cosmos-sdk/version.Commit=$commit -X github.com/cosmos/cosmos-sdk/version.BuildTags=$NETWORK" \
            -o "$BINARY.tmp" ./cmd/sparkdreamd/main.go ) > "$LOG_DIR/build-$version.log" 2>&1; then
        remove_work_dir "$build"
        fail rebuild "Building sparkdreamd $version failed, see $LOG_DIR/build-$version.log"
    fi
    remove_work_dir "$build"
    mv "$BINARY.tmp" "$BINARY"
    echo "  Built $BINARY"
}

cmd_rebuild() {
    exec 8> "$VAULT_DIR/.rebuild.lock"
    flock -n 8 || { echo "Another rebuild check is running. Exiting."; exit 0; }
    mkdir -p "$LOG_DIR" "$WORK_ROOT"

    local started stamp run home snapshot log version expected
    local end_height final_hash elapsed chain_hash network_check="skipped" lag="null" t0
    started=$(now_utc)
    stamp=$(date -u +%Y%m%dT%H%M%SZ)
    log="$LOG_DIR/rebuild-$stamp.log"
    echo "[$started] rebuild check $CHAIN_ID from $VAULT_DIR"

    [ -f "$GENESIS_FILE" ] || fail rebuild "No genesis.json in the vault (run sync first)"

    # Hold the sync lock while checking and snapshotting, so the file set
    # replayed is exactly the one verified. Hard links keep the snapshot
    # stable when a later sync replaces the tail.
    run="$WORK_ROOT/run-$stamp"
    home="$run/home"
    snapshot="$run/archives"
    mkdir -p "$home" "$snapshot"
    exec 9> "$VAULT_DIR/.mirror.lock"
    flock -w 7200 9 || fail rebuild "Timed out waiting for a running sync"
    if ! run_verify; then
        remove_work_dir "$run"
        fail rebuild "Vault check failed: $(verify_summary)"
    fi
    expected=$VAULT_TIP
    [ "$expected" -gt 0 ] || { remove_work_dir "$run"; fail rebuild "The vault holds no blocks"; }
    { complete_ranges; partial_ranges; } | while read -r _ _ path; do ln "$path" "$snapshot/"; done
    flock -u 9

    RPC_URL=""; CHAIN_HEIGHT=""; APP_VERSION=""
    pick_rpc "$VAULT_COMPLETE_TO" "$(vault_last_hash)" || echo "  No endpoint reachable: rebuilding without the network comparison."
    version="$APP_VERSION"
    [ -n "$version" ] || version=$(jq -r '.app_version // empty' "$STATUS_FILE" 2>/dev/null || true)
    ensure_binary "$version"

    if [ -n "$CHAIN_HEIGHT" ]; then
        lag=$(( CHAIN_HEIGHT - expected ))
    fi

    echo "  Replaying blocks 1 to $expected with $("$BINARY" version 2>&1 | head -n 1) ..."
    if ! "$BINARY" init rebuild-check --chain-id "$CHAIN_ID" --home "$home" > "$log" 2>&1; then
        remove_work_dir "$run"
        fail rebuild "sparkdreamd init failed, see $log"
    fi
    cp "$GENESIS_FILE" "$home/config/genesis.json"

    # The replay command logs every store write at debug level (gigabytes
    # over a full chain) and has no level switch, so only the other lines are
    # kept. Its exit status is read from the pipeline, not from grep.
    local replay_rc=0
    t0=$(date +%s)
    set +o pipefail
    "$BINARY" replay-from-archive --home "$home" --archive-dir "$snapshot" --validate 2>&1 \
        | { grep -a -v ' DBG ' || true; } >> "$log"
    replay_rc=${PIPESTATUS[0]}
    set -o pipefail
    if [ "$replay_rc" -ne 0 ]; then
        write_json "$REBUILD_STATUS_FILE" --arg at "$(now_utc)" --arg started "$started" \
            --arg version "$version" --arg log "$log" --arg err "$(tail -n 1 "$log")" \
            --argjson expected "$expected" \
            '{finished_at: $at, started_at: $started, ok: false, app_version: $version,
              expected_height: $expected, error: $err, log: $log}'
        fail rebuild "Replay failed (work dir kept at $run): $(tail -n 1 "$log" | cut -c1-300)"
    fi
    elapsed=$(( $(date +%s) - t0 ))

    end_height=$(grep -a 'Replay complete' "$log" | tail -n 1 | sed -n 's/.*end_height=\([0-9]*\).*/\1/p')
    final_hash=$(grep -a 'Replay complete' "$log" | tail -n 1 | sed -n 's/.*final_app_hash=\([0-9A-Fa-f]*\).*/\1/p')

    local ok=true error=""
    if [ "$end_height" != "$expected" ]; then
        ok=false; error="Replay ended at block ${end_height:-?}, the vault holds blocks to $expected"
    fi

    # The app hash after block N is recorded in the header of block N+1, so
    # the live chain can confirm the rebuilt state bit for bit.
    if [ "$ok" = true ] && [ -n "$RPC_URL" ]; then
        chain_hash=$(rpc "$RPC_URL/block?height=$(( expected + 1 ))" | jq -r '.result.block.header.app_hash // empty' 2>/dev/null || true)
        if [ -z "$chain_hash" ]; then
            network_check="unavailable"
        elif [ "$chain_hash" = "$final_hash" ]; then
            network_check="match"
        else
            network_check="MISMATCH"
            ok=false; error="Rebuilt app hash $final_hash differs from the chain's $chain_hash after block $expected"
        fi
    fi
    if [ "$ok" = true ] && [ "$lag" != "null" ] && [ "$lag" -gt "$REBUILD_MAX_LAG" ]; then
        ok=false; error="Rebuild succeeded, but the vault is $lag blocks behind the chain (limit $REBUILD_MAX_LAG): sync is not keeping up"
    fi

    write_json "$REBUILD_STATUS_FILE" --arg at "$(now_utc)" --arg started "$started" \
        --arg version "$version" --arg log "$log" --arg err "$error" \
        --arg hash "$final_hash" --arg network "$network_check" \
        --arg genesis "$(sha256sum "$GENESIS_FILE" | cut -d' ' -f1)" \
        --argjson ok "$ok" --argjson expected "$expected" \
        --argjson end "${end_height:-0}" --argjson elapsed "$elapsed" --argjson lag "$lag" \
        '{finished_at: $at, started_at: $started, ok: $ok, app_version: $version,
          expected_height: $expected, rebuilt_height: $end, final_app_hash: $hash,
          network_check: $network, lag_blocks: $lag, replay_seconds: $elapsed,
          genesis_sha256: $genesis, error: $err, log: $log}'

    if [ "$ok" != true ]; then
        fail rebuild "$error (work dir kept at $run)"
    fi
    remove_work_dir "$run"

    # Keep the ten most recent rebuild logs, compressed.
    gzip -f "$log" && log="$log.gz"
    jq --arg log "$log" '.log = $log' "$REBUILD_STATUS_FILE" > "$REBUILD_STATUS_FILE.tmp" \
        && mv "$REBUILD_STATUS_FILE.tmp" "$REBUILD_STATUS_FILE"
    ls -1t "$LOG_DIR"/rebuild-*.log* 2>/dev/null | tail -n +11 | while read -r old; do rm -f "$old"; done

    echo "  OK: rebuilt blocks 1 to $end_height in ${elapsed}s, app hash $final_hash (network check: $network_check)."
    if [ "$NOTIFY_SUCCESS" = "true" ]; then
        notify "$CHAIN_ID rebuild check passed" "Blocks 1 to $end_height replayed in ${elapsed}s; network check: $network_check."
    fi
}

# ---------------------------------------------------------------------------
# status
# ---------------------------------------------------------------------------
cmd_status() {
    echo "Vault: $VAULT_DIR ($CHAIN_ID)"
    echo ""
    echo "Last sync:"
    if [ -f "$STATUS_FILE" ]; then jq . "$STATUS_FILE"; else echo "  (never)"; fi
    echo ""
    echo "Last rebuild check:"
    if [ -f "$REBUILD_STATUS_FILE" ]; then jq . "$REBUILD_STATUS_FILE"; else echo "  (never)"; fi
}

case "$COMMAND" in
    sync) cmd_sync ;;
    verify) cmd_verify ;;
    rebuild) cmd_rebuild ;;
    status) cmd_status ;;
esac
