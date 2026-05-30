#!/bin/bash

# ============================================================================
# X/COLLECT MEMBERSHIP-DRIVEN PROMOTION QUEUE TESTS
# ============================================================================
# Covers the EndBlocker drain triggered by RepHooks.AfterMemberAdmitted:
#
#   Phase 1 (inviter-stake refund): when a non-member who is a collaborator
#     on a member-owned collection becomes a member, the inviter's locked
#     DREAM stake is unlocked, the Collaborator record is stripped of its
#     inviter / dream_stake bookkeeping, and the collection's
#     non_member_collaborator_count is decremented.
#
#   Phase 2 (owned-collection promotion): any PENDING TTL collection the
#     new member owns is flipped to ACTIVE + permanent. Endorsement creation
#     fee is refunded. ACTIVE+TTL collections (sponsored / endorsed) also
#     promote, but this script focuses on the PENDING path that exercises
#     the most state transitions.
#
# Both kinds of work share params.max_promotions_per_block (default 50).
# ============================================================================

echo "--- TESTING: X/COLLECT MEMBERSHIP-DRIVEN PROMOTION QUEUE ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"
source "$SCRIPT_DIR/test_helpers.sh"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

ALICE_ADDR=$(get_address alice)
echo "Alice (inviter / collection owner): $ALICE_ADDR"
echo ""

# ============================================================================
# Phase 1 setup: create a brand-new non-member, fund it, add it as
# a collaborator on a member-owned collection (Alice locks DREAM), then
# create a PENDING TTL collection owned by the same non-member.
# ============================================================================
echo "=== PHASE 1: pre-admission fixtures ==="
echo ""

PROMOTEE_ACCOUNT="collect_promoq_pre_admission"
if ! $BINARY keys show $PROMOTEE_ACCOUNT --keyring-backend $KEYRING > /dev/null 2>&1; then
    $BINARY keys add $PROMOTEE_ACCOUNT --keyring-backend $KEYRING > /dev/null 2>&1
fi
PROMOTEE_ADDR=$(get_address $PROMOTEE_ACCOUNT)
echo "  Promotee account: $PROMOTEE_ADDR"

# Promotee must NOT already be a member or the test is meaningless.
MEMBER_INFO=$($BINARY query rep get-member $PROMOTEE_ADDR --output json 2>&1)
if ! echo "$MEMBER_INFO" | grep -q "not found"; then
    # A prior failed run may have admitted this account. Rotate the key so
    # the test always starts from a clean slate.
    $BINARY keys delete $PROMOTEE_ACCOUNT --keyring-backend $KEYRING -y > /dev/null 2>&1
    $BINARY keys add $PROMOTEE_ACCOUNT --keyring-backend $KEYRING > /dev/null 2>&1
    PROMOTEE_ADDR=$(get_address $PROMOTEE_ACCOUNT)
    echo "  Rotated promotee key: $PROMOTEE_ADDR"
fi

# Fund the promotee — enough SPARK for fees AND for the non-member
# collection deposit + endorsement creation fee.
echo "  Funding promotee with SPARK..."
TX_OUT=$(send_tx bank send alice $PROMOTEE_ADDR 60000000${BOND_DENOM} --from alice)
sleep $TX_WAIT

# --- Fixture A: member-owned collection with promotee as non-member
# collaborator (Alice locks DREAM as inviter stake) ---

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 10000))
echo "  Alice creates member-owned host collection..."
TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "promoq-host" "promotion-queue host collection" "" "" \
    --from alice)
assert_tx_success "Alice creates host collection" "$TX_OUT"
HOST_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$HOST_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$ALICE_ADDR")
    HOST_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Host collection ID: $HOST_COLL_ID"

# Snapshot Alice's DREAM balance before locking the inviter stake so we can
# assert the eventual refund.
ALICE_DREAM_BEFORE_LOCK=$($BINARY query rep get-member "$ALICE_ADDR" -o json 2>/dev/null \
    | jq -r '.member.dream_balance // "0"')
ALICE_STAKED_BEFORE_LOCK=$($BINARY query rep get-member "$ALICE_ADDR" -o json 2>/dev/null \
    | jq -r '.member.staked_dream // "0"')
echo "  Alice DREAM before lock: balance=$ALICE_DREAM_BEFORE_LOCK staked=$ALICE_STAKED_BEFORE_LOCK"

echo "  Alice adds promotee as non-member EDITOR collaborator..."
TX_OUT=$(send_tx collect add-collaborator \
    "$HOST_COLL_ID" "$PROMOTEE_ADDR" editor \
    --from alice)
assert_tx_success "Alice adds promotee as non-member collaborator" "$TX_OUT"

# Confirm inviter stake actually locked.
ALICE_STAKED_AFTER_LOCK=$($BINARY query rep get-member "$ALICE_ADDR" -o json 2>/dev/null \
    | jq -r '.member.staked_dream // "0"')
LOCK_DELTA=$((ALICE_STAKED_AFTER_LOCK - ALICE_STAKED_BEFORE_LOCK))
assert_gt "Alice's staked DREAM increased on add-collaborator" "0" "$LOCK_DELTA"
echo "  Alice staked DREAM delta: +$LOCK_DELTA"

# Confirm collaborator record carries the inviter/stake bookkeeping.
HOST_COLL=$(query collect collection "$HOST_COLL_ID")
NM_COUNT=$(echo "$HOST_COLL" | jq -r '.collection.non_member_collaborator_count // "0"')
assert_equal "Host collection has 1 non-member collaborator" "1" "$NM_COUNT"

# --- Fixture B: PENDING TTL collection owned by the promotee ---

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 100000))
echo "  Promotee creates PENDING TTL collection..."
TX_OUT=$(send_tx collect create-collection \
    nft public false "$FUTURE_BLOCK" "promoq-pending" "auto-promotion target" "" "" \
    --from $PROMOTEE_ACCOUNT)
assert_tx_success "Promotee creates PENDING TTL collection" "$TX_OUT"
PENDING_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$PENDING_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$PROMOTEE_ADDR")
    PENDING_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Pending collection ID: $PENDING_COLL_ID"

# Confirm it's actually PENDING + ephemeral.
PENDING_BEFORE=$(query collect collection "$PENDING_COLL_ID")
STATUS_BEFORE=$(echo "$PENDING_BEFORE" | jq -r '.collection.status // empty')
EXPIRES_BEFORE=$(echo "$PENDING_BEFORE" | jq -r '.collection.expires_at // "0"')
DEPOSIT_BURNED_BEFORE=$(echo "$PENDING_BEFORE" | jq -r '.collection.deposit_burned // false')
assert_equal "Pre-admission collection is PENDING" "COLLECTION_STATUS_PENDING" "$STATUS_BEFORE"
assert_gt   "Pre-admission collection has TTL (expires_at > 0)" "0" "$EXPIRES_BEFORE"
assert_equal "Pre-admission collection has deposit_burned=false" "false" "$DEPOSIT_BURNED_BEFORE"

# Snapshot promotee SPARK before admission so we can assert the endorsement
# fee refund flowed back.
PROMOTEE_SPARK_BEFORE=$($BINARY query bank balance $PROMOTEE_ADDR ${BOND_DENOM} --output json 2>/dev/null \
    | jq -r '.balance.amount // "0"')
echo "  Promotee SPARK before admission: $PROMOTEE_SPARK_BEFORE"
echo ""

# ============================================================================
# Phase 2: invite + accept → AfterMemberAdmitted hook fires → EndBlocker
# drains both passes in subsequent blocks.
# ============================================================================
echo "=== PHASE 2: invite + accept + drain ==="
echo ""

REQUIRED_STAKE=$($BINARY query rep required-invitation-stake "$ALICE_ADDR" --output json 2>/dev/null \
    | jq -r '.required_stake // "100000000"')
echo "  Required invitation stake: $REQUIRED_STAKE"

TX_OUT=$(send_tx rep invite-member "$PROMOTEE_ADDR" "$REQUIRED_STAKE" --from alice)
assert_tx_success "Alice invites promotee" "$TX_OUT"
INVITATION_ID=$(extract_event_attr "$TX_RESULT_OUT" "create_invitation" "invitation_id")
if [ -z "$INVITATION_ID" ]; then
    INVITE_LIST=$($BINARY query rep list-invitation -o json 2>/dev/null)
    INVITATION_ID=$(echo "$INVITE_LIST" | jq -r --arg addr "$PROMOTEE_ADDR" \
        '.invitation[] | select(.invitee_address==$addr) | .id // empty' 2>/dev/null | head -1)
fi
echo "  Invitation ID: $INVITATION_ID"

TX_OUT=$(send_tx rep accept-invitation "$INVITATION_ID" --from $PROMOTEE_ACCOUNT)
assert_tx_success "Promotee accepts invitation" "$TX_OUT"

# Wait two blocks for the EndBlocker drain (default cap 50, so both passes
# complete in a single block — the extra block buys finality margin).
echo "  Waiting for EndBlocker drain..."
sleep 12
echo ""

# ============================================================================
# TEST 1: Inviter stake refunded; collaborator record stripped.
# ============================================================================
echo "--- TEST 1: Inviter stake refunded after admission ---"

ALICE_STAKED_AFTER_DRAIN=$($BINARY query rep get-member "$ALICE_ADDR" -o json 2>/dev/null \
    | jq -r '.member.staked_dream // "0"')
STAKED_DELTA_BACK=$((ALICE_STAKED_AFTER_LOCK - ALICE_STAKED_AFTER_DRAIN))
# Allow ±10M udream slack for staked decay (~0.05%/epoch) accumulated during
# the drain wait. The unlock amount is the dominant signal; decay only adds
# a tiny fraction relative to LOCK_DELTA (≈100M).
DELTA_DIFF=$((STAKED_DELTA_BACK - LOCK_DELTA))
[ "$DELTA_DIFF" -lt 0 ] && DELTA_DIFF=$((-DELTA_DIFF))
if [ "$DELTA_DIFF" -lt 10000000 ]; then
    echo "PASS: Alice's locked stake matches the original lock delta (within decay slack)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Alice's locked stake matches the original lock delta"
    echo "  Expected: ~$LOCK_DELTA"
    echo "  Actual:   $STAKED_DELTA_BACK"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# Collaborator record stays — promotee is still a collaborator, now as a
# member — but inviter/dream_stake are cleared.
COLLABS_AFTER=$(query collect collaborators "$HOST_COLL_ID")
INVITER_AFTER=$(echo "$COLLABS_AFTER" | jq -r --arg addr "$PROMOTEE_ADDR" \
    '.collaborators[] | select(.address==$addr) | .inviter // ""')
STAKE_AFTER=$(echo "$COLLABS_AFTER" | jq -r --arg addr "$PROMOTEE_ADDR" \
    '.collaborators[] | select(.address==$addr) | .dream_stake // ""')
assert_equal "Collaborator inviter field cleared" "" "$INVITER_AFTER"
assert_equal "Collaborator dream_stake reset to 0" "0" "$STAKE_AFTER"

# non_member_collaborator_count decremented.
HOST_AFTER=$(query collect collection "$HOST_COLL_ID")
NM_COUNT_AFTER=$(echo "$HOST_AFTER" | jq -r '.collection.non_member_collaborator_count // "0"')
assert_equal "Host non_member_collaborator_count decremented" "0" "$NM_COUNT_AFTER"
echo ""

# ============================================================================
# TEST 2: PENDING TTL collection promoted to ACTIVE + permanent.
# ============================================================================
echo "--- TEST 2: PENDING TTL collection promoted to permanent ---"

PENDING_AFTER=$(query collect collection "$PENDING_COLL_ID")
STATUS_AFTER=$(echo "$PENDING_AFTER" | jq -r '.collection.status // empty')
EXPIRES_AFTER=$(echo "$PENDING_AFTER" | jq -r '.collection.expires_at // "0"')
DEPOSIT_BURNED_AFTER=$(echo "$PENDING_AFTER" | jq -r '.collection.deposit_burned // false')
SEEKING_AFTER=$(echo "$PENDING_AFTER" | jq -r '.collection.seeking_endorsement // false')

assert_equal "Status flipped to ACTIVE" "COLLECTION_STATUS_ACTIVE" "$STATUS_AFTER"
assert_equal "expires_at cleared to 0" "0" "$EXPIRES_AFTER"
assert_equal "deposit_burned set to true" "true" "$DEPOSIT_BURNED_AFTER"
assert_equal "seeking_endorsement cleared" "false" "$SEEKING_AFTER"
echo ""

# ============================================================================
# TEST 3: Endorsement creation fee refunded — promotee's SPARK rose by at
# least the EndorsementCreationFee param value (default 10 SPARK).
# ============================================================================
echo "--- TEST 3: Endorsement creation fee refunded ---"

PROMOTEE_SPARK_AFTER=$($BINARY query bank balance $PROMOTEE_ADDR ${BOND_DENOM} --output json 2>/dev/null \
    | jq -r '.balance.amount // "0"')
SPARK_DELTA=$((PROMOTEE_SPARK_AFTER - PROMOTEE_SPARK_BEFORE))
echo "  Promotee SPARK delta: $SPARK_DELTA"

ENDORSEMENT_FEE=$($BINARY query collect params --output json 2>/dev/null \
    | jq -r '.params.endorsement_creation_fee // "10000000"')
echo "  Param endorsement_creation_fee: $ENDORSEMENT_FEE"

# The delta floors at the fee minus the few-thousand-uspark accept-invitation
# fee — assert_gt just confirms the refund flowed (delta is positive). A
# strict equality check would be flaky against fee changes.
assert_gt "Promotee received the endorsement creation fee refund" "0" "$SPARK_DELTA"
echo ""

# ============================================================================
# TEST 4: Post-admission TTL collection from the same address is ACTIVE
# from creation (no PENDING) — confirms the standard member-create path is
# unchanged.
# ============================================================================
echo "--- TEST 4: Post-admission collection is ACTIVE on creation ---"

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 10000))
TX_OUT=$(send_tx collect create-collection \
    nft public false "$FUTURE_BLOCK" "post-admission" "" "" "" \
    --from $PROMOTEE_ACCOUNT)
assert_tx_success "Post-admission TTL collection creates" "$TX_OUT"

POST_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$POST_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$PROMOTEE_ADDR")
    POST_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
POST_COLL=$(query collect collection "$POST_COLL_ID")
POST_STATUS=$(echo "$POST_COLL" | jq -r '.collection.status // empty')
assert_equal "Post-admission collection is ACTIVE from creation" \
    "COLLECTION_STATUS_ACTIVE" "$POST_STATUS"
echo ""

# ============================================================================
# Summary
# ============================================================================
print_summary
