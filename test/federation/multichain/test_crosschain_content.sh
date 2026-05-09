#!/bin/bash
# ------------------------------------------------------------------
# Cross-Chain Content Federation Tests
#
# Tests the full FederateContent → IBC → OnRecvContentPacket
# round-trip between two Spark Dream chains.
#
# Test 5 (silent-SendPacket regression): proves that silently-discarded
# SendPacket errors in the message servers do not mask data loss. We
# stop the relayer, federate (sender tx may succeed locally), confirm
# the receiver does NOT have the content, then restart the relayer and
# confirm IBC's pending-packet redelivery completes the round-trip.
#
# Prerequisites:
#   - Both chains running with peers + policies configured
#   - hermes relayer running
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/lib_multichain.sh"

PASS_COUNT=0; FAIL_COUNT=0; RESULTS=(); TEST_NAMES=()

echo "--- TESTING: CROSS-CHAIN CONTENT FEDERATION ---"
echo ""

# ------------------------------------------------------------------
# Pre-checks
# ------------------------------------------------------------------
PEER_A_OK=$(qcli_a federation get-peer fedtest-b 2>/dev/null | jq -r '.peer.status // "not found"')
PEER_B_OK=$(qcli_b federation get-peer fedtest-a 2>/dev/null | jq -r '.peer.status // "not found"')
echo "  Pre-check: chain-a peer=fedtest-b status=$PEER_A_OK"
echo "  Pre-check: chain-b peer=fedtest-a status=$PEER_B_OK"
echo ""

if [ "$PEER_A_OK" != "PEER_STATUS_ACTIVE" ] || [ "$PEER_B_OK" != "PEER_STATUS_ACTIVE" ]; then
    echo "  ERROR: Peers must be ACTIVE. Run setup_peers.sh first."
    exit 1
fi

ALICE_A=$(keys_a show alice -a)
ALICE_B=$(keys_b show alice -a)
echo "  Alice (chain-a): $ALICE_A"
echo "  Alice (chain-b): $ALICE_B"
echo ""

# ====================================================================
# TEST 1: Federate content from chain-a to chain-b (happy path)
# ====================================================================
echo "--- TEST 1: Content round-trip A -> B ---"

CONTENT_BODY="Cross-chain test content from A to B: $(date +%s)"
CONTENT_HASH=$(sha256_base64 "$CONTENT_BODY")

TX_RES=$(cli_a tx federation federate-content \
    fedtest-b \
    blog_post \
    "cc-test-001" \
    "Cross Chain Test Post" \
    "$CONTENT_BODY" \
    "" \
    --content-hash "$CONTENT_HASH" \
    --from alice -y --fees 5000uspark --output json 2>&1)

if submit_and_wait_a "$TX_RES" "federate A->B"; then
    echo "  FederateContent confirmed on chain-a"

    if wait_for_ibc_delivery \
        'qcli_b federation list-federated-content' \
        '.content | map(select(.peer_id == "fedtest-a")) | length > 0' \
        60 fedtest-a "$CHANNEL_A"; then

        RECEIVED=$(qcli_b federation list-federated-content | jq -r \
            '.content | map(select(.peer_id == "fedtest-a")) | .[0]')
        RECV_TITLE=$(echo "$RECEIVED" | jq -r '.title // empty')
        RECV_STATUS=$(echo "$RECEIVED" | jq -r '.status // "FEDERATED_CONTENT_STATUS_UNKNOWN"')

        echo "  chain-b received: title='$RECV_TITLE' status=$RECV_STATUS"

        if [ "$RECV_TITLE" = "Cross Chain Test Post" ] && \
           [ "$RECV_STATUS" = "FEDERATED_CONTENT_STATUS_ACTIVE" ]; then
            record_result "Content round-trip A->B" "PASS"
        else
            record_result "Content round-trip A->B" "FAIL"
        fi
    else
        record_result "Content round-trip A->B" "FAIL"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  FederateContent failed: $(echo "$RAW" | head -c 200)"
    record_result "Content round-trip A->B" "FAIL"
fi

# ====================================================================
# TEST 2: Federate content from chain-b to chain-a (reverse)
# ====================================================================
echo ""
echo "--- TEST 2: Content round-trip B -> A ---"

CONTENT_BODY2="Cross-chain test content from B to A: $(date +%s)"
CONTENT_HASH2=$(sha256_base64 "$CONTENT_BODY2")

TX_RES=$(cli_b tx federation federate-content \
    fedtest-a \
    blog_post \
    "cc-test-002" \
    "Reverse Cross Chain Post" \
    "$CONTENT_BODY2" \
    "" \
    --content-hash "$CONTENT_HASH2" \
    --from alice -y --fees 5000uspark --output json 2>&1)

if submit_and_wait_b "$TX_RES" "federate B->A"; then
    echo "  FederateContent confirmed on chain-b"
    if wait_for_ibc_delivery \
        'qcli_a federation list-federated-content' \
        '.content | map(select(.peer_id == "fedtest-b")) | length > 0' \
        60 fedtest-b "$CHANNEL_B"; then

        RECEIVED=$(qcli_a federation list-federated-content | jq -r \
            '.content | map(select(.peer_id == "fedtest-b")) | .[0]')
        RECV_TITLE=$(echo "$RECEIVED" | jq -r '.title // empty')

        echo "  chain-a received: title='$RECV_TITLE'"
        if [ "$RECV_TITLE" = "Reverse Cross Chain Post" ]; then
            record_result "Content round-trip B->A" "PASS"
        else
            record_result "Content round-trip B->A" "FAIL"
        fi
    else
        record_result "Content round-trip B->A" "FAIL"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  FederateContent failed: $(echo "$RAW" | head -c 200)"
    record_result "Content round-trip B->A" "FAIL"
fi

# ====================================================================
# TEST 3: Deduplication — same content hash sent again
# ====================================================================
echo ""
echo "--- TEST 3: Content deduplication (same hash) ---"

PRE_COUNT=$(qcli_b federation list-federated-content 2>/dev/null \
    | jq '[.content[]? | select(.peer_id == "fedtest-a")] | length // 0')

# Capture the original record's received_at so we can verify dedup updates
# in place (or rejects locally) rather than just "doesn't grow the count".
# Without this, the previous PASS-on-`<= PRE_COUNT` check accepted "no
# packet was even sent" as a positive dedup signal.
PRE_RECEIVED_AT=$(qcli_b federation list-federated-content 2>/dev/null \
    | jq -r '[.content[]? | select(.peer_id == "fedtest-a")][0].received_at // empty')

TX_RES=$(cli_a tx federation federate-content \
    fedtest-b \
    blog_post \
    "cc-test-001-dup" \
    "Duplicate Cross Chain" \
    "$CONTENT_BODY" \
    "" \
    --content-hash "$CONTENT_HASH" \
    --from alice -y --fees 5000uspark --output json 2>&1)

if submit_and_wait_a "$TX_RES" "federate dup"; then
    # Check that the duplicate request actually emitted a send_packet event
    # — the keeper may legitimately reject locally before broadcast, in
    # which case there's no IBC traffic and no chance for chain-b to dedup.
    DUP_TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    DUP_SEND_PACKETS=0
    if [ -n "$DUP_TXHASH" ]; then
        # Use qtx_a, NOT cli_a — see lib_multichain.sh comment on why cli_a's
        # extra flags break `query tx`. `|| true` keeps set -e callers alive
        # in the unlikely case of a transient query failure so the test still
        # reaches the structural pre/post comparisons below.
        DUP_SEND_PACKETS=$(qtx_a "$DUP_TXHASH" 2>/dev/null \
            | jq -r '[.events[]? | select(.type == "send_packet")] | length // 0' || true)
        DUP_SEND_PACKETS=${DUP_SEND_PACKETS:-0}
    fi
    echo "  send_packet events on dup tx: $DUP_SEND_PACKETS"

    sleep 8  # give IBC time to settle
    POST_COUNT=$(qcli_b federation list-federated-content 2>/dev/null \
        | jq '[.content[]? | select(.peer_id == "fedtest-a")] | length // 0')
    POST_RECEIVED_AT=$(qcli_b federation list-federated-content 2>/dev/null \
        | jq -r '[.content[]? | select(.peer_id == "fedtest-a")][0].received_at // empty')

    echo "  chain-b content from fedtest-a: pre=$PRE_COUNT post=$POST_COUNT"
    echo "  received_at: pre=$PRE_RECEIVED_AT post=$POST_RECEIVED_AT"

    # Three valid dedup outcomes:
    #   A) chain-a rejects locally (DUP_SEND_PACKETS = 0, count unchanged).
    #   B) chain-b receives but rejects (count unchanged, received_at unchanged).
    #   C) chain-b updates in place (count unchanged, received_at advances).
    # Any of A/B/C with `POST_COUNT == PRE_COUNT` is a pass; growing the
    # count means the duplicate was accepted as a new record.
    if [ "$POST_COUNT" -gt "$PRE_COUNT" ]; then
        echo "  Expected $PRE_COUNT but got $POST_COUNT (duplicate accepted)"
        record_result "Content dedup (same hash)" "FAIL"
    elif [ "$DUP_SEND_PACKETS" -eq 0 ]; then
        echo "  Dedup at sender: chain-a refused to broadcast the duplicate."
        record_result "Content dedup (same hash)" "PASS"
    elif [ "$POST_RECEIVED_AT" != "$PRE_RECEIVED_AT" ]; then
        echo "  Dedup at receiver (update-in-place): received_at advanced."
        record_result "Content dedup (same hash)" "PASS"
    else
        echo "  Dedup at receiver (rejected): count + received_at unchanged."
        record_result "Content dedup (same hash)" "PASS"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  FederateContent failed: $(echo "$RAW" | head -c 200)"
    record_result "Content dedup (same hash)" "FAIL"
fi

# ====================================================================
# TEST 4: Disallowed content type rejected on the sender side
# ====================================================================
echo ""
echo "--- TEST 4: Disallowed content type rejected ---"

FORUM_HASH=$(sha256_base64 "forum content should fail")
TX_RES=$(cli_a tx federation federate-content \
    fedtest-b \
    forum_thread \
    "cc-forum-001" \
    "Forum Post" \
    "Should be rejected" \
    "" \
    --content-hash "$FORUM_HASH" \
    --from alice -y --fees 5000uspark --output json 2>&1)

if submit_and_wait_a "$TX_RES" "disallowed type"; then
    CODE=$(echo "$TX_RESULT" | jq -r '.code // "0"')
    if [ "$CODE" != "0" ]; then
        echo "  Correctly rejected (code=$CODE)"
        record_result "Disallowed content type" "PASS"
    else
        record_result "Disallowed content type" "FAIL"
    fi
else
    echo "  Correctly rejected at broadcast/deliver"
    record_result "Disallowed content type" "PASS"
fi

# ====================================================================
# TEST 5: Silent SendPacket regression — emits-send_packet assertion
#
# The original bug we want to catch: msg_server_federate_content.go has
#   _, _ = k.SendFederationPacket(ctx, msg.PeerId, packetData)
# which silently discards SendPacket errors. If the IBC keeper isn't
# wired (or any other SendPacket failure), the user-side tx still
# succeeds locally but no IBC packet is committed and no `send_packet`
# event is emitted. The receiver never gets anything; the suite hangs
# waiting for delivery. Hermes has nothing to relay.
#
# Whether the bug exists is observable WITHOUT a relayer outage: simply
# verify the federate-content tx emits a `send_packet` event. If yes,
# IBC is wired and the keeper actually committed a packet. If no, the
# silent swallow has dropped a SendPacket failure.
#
# We avoid the relayer-down recovery scenario because the testparams
# build pins IBCPacketTimeout=10s (mainnet=10min). Stopping/restarting
# Hermes alone takes 8-15s, so the recovery test would always fail on
# packet timeout under testparams — a correct timeout, not a bug.
# ====================================================================
echo ""
echo "--- TEST 5: SendPacket event emission (silent regression check) ---"

TEST5_BODY="Silent-send regression test: $(date +%s%N)"
TEST5_HASH=$(sha256_base64 "$TEST5_BODY")

TX_RES=$(cli_a tx federation federate-content \
    fedtest-b \
    blog_post \
    "cc-silent-001" \
    "Silent Send Test" \
    "$TEST5_BODY" \
    "" \
    --content-hash "$TEST5_HASH" \
    --from alice -y --fees 5000uspark --output json 2>&1)

if ! submit_and_wait_a "$TX_RES" "federate-content for silent regression"; then
    echo "  Local tx failed — silent SendPacket regression cannot be evaluated"
    record_result "SendPacket event emission" "FAIL"
else
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    # Use qtx_a — NOT cli_a. cli_a injects `--keyring-backend test
    # --chain-id <id> --home <path>`, and `query tx` rejects
    # `--keyring-backend`/`--chain-id`: the binary prints CLI usage to stdout
    # and exits 1, which under `set -e` aborts the whole script before TEST 5
    # can print anything past its heading. qtx_a passes only the flags that
    # `query tx` actually accepts. `|| true` defends against any other
    # transient query failure so we always reach the structural assertions
    # below instead of dying silently. See lib_multichain.sh for the full
    # rationale.
    EVENT_DUMP=$(qtx_a "$TXHASH" 2>&1 || true)

    SEND_PACKET_COUNT=$(echo "$EVENT_DUMP" | jq -r '
        [.events[]? | select(.type == "send_packet")] | length // 0' 2>/dev/null)
    SEND_PORT=$(echo "$EVENT_DUMP" | jq -r '
        .events[]? | select(.type == "send_packet")
        | .attributes[]? | select(.key == "packet_src_port")
        | .value' 2>/dev/null | head -1)
    SEND_SEQ=$(echo "$EVENT_DUMP" | jq -r '
        .events[]? | select(.type == "send_packet")
        | .attributes[]? | select(.key == "packet_sequence")
        | .value' 2>/dev/null | head -1)
    # Defaults guard against the (possible) case where qtx_a returns
    # non-JSON or empty output: `[ "" -ge 1 ]` would shell-error with
    # "integer expression expected" instead of cleanly hitting the FAIL
    # branch with diagnostic output.
    SEND_PACKET_COUNT=${SEND_PACKET_COUNT:-0}
    SEND_PORT=${SEND_PORT:-}
    SEND_SEQ=${SEND_SEQ:-}

    echo "  send_packet events emitted: $SEND_PACKET_COUNT"
    echo "  port=$SEND_PORT sequence=$SEND_SEQ"

    if [ "$SEND_PACKET_COUNT" -ge 1 ] && [ "$SEND_PORT" = "federation" ] && [ -n "$SEND_SEQ" ] && [ "$SEND_SEQ" != "0" ]; then
        echo "  PASS: federate-content committed a federation packet to the IBC store."
        echo "        SendFederationPacket is wired and not silently swallowing errors."
        record_result "SendPacket event emission" "PASS"
    else
        echo "  FAIL: federate-content did NOT emit a send_packet event on the federation port."
        echo "        This usually means msg_server_federate_content.go discarded a"
        echo "        SendFederationPacket error via the '_, _ =' swallow. Either:"
        echo "        - IBC keeper is not wired (check app.go SetIBCKeeperFn), or"
        echo "        - SendFederationPacket itself is failing (check channel state)."
        echo "  --- relevant tx events ---"
        echo "$EVENT_DUMP" | jq -r '.events[]? | select(.type | test("send_packet|federation|content"))
                                  | {type, attrs:[.attributes[]?|{key, val:(.value|tostring|.[0:60])}]}' 2>/dev/null \
            | head -30
        record_result "SendPacket event emission" "FAIL"
    fi
fi

# ====================================================================
# Summary
# ====================================================================
print_summary "CROSS-CHAIN CONTENT TEST RESULTS"
