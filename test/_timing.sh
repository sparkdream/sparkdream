# ============================================================================
# Shared timing helpers for E2E test runners.
#
# Source from any runner that wants to surface "started at X, ended at Y,
# took Z" information in its summary. Helpers are namespaced with `timing_*`
# to avoid collisions with runners' existing variables.
#
# Output is ASCII-only (no emojis) so summaries render correctly in CI logs,
# basic terminals, and SSH sessions.
#
# Usage:
#   source "$SCRIPT_DIR/../_timing.sh"          # from a module runner
#   source "$SCRIPT_DIR/_timing.sh"             # from test/run_all_tests.sh
#
#   suite_start_epoch=$(timing_now_epoch)
#   suite_start_human=$(timing_now_human)
#   ...
#   suite_end_epoch=$(timing_now_epoch)
#   suite_end_human=$(timing_now_human)
#   echo "  Started:  $suite_start_human"
#   echo "  Ended:    $suite_end_human"
#   echo "  Duration: $(timing_format_duration $((suite_end_epoch - suite_start_epoch)))"
#
# Per-test timings can be tracked via parallel arrays and printed with
# timing_print_per_test_table:
#
#   declare -a TEST_NAMES=()
#   declare -a TEST_RESULTS=()
#   declare -a TEST_DURATIONS_S=()
#
#   t0=$(timing_now_epoch); bash some_test.sh; rc=$?; t1=$(timing_now_epoch)
#   TEST_NAMES+=("Some Test")
#   TEST_RESULTS+=("$([ $rc -eq 0 ] && echo PASS || echo FAIL)")
#   TEST_DURATIONS_S+=($((t1 - t0)))
#
#   timing_print_per_test_table TEST_RESULTS TEST_DURATIONS_S TEST_NAMES
# ============================================================================

# Guard against double-sourcing (e.g. when both a master runner and a
# nested per-module runner source this file in the same shell).
[ -n "${_TIMING_SH_LOADED:-}" ] && return 0
_TIMING_SH_LOADED=1

# Epoch seconds (integer). Suitable for arithmetic.
timing_now_epoch() {
    date +%s
}

# Human-readable wall-clock timestamp, e.g. "2026-05-07 14:32:11".
timing_now_human() {
    date +"%Y-%m-%d %H:%M:%S"
}

# Format a non-negative integer second count as a compact human-readable
# duration. Examples:
#   0     -> "0s"
#   45    -> "45s"
#   137   -> "2m 17s"
#   3725  -> "1h 2m 5s"
# Negative or non-numeric input returns "?".
timing_format_duration() {
    local secs="${1:-0}"
    if ! [[ "$secs" =~ ^[0-9]+$ ]]; then
        echo "?"
        return 0
    fi
    local h=$((secs / 3600))
    local m=$(((secs % 3600) / 60))
    local s=$((secs % 60))
    if [ "$h" -gt 0 ]; then
        printf "%dh %dm %ds" "$h" "$m" "$s"
    elif [ "$m" -gt 0 ]; then
        printf "%dm %ds" "$m" "$s"
    else
        printf "%ds" "$s"
    fi
}

# Print three lines summarizing wall-clock start, end, and duration.
# Caller passes start_epoch end_epoch [start_human] [end_human]. If the
# human strings are omitted, they're computed from the epochs.
#
# Usage:
#   timing_print_summary_block "$start_epoch" "$end_epoch" "$start_human" "$end_human"
timing_print_summary_block() {
    local start_epoch="$1"
    local end_epoch="$2"
    local start_human="${3:-}"
    local end_human="${4:-}"

    if [ -z "$start_human" ]; then
        start_human=$(date -d "@$start_epoch" +"%Y-%m-%d %H:%M:%S" 2>/dev/null || echo "?")
    fi
    if [ -z "$end_human" ]; then
        end_human=$(date -d "@$end_epoch" +"%Y-%m-%d %H:%M:%S" 2>/dev/null || echo "?")
    fi

    local dur
    dur=$(timing_format_duration $((end_epoch - start_epoch)))

    printf "  Started:      %s\n" "$start_human"
    printf "  Ended:        %s\n" "$end_human"
    printf "  Duration:     %s\n" "$dur"
}

# Print a per-test timings table. Caller passes three parallel arrays via
# nameref (bash 4.3+ required, available everywhere this project runs):
#
#   timing_print_per_test_table TEST_RESULTS TEST_DURATIONS_S TEST_NAMES
#
#   TEST_RESULTS:     array of "PASS" / "FAIL" / "SKIP" / etc strings
#   TEST_DURATIONS_S: array of integer seconds
#   TEST_NAMES:       array of human-readable test names
#
# Columns are right-aligned for the duration so values line up across rows.
# Result label is fixed-width [4-char] in square brackets.
timing_print_per_test_table() {
    local -n _results=$1
    local -n _durs=$2
    local -n _names=$3
    local n=${#_names[@]}
    [ "$n" -eq 0 ] && return 0

    # Pre-format durations and find the widest one for column alignment.
    local i
    local -a dur_strs=()
    local max_dur=0
    for ((i=0; i<n; i++)); do
        local d
        d=$(timing_format_duration "${_durs[$i]:-0}")
        dur_strs+=("$d")
        [ ${#d} -gt $max_dur ] && max_dur=${#d}
    done

    echo "  Per-test timings:"
    for ((i=0; i<n; i++)); do
        printf "    [%-4s]  %*s  %s\n" \
            "${_results[$i]}" "$max_dur" "${dur_strs[$i]}" "${_names[$i]}"
    done
}
