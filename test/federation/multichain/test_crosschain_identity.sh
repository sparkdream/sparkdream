#!/bin/bash
# ------------------------------------------------------------------
# Cross-Chain Identity Verification Tests
#
# Exercises the full identity-link IBC round-trip:
#   1. mc-linker-a (chain-a only) links to mc-owner-b's address on chain-b
#   2. chain-a sends IdentityVerificationPacket via IBC
#   3. chain-b stores PendingIdentityChallenge for mc-owner-b
#   4. mc-owner-b confirms on chain-b
#   5. chain-b sends IdentityVerificationConfirmPacket via IBC
#   6. chain-a marks the link VERIFIED
#
# Uses distinct keys per chain (set up by setup_chain_keys.sh) to ensure
# identity tests are not self-links from mirrored genesis accounts.
#
# Prerequisites:
#   - Both chains running with peers + policies configured
#   - hermes relayer running (start_relayer.sh)
#   - setup_chain_keys.sh has populated .chain_keys
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/lib_multichain.sh"

PASS_COUNT=0; FAIL_COUNT=0; RESULTS=(); TEST_NAMES=()

echo "--- TESTING: CROSS-CHAIN IDENTITY VERIFICATION ---"
echo ""

# ------------------------------------------------------------------
# Pre-checks
# ------------------------------------------------------------------
PEER_A_STATUS=$(qcli_a federation get-peer fedtest-b 2>/dev/null | jq -r '.peer.status // "not found"')
PEER_B_STATUS=$(qcli_b federation get-peer fedtest-a 2>/dev/null | jq -r '.peer.status // "not found"')

if [ "$PEER_A_STATUS" != "PEER_STATUS_ACTIVE" ] || [ "$PEER_B_STATUS" != "PEER_STATUS_ACTIVE" ]; then
    echo "  ERROR: Peers must be ACTIVE before running identity tests."
    echo "    chain-a peer fedtest-b: $PEER_A_STATUS"
    echo "    chain-b peer fedtest-a: $PEER_B_STATUS"
    exit 1
fi

if [ -z "$LINKER_A_ADDR" ] || [ -z "$LINKER_A2_ADDR" ] || [ -z "$OWNER_B_ADDR" ]; then
    echo "  ERROR: distinct chain keys not set up. Run setup_chain_keys.sh first."
    exit 1
fi

# Verify the addresses really differ between chains (catches setup regressions)
if keys_b show mc-linker-a -a >/dev/null 2>&1; then
    echo "  ERROR: mc-linker-a leaked into chain-b keyring — identity tests would self-link"
    exit 1
fi
if keys_a show mc-owner-b -a >/dev/null 2>&1; then
    echo "  ERROR: mc-owner-b leaked into chain-a keyring — identity tests would self-link"
    exit 1
fi

echo "  mc-linker-a  (chain-a) : $LINKER_A_ADDR"
echo "  mc-linker-a2 (chain-a) : $LINKER_A2_ADDR"
echo "  mc-owner-b   (chain-b) : $OWNER_B_ADDR"
echo ""

# ====================================================================
# TEST 1: Full identity link + verification round-trip
# ====================================================================
echo "--- TEST 1: Identity link + confirm + verify (full round-trip) ---"

echo "  mc-linker-a linking to mc-owner-b on chain-b..."
TX_RES=$(cli_a tx federation link-identity \
    fedtest-b \
    "$OWNER_B_ADDR" \
    --from mc-linker-a \
    -y \
    --fees 5000${BOND_DENOM} \
    --output json)

LINK_CREATED=false
if submit_and_wait_a "$TX_RES" "link identity"; then
    echo "  LinkIdentity tx confirmed on chain-a"

    # Proto3 omits zero-valued enums from JSON, so .link.status is absent for
    # IDENTITY_LINK_STATUS_UNVERIFIED (=0). Detect link presence by checking
    # for any populated field (linked_at is always set). VERIFIED status, when
    # reached, populates .link.verified_at.
    LINK_DATA=$(qcli_a federation get-identity-link "$LINKER_A_ADDR" fedtest-b 2>/dev/null || echo '{}')
    LINKED_AT=$(echo "$LINK_DATA" | jq -r '.link.linked_at // empty')
    VERIFIED_AT=$(echo "$LINK_DATA" | jq -r '.link.verified_at // empty')
    if [ -n "$VERIFIED_AT" ] && [ "$VERIFIED_AT" != "0" ]; then
        echo "  chain-a link status: VERIFIED (verified_at=$VERIFIED_AT)"
    elif [ -n "$LINKED_AT" ] && [ "$LINKED_AT" != "0" ]; then
        echo "  chain-a link status: UNVERIFIED (linked_at=$LINKED_AT)"
    else
        echo "  chain-a link status: MISSING"
    fi

    if [ -n "$LINKED_AT" ] && [ "$LINKED_AT" != "0" ]; then
        LINK_CREATED=true
    else
        echo "  LinkIdentity did not create a local link record"
        record_result "Full identity round-trip" "FAIL"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  LinkIdentity failed: $(echo "$RAW" | head -c 160)"
    record_result "Full identity round-trip" "FAIL"
fi

if [ "$LINK_CREATED" = true ]; then
    echo ""
    echo "  Waiting for Phase 1 challenge to arrive on chain-b..."
    if wait_for_ibc_delivery \
        "qcli_b federation get-pending-identity-challenge \"$OWNER_B_ADDR\" fedtest-a 2>/dev/null || echo '{}'" \
        '.challenge.claimed_address != null and .challenge.claimed_address != ""' \
        60 fedtest-a "$CHANNEL_A"; then

        CHALLENGE=$(qcli_b federation get-pending-identity-challenge "$OWNER_B_ADDR" fedtest-a 2>/dev/null)
        CLAIMED=$(echo "$CHALLENGE" | jq -r '.challenge.claimed_address // empty')
        echo "  chain-b has pending challenge for: $CLAIMED"

        echo ""
        echo "  mc-owner-b confirming identity link on chain-b..."
        TX_RES=$(cli_b tx federation confirm-identity-link \
            fedtest-a \
            --from mc-owner-b \
            -y \
            --fees 5000${BOND_DENOM} \
            --output json)

        if submit_and_wait_b "$TX_RES" "confirm identity"; then
            echo "  ConfirmIdentityLink tx confirmed on chain-b"
            echo ""
            echo "  Waiting for Phase 2 confirmation to arrive on chain-a..."
            # Wait for verified_at to be populated. Don't check .link.status —
            # proto3 omits the IDENTITY_LINK_STATUS_VERIFIED enum value from JSON
            # output the same way it omits UNVERIFIED. verified_at is the
            # reliable presence indicator.
            if wait_for_ibc_delivery \
                "qcli_a federation get-identity-link \"$LINKER_A_ADDR\" fedtest-b 2>/dev/null || echo '{}'" \
                '(.link.verified_at // "0") | tostring | . != "0" and . != ""' \
                60 fedtest-b "$CHANNEL_B"; then

                LINK_DATA=$(qcli_a federation get-identity-link "$LINKER_A_ADDR" fedtest-b)
                VERIFIED_AT=$(echo "$LINK_DATA" | jq -r '.link.verified_at // "0"')

                if [ -n "$VERIFIED_AT" ] && [ "$VERIFIED_AT" != "0" ]; then
                    echo "  Identity link VERIFIED at $VERIFIED_AT"
                    record_result "Full identity round-trip" "PASS"
                else
                    echo "  Unexpected: verified_at=$VERIFIED_AT after delivery"
                    record_result "Full identity round-trip" "FAIL"
                fi
            else
                echo "  Phase 2 confirmation did not arrive on chain-a within 60s"
                record_result "Full identity round-trip" "FAIL"
            fi
        else
            RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
            echo "  ConfirmIdentityLink failed: $(echo "$RAW" | head -c 160)"
            record_result "Full identity round-trip" "FAIL"
        fi
    else
        echo "  Phase 1 challenge did not arrive on chain-b within 60s"
        record_result "Full identity round-trip" "FAIL"
    fi
fi

# ====================================================================
# TEST 2: Remote identity already claimed prevention
# mc-linker-a2 tries to claim the same chain-b address.
# ====================================================================
echo ""
echo "--- TEST 2: Remote identity already claimed ---"

TX_RES=$(cli_a tx federation link-identity \
    fedtest-b \
    "$OWNER_B_ADDR" \
    --from mc-linker-a2 \
    -y \
    --fees 5000${BOND_DENOM} \
    --output json)

if submit_and_wait_a "$TX_RES" "already-claimed link"; then
    CODE=$(echo "$TX_RESULT" | jq -r '.code // "0"')
    if [ "$CODE" != "0" ]; then
        echo "  Already-claimed identity correctly rejected (code=$CODE)"
        record_result "Remote identity already claimed" "PASS"
    else
        echo "  Should have been rejected but tx succeeded"
        record_result "Remote identity already claimed" "FAIL"
    fi
else
    # Failure at broadcast or in deliver-tx; either is a valid rejection
    echo "  Correctly rejected at broadcast/deliver"
    record_result "Remote identity already claimed" "PASS"
fi

# ====================================================================
# TEST 3: Resolve remote identity across chains
# ====================================================================
echo ""
echo "--- TEST 3: Resolve remote identity ---"

RESOLVE=$(qcli_a federation resolve-remote-identity fedtest-b "$OWNER_B_ADDR" 2>/dev/null)
RESOLVED_ADDR=$(echo "$RESOLVE" | jq -r '.local_address // empty')

echo "  Resolved $OWNER_B_ADDR → $RESOLVED_ADDR"
if [ "$RESOLVED_ADDR" = "$LINKER_A_ADDR" ]; then
    record_result "Resolve remote identity" "PASS"
elif [ -z "$RESOLVED_ADDR" ] || [ "$RESOLVED_ADDR" = "null" ]; then
    echo "  Resolution returned empty (TEST 1 may have failed; skipping)"
    record_result "Resolve remote identity" "FAIL"
else
    echo "  Resolved to unexpected address: $RESOLVED_ADDR"
    record_result "Resolve remote identity" "FAIL"
fi

# ====================================================================
# TEST 4: Confirm without pending challenge fails
# Use alice-on-chain-b (different account, no challenge) to ensure
# we exercise the no-pending-challenge code path cleanly.
# ====================================================================
echo ""
echo "--- TEST 4: Confirm without pending challenge fails ---"

TX_RES=$(cli_b tx federation confirm-identity-link \
    fedtest-a \
    --from alice \
    -y \
    --fees 5000${BOND_DENOM} \
    --output json)

if submit_and_wait_b "$TX_RES" "confirm-no-challenge"; then
    CODE=$(echo "$TX_RESULT" | jq -r '.code // "0"')
    if [ "$CODE" != "0" ]; then
        echo "  Correctly rejected (code=$CODE)"
        record_result "Confirm without challenge" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Confirm without challenge" "FAIL"
    fi
else
    echo "  Correctly rejected at broadcast/deliver"
    record_result "Confirm without challenge" "PASS"
fi

# ====================================================================
# Summary
# ====================================================================
print_summary "CROSS-CHAIN IDENTITY TEST RESULTS"
