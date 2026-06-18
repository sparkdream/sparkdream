#!/bin/bash

# Strict E2E for the sentinel rolling-accuracy-window implementation.
#
# Validates, end to end through the real CLI/state, that resolving a forum
# moderation appeal records the verdict in the sentinel's per-reward-epoch
# accuracy ring (SentinelActivity.accuracy_window), that the resolution lands in
# the correct epoch bucket, and that crossing a reward-epoch boundary opens a
# fresh bucket (the window advances). The window's CONSUMPTION by reward
# distribution (gates) and the OVERTURNED ring path are covered by Go unit
# tests; here we prove the on-chain wiring: rep appeal-resolver ->
# forum bumpAccuracyWindow -> ring -> query.
#
# Every resolve is UPHELD: an OVERTURNED resolve would leave the shared sentinel1
# in a 24h overturn cooldown that breaks later forum tests (see PART 2 note).
#
# Flow per appeal: poster creates a post, sentinel1 hides it, the author appeals
# (forum appeal-post -> rep GovActionAppeal), and the Operations Committee
# (alice) resolves it via `rep resolve-gov-action-appeal`. Lifetime upheld
# counters are asserted to move in parallel with the window.
#
# Depends on the forum account setup (.test_env). Self-bonds sentinel1.

echo "--- TESTING: SENTINEL ROLLING ACCURACY WINDOW ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing). Run: bash setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"
BOND_DENOM="${BOND_DENOM:-uspark}"

FAILED=0
fail() { echo "  FAIL: $1"; FAILED=1; }
pass() { echo "  PASS: $1"; }

TX_FLAGS="--chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json"

# ========================================================================
# Helpers (mirror the shared forum-test patterns)
# ========================================================================
wait_for_tx() {
    local TXHASH=$1 MAX=20 N=0 R
    while [ $N -lt $MAX ]; do
        R=$($BINARY q tx "$TXHASH" --output json 2>&1)
        if echo "$R" | jq -e '.code' >/dev/null 2>&1; then echo "$R"; return 0; fi
        N=$((N + 1)); sleep 1
    done
    echo '{"code":1,"raw_log":"tx not found"}'; return 1
}
check_tx_success() { [ "$(echo "$1" | jq -r '.code // 1')" = "0" ]; }
extract_event_value() {
    echo "$1" | jq -r ".events[]? | select(.type==\"$2\") | .attributes[]? | select(.key==\"$3\") | .value" | tr -d '"'
}

# run_tx <from-key> <tx args...> -> echoes tx-result JSON; nonzero return on
# pre-broadcast failure (no txhash).
run_tx() {
    local FROM=$1; shift
    local RES TXHASH
    RES=$($BINARY tx "$@" --from "$FROM" $TX_FLAGS 2>&1)
    TXHASH=$(echo "$RES" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$TXHASH" ] || [ "$TXHASH" = "null" ]; then echo "$RES"; return 1; fi
    sleep 3
    wait_for_tx "$TXHASH"
}

# Chain height + reward-epoch helpers.
height() { $BINARY status 2>/dev/null | jq -r '.sync_info.latest_block_height // .SyncInfo.latest_block_height // 0'; }
epoch_now() { echo $(( $(height) / EPOCH_BLOCKS )); }

# SentinelActivity accessors for sentinel1.
sa_json() { $BINARY query forum get-sentinel-activity "$SENTINEL1_ADDR" --output json 2>/dev/null; }
sa_field() { sa_json | jq -r ".sentinel_activity.$1 // 0"; }
# Active accuracy buckets, normalized to numbers (proto3 renders uint64 as JSON
# strings, so epoch/upheld/overturned come back quoted). Pre-allocated empty ring
# slots (0/0) are filtered out.
active_buckets() {
    sa_json | jq -c '[.sentinel_activity.accuracy_window[]?
        | {epoch:((.epoch//0)|tonumber), upheld:((.upheld//0)|tonumber), overturned:((.overturned//0)|tonumber)}
        | select((.upheld + .overturned) > 0)]'
}
win_upheld()     { active_buckets | jq '[.[].upheld] | add // 0'; }
win_overturned() { active_buckets | jq '[.[].overturned] | add // 0'; }
active_epochs()  { active_buckets | jq -c '[.[].epoch] | unique'; }

# create_hide_appeal <poster-key> -> echoes "<post_id> <appeal_id>"
create_hide_appeal() {
    local POSTER=$1 RES PID AID
    RES=$(run_tx "$POSTER" forum create-post "$TEST_CATEGORY_ID" "0" "window-test-$(date +%s)-$RANDOM")
    PID=$(extract_event_value "$RES" "post_created" "post_id")
    [ -z "$PID" ] && PID=$($BINARY query forum list-post --output json 2>/dev/null | jq -r '.post[-1].id // empty')
    run_tx sentinel1 forum hide-post "$PID" "1" "window test hide" >/dev/null
    sleep 6   # appeal cooldown (5s) before the author may appeal
    run_tx "$POSTER" forum appeal-post "$PID" >/dev/null
    AID=$($BINARY query rep list-gov-action-appeal --output json 2>/dev/null \
        | jq -r --arg t "$PID" '[.gov_action_appeal[]? | select(.action_target==$t and .status=="GOV_APPEAL_STATUS_PENDING")] | sort_by(.id) | last | .id // empty')
    echo "$PID $AID"
}

# resolve <appeal-id> <upheld|overturned>
resolve() { run_tx alice rep resolve-gov-action-appeal "$1" "$2" "e2e accuracy window test"; }

# ========================================================================
# PART 0: DERIVE EPOCH CADENCE + BOND SENTINEL
# ========================================================================
echo "--- PART 0: SETUP ---"
EPOCH_BLOCKS=$($BINARY query rep params --output json 2>/dev/null | jq -r '.params.sentinel_reward_epoch_blocks // "20"')
[ -z "$EPOCH_BLOCKS" ] || [ "$EPOCH_BLOCKS" = "null" ] && EPOCH_BLOCKS=20
echo "  Reward-epoch cadence: $EPOCH_BLOCKS blocks"

# Bond sentinel1 to a healthy level so a single overturn slash (100 DREAM) never
# drops it below the 500-DREAM min and demotes it mid-test.
CUR_BOND=$($BINARY query rep bonded-role forum-sentinel "$SENTINEL1_ADDR" --output json 2>/dev/null | jq -r '.bonded_role.current_bond // "0"')
[ -z "$CUR_BOND" ] || [ "$CUR_BOND" = "null" ] && CUR_BOND=0
if [ "$CUR_BOND" -lt 1000000000 ]; then
    echo "  Bonding sentinel1 (current bond: $CUR_BOND)..."
    run_tx sentinel1 rep bond-role forum-sentinel 2500000000 >/dev/null
fi
CUR_BOND=$($BINARY query rep bonded-role forum-sentinel "$SENTINEL1_ADDR" --output json 2>/dev/null | jq -r '.bonded_role.current_bond // "0"')
echo "  sentinel1 bond: $CUR_BOND"
[ "$CUR_BOND" -ge 1000000000 ] && pass "sentinel1 bonded with headroom" || fail "sentinel1 bond too low ($CUR_BOND)"

# ========================================================================
# PART 1: WINDOW PARAM IS VISIBLE
# ========================================================================
echo "--- PART 1: ACCURACY-WINDOW PARAM VISIBILITY ---"
WIN_PARAM=$($BINARY query rep params --output json 2>/dev/null | jq -r '.params.sentinel_accuracy_window_epochs // empty')
if [ -n "$WIN_PARAM" ] && [ "$WIN_PARAM" -ge 1 ] 2>/dev/null; then
    pass "sentinel_accuracy_window_epochs present in params ($WIN_PARAM)"
else
    fail "sentinel_accuracy_window_epochs missing/invalid (got '$WIN_PARAM')"
fi

# Capture baselines (sentinel1 may carry activity from earlier tests in the run).
B_WIN_UP=$(win_upheld); B_WIN_OV=$(win_overturned)
B_LIFE_UP=$(sa_field upheld_hides); B_LIFE_OV=$(sa_field overturned_hides)
echo "  baseline windowed up/ov: $B_WIN_UP/$B_WIN_OV ; lifetime hides up/ov: $B_LIFE_UP/$B_LIFE_OV"

# ========================================================================
# PART 2: TWO UPHELD RESOLVES -> RING + LIFETIME UPHELD COUNTERS MOVE
#
# Every resolve in this test is UPHELD on purpose. An OVERTURNED resolve puts
# the sentinel in a 24h overturn cooldown (ErrSentinelCooldown) that persists
# for the rest of the suite, breaking every later forum test that hides with the
# shared sentinel1 (promoter_warning, post_conviction, ...). The OVERTURNED ring
# path is byte-identical wiring (bumpAccuracyWindow with upheld=false) and is
# covered by the Go unit tests; exercising it here is not worth contaminating a
# shared moderator.
# ========================================================================
echo "--- PART 2: WINDOW POPULATION (TWO UPHELD) ---"

read -r PID1 AID1 <<<"$(create_hide_appeal poster1)"
if [ -n "$AID1" ]; then pass "appeal A filed (post $PID1, appeal $AID1)"; else fail "could not obtain appeal A id (post $PID1)"; fi
if [ -n "$AID1" ]; then
    RES=$(resolve "$AID1" upheld)
    check_tx_success "$RES" && pass "appeal A resolved UPHELD" || { echo "  $(echo "$RES" | jq -r '.raw_log // .')"; fail "resolve A upheld"; }
fi

read -r PID2 AID2 <<<"$(create_hide_appeal poster2)"
if [ -n "$AID2" ]; then pass "appeal B filed (post $PID2, appeal $AID2)"; else fail "could not obtain appeal B id (post $PID2)"; fi
if [ -n "$AID2" ]; then
    RES=$(resolve "$AID2" upheld)
    check_tx_success "$RES" && pass "appeal B resolved UPHELD" || { echo "  $(echo "$RES" | jq -r '.raw_log // .')"; fail "resolve B upheld"; }
fi

M_WIN_UP=$(win_upheld); M_WIN_OV=$(win_overturned)
M_LIFE_UP=$(sa_field upheld_hides); M_LIFE_OV=$(sa_field overturned_hides)

[ "$M_WIN_UP" = "$((B_WIN_UP + 2))" ] && pass "windowed upheld +2 ($B_WIN_UP -> $M_WIN_UP)" || fail "windowed upheld delta wrong ($B_WIN_UP -> $M_WIN_UP)"
[ "$M_WIN_OV" = "$B_WIN_OV" ] && pass "windowed overturned unchanged ($B_WIN_OV)" || fail "windowed overturned moved unexpectedly ($B_WIN_OV -> $M_WIN_OV)"
[ "$M_LIFE_UP" = "$((B_LIFE_UP + 2))" ] && pass "lifetime upheld_hides +2 ($B_LIFE_UP -> $M_LIFE_UP)" || fail "lifetime upheld_hides delta wrong ($B_LIFE_UP -> $M_LIFE_UP)"

EPOCHS_P2=$(active_epochs)
echo "  active accuracy-window epochs after PART 2: $EPOCHS_P2"

# ========================================================================
# PART 3: CROSS A REWARD-EPOCH BOUNDARY -> A FRESH EPOCH BUCKET APPEARS.
# ========================================================================
echo "--- PART 3: EPOCH BUCKETING ACROSS A REWARD-EPOCH BOUNDARY ---"

E_BEFORE=$(epoch_now)
TARGET_H=$(( (E_BEFORE + 1) * EPOCH_BLOCKS ))
echo "  current reward epoch $E_BEFORE; waiting for height >= $TARGET_H ..."
WAIT_N=0
while [ "$(height)" -lt "$TARGET_H" ] && [ $WAIT_N -lt 120 ]; do sleep 2; WAIT_N=$((WAIT_N + 1)); done
E_AFTER=$(epoch_now)
[ "$E_AFTER" -gt "$E_BEFORE" ] && pass "advanced to a new reward epoch ($E_BEFORE -> $E_AFTER)" || fail "did not cross a reward-epoch boundary ($E_BEFORE -> $E_AFTER)"

read -r PID3 AID3 <<<"$(create_hide_appeal poster1)"
if [ -n "$AID3" ]; then pass "appeal C filed in new epoch (post $PID3, appeal $AID3)"; else fail "could not obtain appeal C id (post $PID3)"; fi
if [ -n "$AID3" ]; then
    RES=$(resolve "$AID3" upheld)
    check_tx_success "$RES" && pass "appeal C resolved UPHELD" || { echo "  $(echo "$RES" | jq -r '.raw_log // .')"; fail "resolve C upheld"; }
fi

E_WIN_UP=$(win_upheld)
E_LIFE_UP=$(sa_field upheld_hides)
E_WIN_OV=$(win_overturned)
EPOCHS_P3=$(active_epochs)
echo "  active accuracy-window epochs after PART 3: $EPOCHS_P3"

# A brand-new epoch bucket (present in P3, absent in P2) proves per-epoch bucketing.
NEW_EPOCHS=$(jq -n --argjson a "$EPOCHS_P3" --argjson b "$EPOCHS_P2" '($a - $b) | length')
[ "$NEW_EPOCHS" -ge 1 ] 2>/dev/null && pass "a fresh epoch bucket was opened after the boundary" || fail "no new epoch bucket appeared ($EPOCHS_P2 -> $EPOCHS_P3)"

# The new resolve added exactly one windowed upheld + one lifetime upheld;
# overturned stays at baseline (the two counters are tracked separately).
[ "$E_WIN_UP" = "$((M_WIN_UP + 1))" ] && pass "windowed upheld +1 in new epoch ($M_WIN_UP -> $E_WIN_UP)" || fail "windowed upheld delta wrong in PART 3 ($M_WIN_UP -> $E_WIN_UP)"
[ "$E_LIFE_UP" = "$((M_LIFE_UP + 1))" ] && pass "lifetime upheld_hides +1 ($M_LIFE_UP -> $E_LIFE_UP)" || fail "lifetime upheld_hides delta wrong ($M_LIFE_UP -> $E_LIFE_UP)"
[ "$E_WIN_OV" = "$B_WIN_OV" ] && pass "windowed overturned still baseline ($B_WIN_OV)" || fail "windowed overturned changed unexpectedly ($B_WIN_OV -> $E_WIN_OV)"

echo ""
if [ "$FAILED" = "0" ]; then
    echo "--- SENTINEL ACCURACY WINDOW TEST: ALL CHECKS PASSED ---"
    exit 0
else
    echo "--- SENTINEL ACCURACY WINDOW TEST: FAILURES PRESENT ---"
    exit 1
fi
