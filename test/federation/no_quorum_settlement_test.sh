#!/bin/bash

echo "--- TESTING: FEDERATION no-quorum settlement + verifier commitment release ---"
#
# Covers the P0.1 / P0.4 behaviour that the Mastodon live-link work added,
# none of which the pre-existing suite exercised:
#
#   TEST 1: DISPUTED + no arbiter quorum -> UNRESOLVED, and the verifier's
#           committed bond comes back. Before P0.1 the content sat DISPUTED
#           until content_ttl and the commitment leaked forever, because the
#           only ReleaseBond path ran off the ChallengeWindow queue, which a
#           disputed record never enters.
#   TEST 2: UNRESOLVED is terminal -- MsgModerateContent must refuse it as a
#           SOURCE, not merely as a target. Validating new_status alone let an
#           OpsComm member manufacture a VERIFIED status for content that
#           nobody verified.
#   TEST 3: CHALLENGED + no arbiter quorum -> back to VERIFIED, and the
#           commitment comes back. This follows applyJuryVerdictTimeout's
#           existing rule for the same "no finding was reached" condition.
#           Settling it to UNRESOLVED instead would let anyone permanently
#           demote any verified content for the price of one challenge fee.
#   TEST 4: A verifier is still able to verify after N no-quorum disputes.
#           The practical symptom of the leak: at 50 DREAM committed per
#           event against a 500 DREAM bond, ten events locked the role out
#           silently.
#   TEST 5-8: the P0.4 ListFederatedContent filters (peer_id, status,
#           content_type, creator_identity) and match-space pagination.
#
# Timing: relies on testparams' 15s arbiter_resolution_window, so no gov
# proposal is needed -- the windows are already short enough to walk a
# record through its whole lifecycle inside one test run.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="${PROPOSAL_DIR:-$SCRIPT_DIR/proposals}"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "ERROR: .test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi
source "$SCRIPT_DIR/.test_env"
# shellcheck disable=SC1091
source "$SCRIPT_DIR/peer_fixtures.sh"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

PEER="mastodon.example"
# alice is the suite's bonded verifier (verifier_test.sh:71). The
# verifier1/verifier2 keys exist but are TRUST_LEVEL_NEW, and bond-role
# requires ESTABLISHED, so they cannot hold this role.
VERIFIER_A="alice"
VERIFIER_A_ADDR="$ALICE_ADDR"

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then PASS_COUNT=$((PASS_COUNT + 1)); else FAIL_COUNT=$((FAIL_COUNT + 1)); fi
    echo "  => $RESULT"
}

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx "$TXHASH" --output json 2>/dev/null)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    echo "ERROR: Transaction $TXHASH not found" >&2; return 1
}

submit_and_wait() {
    local TX_RES=$1
    local LABEL=${2:-"transaction"}
    TX_OK=false
    local TXHASH
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then echo "  FAIL: $LABEL - no txhash"; TX_RESULT="$TX_RES"; return 1; fi
    local BCODE
    BCODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BCODE" != "0" ] && [ "$BCODE" != "null" ]; then
        echo "  FAIL: $LABEL - broadcast rejected (code=$BCODE)"
        TX_RESULT="$TX_RES"
        return 1
    fi
    sleep 4
    TX_RESULT=$(wait_for_tx "$TXHASH") || return 1
    local CODE
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  FAIL: $LABEL (code=$CODE) raw=$(echo "$TX_RESULT" | jq -r '.raw_log' | head -c 200)"
        return 1
    fi
    TX_OK=true
    return 0
}

sha256_base64() {
    echo -n "$1" | sha256sum | awk '{print $1}' | xxd -r -p | base64 -w0
}

content_status() {
    $BINARY query federation get-federated-content "$1" --output json 2>/dev/null \
        | jq -r '.content.status // "MISSING"'
}

committed_bond() {
    $BINARY query rep bonded-role federation-verifier "$VERIFIER_A_ADDR" --output json 2>/dev/null \
        | jq -r '.bonded_role.total_committed_bond // "0"'
}

# commitment_released <content_id>
#
# The per-record flag, which is the ONLY reliable signal that this
# particular commitment was given back. The aggregate total_committed_bond
# is not usable as a before/after assertion here: verifier_test.sh leaves
# its own DISPUTED records behind, and those settle on their own 15s
# arbiter window -- i.e. DURING this suite -- releasing their commitments
# and moving the aggregate independently of anything we did. An earlier
# version of this test compared the aggregate and reported a spurious
# "LEAK: 40000000 -> 40000000 -> 20000000", which was simply someone
# else's commitment being released correctly.
commitment_released() {
    $BINARY query federation get-verification-record "$1" --output json 2>/dev/null \
        | jq -r '.record.commitment_released // false'
}

# Poll block TIME (not wall clock) past the arbiter resolution window, then
# give the EndBlocker a couple of blocks to run its sweep. Block rate drifts
# under load, so polling beats sleeping -- see the block-rate note in the
# collect e2e suite.
wait_past_arbiter_window() {
    local START_TIME
    START_TIME=$($BINARY status 2>/dev/null | jq -r '.sync_info.latest_block_time // empty')
    if [ -z "$START_TIME" ]; then sleep 25; return; fi
    local START_EPOCH
    START_EPOCH=$(date -d "$START_TIME" +%s 2>/dev/null || echo 0)
    local WINDOW=20   # testparams arbiter_resolution_window is 15s; +5s slack
    local ATTEMPT=0
    while [ $ATTEMPT -lt 60 ]; do
        local NOW
        NOW=$($BINARY status 2>/dev/null | jq -r '.sync_info.latest_block_time // empty')
        local NOW_EPOCH
        NOW_EPOCH=$(date -d "$NOW" +%s 2>/dev/null || echo 0)
        if [ "$NOW_EPOCH" -gt 0 ] && [ "$START_EPOCH" -gt 0 ] && \
           [ $((NOW_EPOCH - START_EPOCH)) -ge $WINDOW ]; then
            sleep 3   # let the sweep land in a block
            return
        fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 2
    done
    sleep 3
}

# submit + mismatch-verify one item, leaving it DISPUTED with a live
# commitment. Echoes the content id.
make_disputed_content() {
    local SEED=$1
    local BODY="no-quorum body $SEED"
    local REAL_HASH WRONG_HASH TX_RES CID
    REAL_HASH=$(sha256_base64 "$BODY")
    WRONG_HASH=$(sha256_base64 "definitely not $SEED")

    TX_RES=$($BINARY tx federation submit-federated-content \
        "$PEER" "nq-$SEED" "blog_post" \
        "@nq@${PEER}" "No Quorum" "No Quorum $SEED" \
        "$BODY" "https://${PEER}/users/nq/statuses/$SEED" "1700050000" \
        --content-hash "$REAL_HASH" \
        --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    submit_and_wait "$TX_RES" "submit nq-$SEED" >&2 || return 1
    CID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="federated_content_received").attributes[] | select(.key=="content_id").value' | tr -d '"')
    [ -z "$CID" ] && return 1

    TX_RES=$($BINARY tx federation verify-content "$CID" \
        --content-hash "$WRONG_HASH" \
        --from $VERIFIER_A --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    submit_and_wait "$TX_RES" "mismatch verify nq-$SEED" >&2 || return 1
    echo "$CID"
}

# ========================================================================
# Setup: peer + bridge + bonded verifier
#
# Follows verifier_test.sh: operator2's ACTIVE bridge on the shared
# mastodon.example peer is a suite-level fixture. Post-Phase-4 the
# BridgeBinding carries no status, so live operator state is read from
# x/service.Operator keyed by (address, service_type).
# ========================================================================
echo ""
echo "--- Setup: peer, bridge, verifier ---"

BRIDGE_RAW=$($BINARY query federation get-bridge-binding $OPERATOR2_ADDR "$PEER" --output json 2>&1)
if echo "$BRIDGE_RAW" | jq -e '.bridge_binding.address' >/dev/null 2>&1; then
    BRIDGE_CHECK=$($BINARY query service operator $OPERATOR2_ADDR federation-bridge-activitypub --output json 2>&1 | jq -r '.operator.status // "OPERATOR_STATUS_UNSPECIFIED"')
else
    BRIDGE_CHECK="not found"
fi
echo "  operator2 bridge on $PEER: $BRIDGE_CHECK"

if [ "$BRIDGE_CHECK" != "OPERATOR_STATUS_ACTIVE" ]; then
    echo ""
    echo "SKIP: no ACTIVE operator2 bridge on $PEER."
    echo "This suite builds on the bridge fixture that bridge_operator_test.sh"
    echo "and verifier_test.sh establish; run those first (or run_all_tests.sh)."
    exit 0
fi

BOND_CHECK=$($BINARY query rep bonded-role federation-verifier "$VERIFIER_A_ADDR" --output json 2>/dev/null | jq -r '.bonded_role.bond_status // "NONE"')
if [ "$BOND_CHECK" != "BONDED_ROLE_STATUS_NORMAL" ]; then
    TX_RES=$($BINARY tx rep bond-role federation-verifier 500000000 \
        --from $VERIFIER_A --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    submit_and_wait "$TX_RES" "bond verifier" || true
fi
echo "  verifier committed bond at start: $(committed_bond)"

# ========================================================================
# TEST 1: DISPUTED + no quorum -> UNRESOLVED, commitment released
# ========================================================================
echo ""
echo "--- TEST 1: no-quorum DISPUTED settles to UNRESOLVED and frees the bond ---"

COMMITTED_BEFORE=$(committed_bond)
CID1=$(make_disputed_content "t1")

if [ -n "$CID1" ]; then
    STATUS=$(content_status "$CID1")
    COMMITTED_DURING=$(committed_bond)
    echo "  content $CID1 status after mismatch verify: $STATUS (committed=$COMMITTED_DURING)"

    if [ "$STATUS" != "FEDERATED_CONTENT_STATUS_DISPUTED" ]; then
        echo "  Expected DISPUTED before the window closes, got $STATUS"
        record_result "no-quorum DISPUTED -> UNRESOLVED" "FAIL"
    else
        wait_past_arbiter_window
        STATUS=$(content_status "$CID1")
        COMMITTED_AFTER=$(committed_bond)
        echo "  status after arbiter window: $STATUS (committed=$COMMITTED_AFTER)"

        if [ "$STATUS" == "FEDERATED_CONTENT_STATUS_UNRESOLVED" ]; then
            record_result "no-quorum DISPUTED -> UNRESOLVED" "PASS"
        else
            echo "  Expected UNRESOLVED, got $STATUS"
            record_result "no-quorum DISPUTED -> UNRESOLVED" "FAIL"
        fi

        echo ""
        echo "--- TEST 1b: the verifier's committed bond returns ---"
        RELEASED=$(commitment_released "$CID1")
        REC_AMT=$($BINARY query federation get-verification-record "$CID1" --output json 2>/dev/null | jq -r '.record.committed_amount // "0"')
        echo "  record $CID1: committed_amount=$REC_AMT commitment_released=$RELEASED"
        echo "  (aggregate moved $COMMITTED_BEFORE -> $COMMITTED_DURING -> $COMMITTED_AFTER;"
        echo "   the aggregate also moves when OTHER suites' disputed records settle)"
        if [ "$RELEASED" == "true" ]; then
            echo "  this record's commitment was returned to available bond"
            record_result "no-quorum releases the commitment" "PASS"
        else
            echo "  LEAK: the record still reads commitment_released=false after settlement"
            record_result "no-quorum releases the commitment" "FAIL"
        fi
    fi
else
    echo "  Could not build a DISPUTED record"
    record_result "no-quorum DISPUTED -> UNRESOLVED" "FAIL"
    record_result "no-quorum releases the commitment" "FAIL"
fi

# ========================================================================
# TEST 2: UNRESOLVED is terminal — moderation must refuse it as a SOURCE
# ========================================================================
echo ""
echo "--- TEST 2: MsgModerateContent refuses UNRESOLVED content ---"

if [ -n "$CID1" ] && [ "$(content_status "$CID1")" == "FEDERATED_CONTENT_STATUS_UNRESOLVED" ]; then
    cat > "$PROPOSAL_DIR/moderate_unresolved.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgModerateContent",
      "authority": "$OPS_POLICY",
      "content_id": "$CID1",
      "new_status": "FEDERATED_CONTENT_STATUS_VERIFIED",
      "reason": "laundering an unverified anchor"
    }
  ],
  "metadata": "Must be rejected: UNRESOLVED is system-assigned and terminal"
}
EOF
    # The rejection is what matters; the proposal machinery only has to
    # carry the message as far as execution.
    TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/moderate_unresolved.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000${BOND_DENOM} --output json 2>/dev/null)
    PROP_OK=false
    PROP_EXECUTED=false
    if submit_and_wait "$TX_RES" "moderate-unresolved proposal"; then PROP_OK=true; fi

    if [ "$PROP_OK" == "true" ]; then
        PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
        for VOTER in alice bob; do
            TX_RES=$($BINARY tx commons vote-proposal "$PROP_ID" yes --from $VOTER -y \
                --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json 2>/dev/null)
            submit_and_wait "$TX_RES" "$VOTER vote" >/dev/null 2>&1 || true
        done
        TX_RES=$($BINARY tx commons execute-proposal "$PROP_ID" --from alice -y \
            --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} \
            --gas 2000000 --output json 2>/dev/null)
        # The execution is EXPECTED to fail -- that is the assertion. What
        # matters is that it was attempted and reached the handler.
        submit_and_wait "$TX_RES" "execute moderate-unresolved" || true
        EXEC_RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
        echo "  execution raw_log: $(echo "$EXEC_RAW" | head -c 160)"
        PROP_EXECUTED=true
        sleep 4
    fi

    FINAL=$(content_status "$CID1")
    if [ "$PROP_EXECUTED" != "true" ]; then
        # Without a real execution attempt this proves nothing: the content
        # would stay UNRESOLVED simply because nothing tried to change it.
        echo "  proposal never reached execution -- cannot assert the guard"
        record_result "UNRESOLVED rejects moderation" "FAIL"
    elif [ "$FINAL" == "FEDERATED_CONTENT_STATUS_UNRESOLVED" ]; then
        echo "  execution attempted and content stayed UNRESOLVED; the source guard held"
        record_result "UNRESOLVED rejects moderation" "PASS"
    else
        echo "  UNRESOLVED was moderated to $FINAL -- the source guard is missing"
        record_result "UNRESOLVED rejects moderation" "FAIL"
    fi
else
    echo "  No UNRESOLVED content to moderate"
    record_result "UNRESOLVED rejects moderation" "FAIL"
fi

# ========================================================================
# TEST 3: CHALLENGED + no quorum -> VERIFIED (not UNRESOLVED)
# ========================================================================
echo ""
echo "--- TEST 3: no-quorum CHALLENGED reverts to VERIFIED ---"

CH_BODY="challenge-then-silence body"
CH_HASH=$(sha256_base64 "$CH_BODY")
CH_CID=""

TX_RES=$($BINARY tx federation submit-federated-content \
    "$PEER" "nq-chal-001" "blog_post" \
    "@nqchal@${PEER}" "Challenge Grief" "Challenge Grief" \
    "$CH_BODY" "https://${PEER}/users/nqchal/statuses/1" "1700050100" \
    --content-hash "$CH_HASH" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)

if submit_and_wait "$TX_RES" "submit challenge-grief content"; then
    CH_CID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="federated_content_received").attributes[] | select(.key=="content_id").value' | tr -d '"')
fi

if [ -n "$CH_CID" ]; then
    COMMITTED_BEFORE_CH=$(committed_bond)
    TX_RES=$($BINARY tx federation verify-content "$CH_CID" --content-hash "$CH_HASH" \
        --from $VERIFIER_A --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)

    if submit_and_wait "$TX_RES" "verify challenge-grief content"; then
        # evidence is POSITIONAL. bob challenges: a verifier cannot
        # challenge their own verification (ErrSelfChallenge), and
        # challenger1 is TRUST_LEVEL_NEW.
        TX_RES=$($BINARY tx federation challenge-verification "$CH_CID" \
            "drive-by challenge, then silence" \
            --content-hash "$(sha256_base64 "a griefer's hash")" \
            --from bob --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000${BOND_DENOM} -y --output json)

        if submit_and_wait "$TX_RES" "challenge verification"; then
            echo "  status right after challenge: $(content_status "$CH_CID")"
            wait_past_arbiter_window
            FINAL=$(content_status "$CH_CID")
            COMMITTED_AFTER_CH=$(committed_bond)
            echo "  status after arbiter window: $FINAL (committed=$COMMITTED_AFTER_CH)"

            if [ "$FINAL" == "FEDERATED_CONTENT_STATUS_VERIFIED" ]; then
                echo "  a challenge that produced no quorum produced no finding; verification stands"
                record_result "no-quorum CHALLENGED -> VERIFIED" "PASS"
            elif [ "$FINAL" == "FEDERATED_CONTENT_STATUS_UNRESOLVED" ]; then
                echo "  REGRESSION: one challenge fee permanently demoted verified content"
                record_result "no-quorum CHALLENGED -> VERIFIED" "FAIL"
            else
                echo "  Expected VERIFIED, got $FINAL"
                record_result "no-quorum CHALLENGED -> VERIFIED" "FAIL"
            fi

            CH_RELEASED=$(commitment_released "$CH_CID")
            echo "  record $CH_CID: commitment_released=$CH_RELEASED"
            if [ "$CH_RELEASED" == "true" ]; then
                record_result "challenged no-quorum frees the commitment" "PASS"
            else
                echo "  LEAK: record still reads commitment_released=false"
                record_result "challenged no-quorum frees the commitment" "FAIL"
            fi
        else
            echo "  Could not challenge"
            record_result "no-quorum CHALLENGED -> VERIFIED" "FAIL"
            record_result "challenged no-quorum frees the commitment" "FAIL"
        fi
    else
        echo "  Could not verify"
        record_result "no-quorum CHALLENGED -> VERIFIED" "FAIL"
        record_result "challenged no-quorum frees the commitment" "FAIL"
    fi
else
    echo "  Could not submit content"
    record_result "no-quorum CHALLENGED -> VERIFIED" "FAIL"
    record_result "challenged no-quorum frees the commitment" "FAIL"
fi

# ========================================================================
# TEST 4: the verifier can still verify after repeated no-quorum disputes
# ========================================================================
echo ""
echo "--- TEST 4: verifier still usable after repeated disputes ---"

DISPUTE_ROUNDS=3
for i in $(seq 1 $DISPUTE_ROUNDS); do
    CID=$(make_disputed_content "loop$i")
    [ -z "$CID" ] && break
done
wait_past_arbiter_window

FINAL_COMMITTED=$(committed_bond)
echo "  committed bond after $DISPUTE_ROUNDS disputes: $FINAL_COMMITTED"

PROBE_BODY="post-dispute probe"
PROBE_HASH=$(sha256_base64 "$PROBE_BODY")
TX_RES=$($BINARY tx federation submit-federated-content \
    "$PEER" "nq-probe-001" "blog_post" \
    "@probe@${PEER}" "Probe" "Probe" \
    "$PROBE_BODY" "https://${PEER}/users/probe/statuses/1" "1700050200" \
    --content-hash "$PROBE_HASH" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)

if submit_and_wait "$TX_RES" "submit probe content"; then
    PROBE_CID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="federated_content_received").attributes[] | select(.key=="content_id").value' | tr -d '"')
    TX_RES=$($BINARY tx federation verify-content "$PROBE_CID" --content-hash "$PROBE_HASH" \
        --from $VERIFIER_A --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    if submit_and_wait "$TX_RES" "probe verify"; then
        echo "  verifier still able to verify after $DISPUTE_ROUNDS disputes"
        record_result "verifier usable after repeated disputes" "PASS"
    else
        echo "  verifier locked out -- commitments are leaking"
        record_result "verifier usable after repeated disputes" "FAIL"
    fi
else
    echo "  Could not submit probe content"
    record_result "verifier usable after repeated disputes" "FAIL"
fi

# ========================================================================
# TEST 5-8: P0.4 ListFederatedContent filters + match-space pagination
# ========================================================================
echo ""
echo "--- TEST 5: list-federated-content --peer-id filter ---"

BY_PEER=$($BINARY query federation list-federated-content --peer-id "$PEER" --output json 2>/dev/null)
if echo "$BY_PEER" | jq -e '.content' >/dev/null 2>&1; then
    OFF_PEER=$(echo "$BY_PEER" | jq -r "[.content[] | select(.peer_id != \"$PEER\")] | length")
    COUNT=$(echo "$BY_PEER" | jq '.content | length')
    echo "  $COUNT rows, $OFF_PEER of them from another peer"
    if [ "$OFF_PEER" == "0" ] && [ "$COUNT" -gt 0 ]; then
        record_result "list-federated-content --peer-id" "PASS"
    else
        record_result "list-federated-content --peer-id" "FAIL"
    fi
else
    echo "  query failed: $(echo "$BY_PEER" | head -c 200)"
    record_result "list-federated-content --peer-id" "FAIL"
fi

echo ""
echo "--- TEST 6: --status filter takes the full enum name ---"

BY_STATUS=$($BINARY query federation list-federated-content \
    --status FEDERATED_CONTENT_STATUS_UNRESOLVED --output json 2>/dev/null)
if echo "$BY_STATUS" | jq -e '.content' >/dev/null 2>&1; then
    OFF=$(echo "$BY_STATUS" | jq -r '[.content[] | select(.status != "FEDERATED_CONTENT_STATUS_UNRESOLVED")] | length')
    COUNT=$(echo "$BY_STATUS" | jq '.content | length')
    echo "  $COUNT UNRESOLVED rows, $OFF with the wrong status"
    if [ "$OFF" == "0" ] && [ "$COUNT" -gt 0 ]; then
        record_result "list-federated-content --status" "PASS"
    else
        record_result "list-federated-content --status" "FAIL"
    fi
else
    echo "  query failed: $(echo "$BY_STATUS" | head -c 200)"
    record_result "list-federated-content --status" "FAIL"
fi

echo ""
echo "--- TEST 7: an unknown status name is rejected, not silently ignored ---"

BAD=$($BINARY query federation list-federated-content --status VERIFIED --output json 2>&1)
if echo "$BAD" | grep -qi "unknown status"; then
    echo "  rejected with a useful message"
    record_result "--status rejects a bad enum name" "PASS"
else
    echo "  expected a rejection, got: $(echo "$BAD" | head -c 200)"
    record_result "--status rejects a bad enum name" "FAIL"
fi

echo ""
echo "--- TEST 8: filtered pagination counts MATCHES, not rows scanned ---"
#
# offset/total are in match space: a caller can only observe matches, so an
# offset denominated in index entries walked would re-read rows it had
# already seen. Paging with offset += limit must reassemble the full set
# with no duplicates and no gaps.

PAGE_ALL=$($BINARY query federation list-federated-content --peer-id "$PEER" \
    --status FEDERATED_CONTENT_STATUS_UNRESOLVED --page-count-total --output json 2>/dev/null)
TOTAL=$(echo "$PAGE_ALL" | jq -r '.pagination.total // "0"')
ALL_IDS=$(echo "$PAGE_ALL" | jq -r '[.content[].id] | sort | join(",")')
echo "  total=$TOTAL ids=[$ALL_IDS]"

if [ "$TOTAL" -gt 0 ] 2>/dev/null; then
    PAGED_IDS=""
    OFFSET=0
    while [ "$OFFSET" -lt "$TOTAL" ]; do
        PAGE=$($BINARY query federation list-federated-content --peer-id "$PEER" \
            --status FEDERATED_CONTENT_STATUS_UNRESOLVED \
            --page-offset "$OFFSET" --page-limit 1 --output json 2>/dev/null)
        PID=$(echo "$PAGE" | jq -r '.content[0].id // empty')
        [ -z "$PID" ] && break
        PAGED_IDS="$PAGED_IDS $PID"
        OFFSET=$((OFFSET + 1))
    done
    PAGED_SORTED=$(echo $PAGED_IDS | tr ' ' '\n' | grep -v '^$' | sort | tr '\n' ',' | sed 's/,$//')
    echo "  paged ids=[$PAGED_SORTED]"
    if [ "$PAGED_SORTED" == "$ALL_IDS" ]; then
        echo "  one-row pages reassemble the full match set exactly"
        record_result "filtered pagination is in match space" "PASS"
    else
        echo "  paging lost or duplicated rows"
        record_result "filtered pagination is in match space" "FAIL"
    fi
else
    echo "  no UNRESOLVED rows to page"
    record_result "filtered pagination is in match space" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo ""
echo "========================================"
echo "NO-QUORUM SETTLEMENT TEST SUMMARY"
echo "========================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-48s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo "----------------------------------------"
echo "  PASSED: $PASS_COUNT"
echo "  FAILED: $FAIL_COUNT"
echo "========================================"

[ $FAIL_COUNT -eq 0 ]
