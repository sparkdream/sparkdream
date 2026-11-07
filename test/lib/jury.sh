#!/bin/bash
# ============================================================================
# SHARED JURY HELPERS FOR E2E TESTS
# ============================================================================
# x/rep seats a jury in the BeginBlocker of the block AFTER the tx that
# opened (or refilled) it, so the jurors are not there yet when wait_for_tx
# returns for that tx. Poll for the seating instead of reading the roster
# right away.
#
# Usage from a test script:
#   source "$SCRIPT_DIR/../lib/jury.sh"
#   wait_for_jury_seated "$REVIEW_ID"        # default 30 s
#   wait_for_jury_seated "$REVIEW_ID" 60
#
# Returns 0 once the review has no seats waiting for a draw (pending_seats
# is 0, omitted from JSON when zero), 1 on timeout. A review nobody can be
# seated on still returns 0: the draw ran and seated whoever it could.
# ============================================================================

wait_for_jury_seated() {
    local review_id="$1"
    local timeout="${2:-30}"
    local bin="${BINARY:-sparkdreamd}"
    local deadline=$(( $(date +%s) + timeout ))
    local pending
    while [ "$(date +%s)" -lt "$deadline" ]; do
        pending=$(timeout 10 "$bin" query rep get-jury-review "$review_id" --output json 2>/dev/null \
            | jq -r '.jury_review.pending_seats // "0"')
        if [ "$pending" = "0" ]; then
            return 0
        fi
        sleep 1
    done
    echo "  [WARN] jury review $review_id still has $pending seat(s) waiting for a draw after ${timeout}s"
    return 1
}
