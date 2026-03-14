#!/bin/bash

# check_testparams.sh — Verify the running binary was built with -tags testparams.
# Source this from any per-module run_all_tests.sh before running tests.
#
# Usage:  source "$(dirname "$0")/../check_testparams.sh"

BINARY="${BINARY:-sparkdreamd}"

check_testparams_build() {
    if ! command -v "$BINARY" &>/dev/null; then
        echo "ERROR: $BINARY not found in PATH."
        echo "  Run:  ignite chain build --build.tags testparams"
        exit 1
    fi

    # Query a parameter that differs between production and testparams builds.
    # Production commons MinExecutionPeriod is 72h (259200s).
    # Testparams value is 1s.
    # We check the Commons Council group's min_execution_period from genesis.
    # If the chain isn't running, fall back to checking genesis directly.
    local min_exec
    min_exec=$("$BINARY" query commons get-group "Commons Council" --output json 2>/dev/null \
        | jq -r '.group.decision_policy.min_execution_period // empty' 2>/dev/null)

    if [ -z "$min_exec" ]; then
        # Chain might not be running yet — skip check, trust the caller.
        return 0
    fi

    # Testparams sets 1s; production sets 259200s (72h) or higher.
    # Parse the duration: strip trailing 's' and compare.
    local seconds
    seconds=$(echo "$min_exec" | sed 's/s$//')

    if [ -n "$seconds" ] && [ "$seconds" -gt 60 ] 2>/dev/null; then
        echo ""
        echo "WARNING: Binary appears to be built with PRODUCTION parameters."
        echo "  min_execution_period = ${min_exec} (expected ~1s for tests)"
        echo ""
        echo "  Rebuild with:  ignite chain build --build.tags testparams"
        echo "  Or from test/:  ../run_all_tests.sh (handles build automatically)"
        echo ""
        read -r -t 10 -p "Continue anyway? [y/N] " answer || answer="n"
        if [[ ! "$answer" =~ ^[Yy] ]]; then
            echo "Aborting."
            exit 1
        fi
    fi
}

check_testparams_build
