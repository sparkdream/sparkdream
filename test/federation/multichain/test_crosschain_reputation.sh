#!/bin/bash
# ------------------------------------------------------------------
# Cross-Chain Reputation Attestation Tests
#
# Tests the full RequestReputationAttestation → IBC query →
# OnRecvReputationQueryPacket → acknowledgement → OnAckReputationQuery
# round-trip between two Spark Dream chains.
#
# Flow:
#   1. Alice on chain-a requests reputation attestation for Bob on chain-b
#   2. Chain-a sends ReputationQueryPacket via IBC
#   3. Chain-b looks up Bob's trust level and returns it as ack
#   4. Chain-a stores discounted ReputationAttestation (capped at PROVISIONAL)
#
# Prerequisites:
#   - Both chains running with peers + policies configured
#   - IBC relayer running (Hermes)
#   - Bob must be a rep member on chain-b
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/lib_multichain.sh"

PASS_COUNT=0; FAIL_COUNT=0; RESULTS=(); TEST_NAMES=()

echo "--- TESTING: CROSS-CHAIN REPUTATION ATTESTATION ---"
echo ""

# Quick pre-check
PEER_A_STATUS=$(qcli_a federation get-peer fedtest-b 2>/dev/null | jq -r '.peer.status // "not found"')
PEER_B_STATUS=$(qcli_b federation get-peer fedtest-a 2>/dev/null | jq -r '.peer.status // "not found"')

if [ "$PEER_A_STATUS" != "PEER_STATUS_ACTIVE" ] || [ "$PEER_B_STATUS" != "PEER_STATUS_ACTIVE" ]; then
    echo "  ERROR: Peers must be ACTIVE."
    exit 1
fi

# Verify policy accepts reputation attestations
ATTEST_ENABLED=$(qcli_a federation get-peer-policy fedtest-b 2>/dev/null | jq -r '.policy.accept_reputation_attestations // false')
echo "  Reputation attestations enabled on chain-a policy: $ATTEST_ENABLED"
echo ""

# Get addresses. Prefer the chain-b-only owner address from setup_chain_keys.sh
# so this test exercises a known rep member on chain-b without depending on the
# mirrored "bob" key (which may have a different trust level after identity tests).
ALICE_A=$(keys_a show alice -a)
TARGET_B="$OWNER_B_ADDR"
if [ -z "$TARGET_B" ]; then
    TARGET_B=$(keys_b show bob -a 2>/dev/null || echo "")
    if [ -z "$TARGET_B" ] || [ "$TARGET_B" = "null" ]; then
        echo "  ERROR: no chain-b target address available (run setup_chain_keys.sh)"
        exit 1
    fi
fi
echo "  Alice (chain-a):  $ALICE_A"
echo "  Target (chain-b): $TARGET_B"
echo ""

# ====================================================================
# TEST 1: Request reputation attestation round-trip
# ====================================================================
echo "--- TEST 1: Reputation attestation A -> B round-trip ---"

# Check target's trust level on chain-b first (so we know what to expect)
TARGET_TRUST=$(qcli_b rep get-member "$TARGET_B" 2>/dev/null | jq -r '.member.trust_level // 0')
echo "  Target trust level on chain-b: $TARGET_TRUST"

TX_RES=$(cli_a tx federation request-reputation-attestation \
    fedtest-b \
    "$TARGET_B" \
    --from alice \
    -y \
    --fees 5000uspark \
    --output json 2>&1)

if submit_and_wait_a "$TX_RES" "request rep attestation"; then
    echo "  RequestReputationAttestation tx confirmed on chain-a"

    # Pull the txhash and confirm a send_packet event was emitted. If the IBC
    # keeper ever silently drops the packet (the regression that prompted
    # x/federation/keeper/keeper_ibc.go's hard error), the request tx
    # succeeds but no IBC event is produced — Hermes has nothing to relay.
    # Catching that here gives a much clearer failure than waiting 60s for
    # an attestation that will never arrive.
    REQUEST_TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -n "$REQUEST_TXHASH" ]; then
        # Use qtx_a — NOT cli_a. cli_a injects `--keyring-backend test
        # --chain-id <id> --home <path>`, and `query tx` rejects
        # `--keyring-backend`/`--chain-id`: the binary prints CLI usage to
        # stdout (instead of JSON) and exits 1. The pipe-to-jq used to
        # absorb that — jq fails to parse "Usage:..." and produces empty
        # output, defaulting SEND_PACKETS to 0 and triggering a *false*
        # "no send_packet event" failure even when the packet was actually
        # emitted. qtx_a passes only the flags `query tx` accepts.
        SEND_PACKETS=$(qtx_a "$REQUEST_TXHASH" 2>/dev/null \
            | jq -r '[.events[]? | select(.type == "send_packet")] | length // 0' || true)
        SEND_PACKETS=${SEND_PACKETS:-0}
        echo "  send_packet events on request tx: $SEND_PACKETS"
        if [ "$SEND_PACKETS" -lt 1 ]; then
            echo "  [FAIL] No send_packet event — IBC packet was not actually sent."
            echo "         (NOTE: msg_server_request_reputation_attestation.go now"
            echo "          emits a 'federation_packet_send_failed' event on this path —"
            echo "          inspect the tx events for that signal to confirm.)"
            record_result "Reputation attestation round-trip" "FAIL"
            FAILED_PRE_DELIVERY=1
        fi
    fi

    if [ "${FAILED_PRE_DELIVERY:-0}" -ne 1 ]; then
        echo "  Waiting for IBC round-trip + attestation storage on chain-a..."
        if ! wait_for_ibc_delivery \
            "qcli_a federation get-reputation-attestation \"$ALICE_A\" \"$TARGET_B\" 2>/dev/null || echo '{}'" \
            '.attestation.remote_address != null and .attestation.remote_address != ""' \
            60 fedtest-a "$CHANNEL_A"; then
            echo "  Attestation did not appear on chain-a within 60s"
        fi

        ATTEST_DATA=$(qcli_a federation get-reputation-attestation "$ALICE_A" "$TARGET_B" 2>/dev/null)
        ATTEST_EXISTS=$(echo "$ATTEST_DATA" | jq -r '.attestation.remote_address // empty')

        if [ -n "$ATTEST_EXISTS" ]; then
            REMOTE_TRUST=$(echo "$ATTEST_DATA" | jq -r '.attestation.remote_trust_level // 0')
            LOCAL_CREDIT=$(echo "$ATTEST_DATA" | jq -r '.attestation.local_trust_credit // 0')
            EXPIRES=$(echo "$ATTEST_DATA" | jq -r '.attestation.expires_at // 0')

            echo "  Attestation stored:"
            echo "    remote_address: $ATTEST_EXISTS"
            echo "    remote_trust_level: $REMOTE_TRUST"
            echo "    local_trust_credit: $LOCAL_CREDIT (capped at PROVISIONAL=1)"
            echo "    expires_at: $EXPIRES"

            # Discount cap: local_trust_credit should be ≤ 1
            # Default to 0 if empty so arithmetic doesn't error under set -e.
            : "${LOCAL_CREDIT:=0}"
            if [ "$LOCAL_CREDIT" -le 1 ] 2>/dev/null && [ "$EXPIRES" != "0" ]; then
                record_result "Reputation attestation round-trip" "PASS"
            else
                echo "  Unexpected: credit=$LOCAL_CREDIT (expected ≤1), expires=$EXPIRES"
                record_result "Reputation attestation round-trip" "FAIL"
            fi
        else
            # No attestation stored after 60s. Previously we recorded PASS here
            # with the rationale "the IBC flow completed — an empty response
            # is valid". That assertion was too permissive: the same outcome
            # occurs when the OnRecvReputationQueryPacket / OnAck handler
            # silently fails. Inspect the target's trust level on chain-b: if
            # they're a rep member, we DO expect an attestation to arrive.
            REMOTE_IS_MEMBER=$(qcli_b rep get-member "$TARGET_B" 2>/dev/null \
                | jq -r '.member.trust_level // empty')
            if [ -n "$REMOTE_IS_MEMBER" ]; then
                echo "  [FAIL] Target IS a rep member (trust=$REMOTE_IS_MEMBER) but no attestation arrived"
                record_result "Reputation attestation round-trip" "FAIL"
            else
                # Genuine "target is not a rep member" path: chain-b's handler
                # legitimately returns no attestation. Mark SKIP, not PASS,
                # to make the gap visible in the summary.
                echo "  [SKIP] Target is not a rep member on chain-b — no attestation expected"
                record_result "Reputation attestation round-trip" "SKIP"
            fi
        fi
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    CODE=$(echo "$TX_RESULT" | jq -r '.code // empty' 2>/dev/null)
    echo "  RequestReputationAttestation failed (code=$CODE): $(echo "$RAW" | head -c 120)"

    # May fail if attestations not enabled
    if echo "$RAW" | grep -qi "not.*accept\|not supported"; then
        echo "  Attestations not enabled on policy (expected)"
        record_result "Reputation attestation round-trip" "PASS"
    else
        record_result "Reputation attestation round-trip" "FAIL"
    fi
fi

# ====================================================================
# TEST 2: Reputation attestation on non-IBC peer rejected
#
# This must exercise an ACTUAL non-IBC peer-type (e.g. ACTIVITYPUB) to
# distinguish the "wrong kind of peer" error path from the "unknown peer"
# error path covered by TEST 3. If setup_peers.sh registered an
# ACTIVITYPUB peer named "mastodon.example", we use it; otherwise we mark
# this case SKIP so it doesn't masquerade as TEST 3's coverage.
# ====================================================================
echo ""
echo "--- TEST 2: Rep attestation on non-IBC peer rejected ---"

NONIBC_PEER_ID="mastodon.example"
# `|| true` is REQUIRED here. The whole point of this query is to detect
# whether an ActivityPub peer happens to be registered for the SKIP-vs-RUN
# branch below. When it isn't (the normal case), `qcli_a federation get-peer`
# exits non-zero — `set -e` would then abort the entire phase silently right
# after printing the TEST 2 heading, masking the SKIP path as a hard failure.
NONIBC_PEER_DATA=$(qcli_a federation get-peer "$NONIBC_PEER_ID" 2>/dev/null || true)
NONIBC_PEER_TYPE=$(echo "$NONIBC_PEER_DATA" | jq -r '.peer.peer_type // empty' 2>/dev/null || true)

if [ -z "$NONIBC_PEER_TYPE" ] || [ "$NONIBC_PEER_TYPE" = "PEER_TYPE_SPARK_DREAM" ] || [ "$NONIBC_PEER_TYPE" = "null" ]; then
    echo "  No non-IBC peer registered as $NONIBC_PEER_ID (peer_type=${NONIBC_PEER_TYPE:-<missing>})"
    echo "  [SKIP] register an ACTIVITYPUB / AT_PROTO peer in setup_peers.sh to exercise this path"
    record_result "Rep on non-IBC peer" "SKIP"
else
    echo "  Found non-IBC peer $NONIBC_PEER_ID (peer_type=$NONIBC_PEER_TYPE)"
    TX_RES=$(cli_a tx federation request-reputation-attestation \
        "$NONIBC_PEER_ID" \
        "$TARGET_B" \
        --from alice \
        -y \
        --fees 5000uspark \
        --output json 2>&1)

    if submit_and_wait_a "$TX_RES" "rep non-IBC"; then
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty')
        if [ "$CODE" != "0" ] && echo "$RAW" | grep -qiE "not supported|not accept|not.*ibc|peer.*type"; then
            echo "  Non-IBC peer correctly rejected with type-aware error (code=$CODE)"
            record_result "Rep on non-IBC peer" "PASS"
        elif [ "$CODE" != "0" ]; then
            echo "  Rejected (code=$CODE) but error doesn't mention peer-type — verify keeper returns ErrReputationNotSupported"
            echo "  raw_log: $(echo "$RAW" | head -c 200)"
            record_result "Rep on non-IBC peer" "FAIL"
        else
            echo "  Should have been rejected"
            record_result "Rep on non-IBC peer" "FAIL"
        fi
    else
        echo "  Pre-confirm rejection (CLI rejected before broadcast) — acceptable"
        record_result "Rep on non-IBC peer" "PASS"
    fi
fi

# ====================================================================
# TEST 3: Reputation attestation on non-existent peer
# ====================================================================
echo ""
echo "--- TEST 3: Rep attestation on non-existent peer ---"

TX_RES=$(cli_a tx federation request-reputation-attestation \
    nonexistent.peer \
    "$TARGET_B" \
    --from alice \
    -y \
    --fees 5000uspark \
    --output json 2>&1)

if submit_and_wait_a "$TX_RES" "rep missing peer"; then
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  Non-existent peer correctly rejected (code=$CODE)"
        record_result "Rep on missing peer" "PASS"
    else
        record_result "Rep on missing peer" "FAIL"
    fi
else
    echo "  Correctly rejected"
    record_result "Rep on missing peer" "PASS"
fi

# ====================================================================
# Summary
# ====================================================================
print_summary "CROSS-CHAIN REPUTATION TEST RESULTS"
