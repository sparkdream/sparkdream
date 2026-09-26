# Snapshot fingerprint helper, sourced by test/_auto_snapshot.sh, every
# module's run_all_tests.sh (--save-setup) and test/run_parallel.sh.
#
# A post-setup snapshot is chain state produced by three inputs, and it is
# only reusable while all three are unchanged:
#
#   setup   test/<module>/setup_test_accounts.sh  -- what setup does
#   binary  the sparkdreamd that ran genesis     -- genesis bootstrap code,
#                                                   InitGenesis, DefaultParams
#   config  <project>/config.yml                 -- genesis values ignite writes
#
# Hashing only the setup script let a snapshot outlive a code change: after a
# new committee permission was added to the genesis bootstrap, restoring the
# old snapshot silently brought back the old permission set. The generated
# genesis.json is not hashed -- every init writes fresh validator keys and a
# new genesis_time, so it never matches -- config.yml is its stable input.
#
# The fingerprint is stored as <snapshot>/fingerprint, one "<input> <sha256>"
# line per input. A snapshot without one (saved before this gate existed) is
# stale and refreshes once.
#
# Override the binary with SNAPSHOT_FINGERPRINT_BINARY (run_parallel.sh points
# it at the GOPATH binary it copies into each suite).

_SNAPSHOT_FP_PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

_snapshot_fp_sha() {
    if [ -f "$1" ]; then
        sha256sum "$1" 2>/dev/null | cut -d' ' -f1
    else
        echo "none"
    fi
}

_snapshot_fp_binary() {
    local b="${SNAPSHOT_FINGERPRINT_BINARY:-}"
    [ -n "$b" ] || b="$(command -v "${BINARY:-sparkdreamd}" 2>/dev/null)"
    [ -n "$b" ] || b="$(go env GOPATH 2>/dev/null)/bin/sparkdreamd"
    echo "$b"
}

# snapshot_fingerprint <setup_script>: prints the current fingerprint.
snapshot_fingerprint() {
    echo "setup $(_snapshot_fp_sha "$1")"
    echo "binary $(_snapshot_fp_sha "$(_snapshot_fp_binary)")"
    echo "config $(_snapshot_fp_sha "$_SNAPSHOT_FP_PROJECT_DIR/config.yml")"
}

# snapshot_write_fingerprint <snapshot_dir> <setup_script>: records the
# fingerprint of the inputs that produced the snapshot just saved.
snapshot_write_fingerprint() {
    [ -d "$1" ] || return 0
    snapshot_fingerprint "$2" > "$1/fingerprint"
}

# snapshot_stale_inputs <snapshot_dir> <setup_script>: prints the inputs that
# changed since the snapshot was saved (e.g. "binary config"), or "unrecorded"
# for a snapshot without a fingerprint. Prints nothing when the snapshot is
# fresh. Returns 0 when fresh, 1 when stale.
snapshot_stale_inputs() {
    local stored="$1/fingerprint"
    if [ ! -f "$stored" ]; then
        echo "unrecorded"
        return 1
    fi
    local changed=() input sha want
    while read -r input sha; do
        want=$(grep -m1 "^$input " "$stored" 2>/dev/null | cut -d' ' -f2)
        # an unreadable binary or config.yml ("none") never matches; a module
        # without a setup script matches a snapshot saved without one
        if [ "$sha" != "$want" ] || { [ "$sha" = "none" ] && [ "$input" != "setup" ]; }; then
            changed+=("$input")
        fi
    done < <(snapshot_fingerprint "$2")
    [ ${#changed[@]} -eq 0 ] && return 0
    echo "${changed[*]}"
    return 1
}
