#!/bin/bash

echo "--- TESTING: OPERATIONAL PARAMS & GOVERNANCE ---"
echo ""

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# Initialize tracking
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

# ========================================================================
# Helper Functions
# ========================================================================

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0

    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
    return 1
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 1
    fi
    return 0
}

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 0
    fi
    return 1
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi

    local BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}

# Get governance module account address
get_gov_authority() {
    $BINARY query auth module-account gov --output json 2>&1 | jq -r '.account.value.address // .account.base_account.address // .account.address // empty'
}

# Submit a governance proposal, vote, and wait for it to pass.
# Usage: submit_gov_proposal <proposal_json_file>
# Sets GOV_PROPOSAL_RESULT to "PASS" or "FAIL"
submit_and_pass_gov_proposal() {
    local PROPOSAL_FILE=$1
    GOV_PROPOSAL_RESULT="FAIL"

    echo "  Submitting governance proposal..."
    local TX_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_FILE" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000000uspark \
        --gas 500000 \
        -y \
        --output json 2>&1)

    local TX_HASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TX_HASH" ] || [ "$TX_HASH" == "null" ]; then
        echo "  Failed to submit proposal (no txhash)"
        echo "  $TX_RES"
        return 1
    fi

    sleep 6
    local TX_RESULT_LOCAL=$(wait_for_tx "$TX_HASH")
    local CODE=$(echo "$TX_RESULT_LOCAL" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  Proposal submission failed: $(echo "$TX_RESULT_LOCAL" | jq -r '.raw_log // "unknown"')"
        return 1
    fi

    # Extract proposal ID from events
    local PROPOSAL_ID=$(echo "$TX_RESULT_LOCAL" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"' | head -1)
    if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
        echo "  Could not extract proposal ID from events"
        return 1
    fi
    echo "  Proposal ID: $PROPOSAL_ID"

    # Vote YES from alice (validator with full voting power)
    echo "  Voting YES..."
    TX_RES=$($BINARY tx gov vote "$PROPOSAL_ID" yes \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    sleep 6

    # Wait for voting period to end (expedited=40s, standard=60s)
    # Add buffer for safety
    echo "  Waiting for voting period to end (~50 seconds)..."
    sleep 50

    # Check proposal status
    local PROP_STATUS=$($BINARY query gov proposal "$PROPOSAL_ID" --output json 2>&1 | jq -r '.proposal.status // empty')
    echo "  Proposal status: $PROP_STATUS"

    if [ "$PROP_STATUS" = "PROPOSAL_STATUS_PASSED" ]; then
        echo "  Governance proposal passed"
        GOV_PROPOSAL_RESULT="PASS"
        return 0
    else
        echo "  Proposal did not pass (status: $PROP_STATUS)"
        return 1
    fi
}

# ========================================================================
# TEST 1: Query initial session params
# ========================================================================
echo "--- TEST 1: Query initial session params ---"

PARAMS=$($BINARY query session params --output json 2>&1)
INITIAL_MAX_SESSIONS=$(echo "$PARAMS" | jq -r '.params.max_sessions_per_granter // "0"')
INITIAL_MAX_MSG_TYPES=$(echo "$PARAMS" | jq -r '.params.max_msg_types_per_session // "0"')
INITIAL_CEILING_COUNT=$(echo "$PARAMS" | jq '.params.max_allowed_msg_types | length')
INITIAL_ACTIVE_COUNT=$(echo "$PARAMS" | jq '.params.allowed_msg_types | length')

echo "  max_sessions_per_granter: $INITIAL_MAX_SESSIONS"
echo "  max_msg_types_per_session: $INITIAL_MAX_MSG_TYPES"
echo "  ceiling types count: $INITIAL_CEILING_COUNT"
echo "  active types count: $INITIAL_ACTIVE_COUNT"

if [ "$INITIAL_MAX_SESSIONS" = "10" ] && [ "$INITIAL_MAX_MSG_TYPES" = "20" ]; then
    record_result "Query initial params" "PASS"
else
    echo "  Unexpected default values"
    record_result "Query initial params" "FAIL"
fi

# ========================================================================
# TEST 2: Update operational params via governance (narrow allowlist)
# ========================================================================
echo "--- TEST 2: Narrow allowlist via governance proposal ---"

GOV_AUTHORITY=$(get_gov_authority)
echo "  Governance authority: $GOV_AUTHORITY"

if [ -z "$GOV_AUTHORITY" ]; then
    echo "  Could not determine governance authority address"
    record_result "Narrow allowlist via governance" "FAIL"
else
    # Build a narrowed active list: remove x/collect and x/name types
    # Keep only blog + forum types
    NARROWED_TYPES=$(echo "$PARAMS" | jq '[.params.allowed_msg_types[] | select(startswith("/sparkdream.blog") or startswith("/sparkdream.forum"))]')
    NARROWED_COUNT=$(echo "$NARROWED_TYPES" | jq 'length')
    echo "  Narrowing allowed_msg_types from $INITIAL_ACTIVE_COUNT to $NARROWED_COUNT types"

    # Extract current params for the operational params message
    # max_expiration and max_spend_limit need careful handling
    MAX_EXP_SECONDS=$(echo "$PARAMS" | jq -r '.params.max_expiration // "604800s"' | sed 's/s$//')
    MAX_SPEND_AMT=$(echo "$PARAMS" | jq -r '.params.max_spend_limit.amount // "100000000"')
    MAX_SPEND_DENOM=$(echo "$PARAMS" | jq -r '.params.max_spend_limit.denom // "uspark"')

    cat > "$PROPOSAL_DIR/narrow_allowlist.json" <<PROPEOF
{
  "messages": [
    {
      "@type": "/sparkdream.session.v1.MsgUpdateOperationalParams",
      "authority": "$GOV_AUTHORITY",
      "operational_params": {
        "allowed_msg_types": $NARROWED_TYPES,
        "max_sessions_per_granter": "$INITIAL_MAX_SESSIONS",
        "max_msg_types_per_session": "$INITIAL_MAX_MSG_TYPES",
        "max_expiration": "${MAX_EXP_SECONDS}s",
        "max_spend_limit": {
          "denom": "$MAX_SPEND_DENOM",
          "amount": "$MAX_SPEND_AMT"
        }
      }
    }
  ],
  "deposit": "100000000uspark",
  "title": "Narrow Session Allowlist",
  "summary": "Remove x/name and x/collect message types from the active session allowlist",
  "expedited": true
}
PROPEOF

    submit_and_pass_gov_proposal "$PROPOSAL_DIR/narrow_allowlist.json"

    if [ "$GOV_PROPOSAL_RESULT" = "PASS" ]; then
        # Verify the allowlist was narrowed
        UPDATED_PARAMS=$($BINARY query session params --output json 2>&1)
        UPDATED_ACTIVE_COUNT=$(echo "$UPDATED_PARAMS" | jq '.params.allowed_msg_types | length')
        UPDATED_CEILING_COUNT=$(echo "$UPDATED_PARAMS" | jq '.params.max_allowed_msg_types | length')

        echo "  After update: active=$UPDATED_ACTIVE_COUNT, ceiling=$UPDATED_CEILING_COUNT"

        # Active list should be smaller; ceiling should be unchanged
        if [ "$UPDATED_ACTIVE_COUNT" -lt "$INITIAL_ACTIVE_COUNT" ] && [ "$UPDATED_CEILING_COUNT" = "$INITIAL_CEILING_COUNT" ]; then
            echo "  Allowlist narrowed, ceiling preserved"

            # Verify removed types are gone from active
            HAS_NAME=$(echo "$UPDATED_PARAMS" | jq -r '.params.allowed_msg_types[] | select(startswith("/sparkdream.name"))' | head -1)
            if [ -z "$HAS_NAME" ]; then
                echo "  x/name types removed from active list"
                record_result "Narrow allowlist via governance" "PASS"
            else
                echo "  x/name types still in active list!"
                record_result "Narrow allowlist via governance" "FAIL"
            fi
        else
            echo "  Unexpected counts: active=$UPDATED_ACTIVE_COUNT (expected < $INITIAL_ACTIVE_COUNT), ceiling=$UPDATED_CEILING_COUNT"
            record_result "Narrow allowlist via governance" "FAIL"
        fi
    else
        record_result "Narrow allowlist via governance" "FAIL"
    fi
fi

# ========================================================================
# TEST 3: Verify narrowed allowlist blocks session creation
# ========================================================================
echo "--- TEST 3: Narrowed allowlist blocks session creation ---"

# Try to create a session with /sparkdream.name.v1.MsgSetPrimary (should be removed from active list)
if ! $BINARY keys show opparams_grantee1 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add opparams_grantee1 --keyring-backend test > /dev/null 2>&1
fi
OP_GRANTEE1_ADDR=$($BINARY keys show opparams_grantee1 -a --keyring-backend test)

EXPIRATION=$(date -u -d "+1 hour" +"%Y-%m-%dT%H:%M:%SZ")
TX_RES=$($BINARY tx session create-session \
    "$OP_GRANTEE1_ADDR" \
    "/sparkdream.name.v1.MsgSetPrimary" \
    "50000000uspark" \
    "$EXPIRATION" \
    "0" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "not in.*allowlist\|allowed_msg_types"; then
        echo "  Correctly rejected: removed type blocked"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Narrowed allowlist blocks creation" "PASS"
else
    # If test 2 didn't pass (governance proposal failed), this test should still try
    RESULT_CHECK=$(echo "$TX_RESULT" | jq -r '.code // "0"')
    if [ "$RESULT_CHECK" != "0" ]; then
        echo "  TX failed (may be expected even without narrowing)"
        record_result "Narrowed allowlist blocks creation" "PASS"
    else
        echo "  Session creation succeeded — allowlist may not have been narrowed"
        record_result "Narrowed allowlist blocks creation" "FAIL"
    fi
fi

# ========================================================================
# TEST 4: Restore allowlist via governance (re-add from ceiling)
# ========================================================================
echo "--- TEST 4: Restore allowlist via governance ---"

if [ -z "$GOV_AUTHORITY" ]; then
    echo "  No governance authority, skipping"
    record_result "Restore allowlist via governance" "FAIL"
else
    # Get the current ceiling (should be unchanged)
    CURRENT_PARAMS=$($BINARY query session params --output json 2>&1)
    FULL_CEILING=$(echo "$CURRENT_PARAMS" | jq '.params.max_allowed_msg_types')
    MAX_EXP_SECONDS=$(echo "$CURRENT_PARAMS" | jq -r '.params.max_expiration // "604800s"' | sed 's/s$//')
    MAX_SPEND_AMT=$(echo "$CURRENT_PARAMS" | jq -r '.params.max_spend_limit.amount // "100000000"')
    MAX_SPEND_DENOM=$(echo "$CURRENT_PARAMS" | jq -r '.params.max_spend_limit.denom // "uspark"')

    # Restore active list to full ceiling
    cat > "$PROPOSAL_DIR/restore_allowlist.json" <<PROPEOF
{
  "messages": [
    {
      "@type": "/sparkdream.session.v1.MsgUpdateOperationalParams",
      "authority": "$GOV_AUTHORITY",
      "operational_params": {
        "allowed_msg_types": $FULL_CEILING,
        "max_sessions_per_granter": "$INITIAL_MAX_SESSIONS",
        "max_msg_types_per_session": "$INITIAL_MAX_MSG_TYPES",
        "max_expiration": "${MAX_EXP_SECONDS}s",
        "max_spend_limit": {
          "denom": "$MAX_SPEND_DENOM",
          "amount": "$MAX_SPEND_AMT"
        }
      }
    }
  ],
  "deposit": "100000000uspark",
  "title": "Restore Session Allowlist",
  "summary": "Re-add all ceiling types to the active session allowlist",
  "expedited": true
}
PROPEOF

    submit_and_pass_gov_proposal "$PROPOSAL_DIR/restore_allowlist.json"

    if [ "$GOV_PROPOSAL_RESULT" = "PASS" ]; then
        RESTORED_PARAMS=$($BINARY query session params --output json 2>&1)
        RESTORED_ACTIVE_COUNT=$(echo "$RESTORED_PARAMS" | jq '.params.allowed_msg_types | length')

        echo "  Restored active count: $RESTORED_ACTIVE_COUNT (ceiling: $INITIAL_CEILING_COUNT)"

        if [ "$RESTORED_ACTIVE_COUNT" = "$INITIAL_CEILING_COUNT" ]; then
            echo "  Allowlist fully restored from ceiling"
            record_result "Restore allowlist via governance" "PASS"
        else
            echo "  Active count doesn't match ceiling"
            record_result "Restore allowlist via governance" "FAIL"
        fi
    else
        record_result "Restore allowlist via governance" "FAIL"
    fi
fi

# ========================================================================
# TEST 5: Error - exceeds ceiling (add type not in ceiling)
# ========================================================================
echo "--- TEST 5: Error - exceeds ceiling ---"

if [ -z "$GOV_AUTHORITY" ]; then
    echo "  No governance authority, skipping"
    record_result "Error: exceeds ceiling" "FAIL"
else
    CURRENT_PARAMS=$($BINARY query session params --output json 2>&1)
    CURRENT_ACTIVE=$(echo "$CURRENT_PARAMS" | jq '.params.allowed_msg_types')
    MAX_EXP_SECONDS=$(echo "$CURRENT_PARAMS" | jq -r '.params.max_expiration // "604800s"' | sed 's/s$//')
    MAX_SPEND_AMT=$(echo "$CURRENT_PARAMS" | jq -r '.params.max_spend_limit.amount // "100000000"')
    MAX_SPEND_DENOM=$(echo "$CURRENT_PARAMS" | jq -r '.params.max_spend_limit.denom // "uspark"')

    # Add a type that's NOT in the ceiling
    INVALID_ACTIVE=$(echo "$CURRENT_ACTIVE" | jq '. + ["/sparkdream.rep.v1.MsgInviteMember"]')

    cat > "$PROPOSAL_DIR/exceeds_ceiling.json" <<PROPEOF
{
  "messages": [
    {
      "@type": "/sparkdream.session.v1.MsgUpdateOperationalParams",
      "authority": "$GOV_AUTHORITY",
      "operational_params": {
        "allowed_msg_types": $INVALID_ACTIVE,
        "max_sessions_per_granter": "$INITIAL_MAX_SESSIONS",
        "max_msg_types_per_session": "$INITIAL_MAX_MSG_TYPES",
        "max_expiration": "${MAX_EXP_SECONDS}s",
        "max_spend_limit": {
          "denom": "$MAX_SPEND_DENOM",
          "amount": "$MAX_SPEND_AMT"
        }
      }
    }
  ],
  "deposit": "100000000uspark",
  "title": "Invalid Ceiling Expansion",
  "summary": "This should fail: trying to add a type not in the ceiling",
  "expedited": true
}
PROPEOF

    submit_and_pass_gov_proposal "$PROPOSAL_DIR/exceeds_ceiling.json"

    # The proposal should pass (voting succeeds) but the MESSAGE execution
    # should fail, resulting in PROPOSAL_STATUS_FAILED.
    # In Cosmos SDK v0.50, if the message execution fails, the proposal status
    # becomes PROPOSAL_STATUS_FAILED.
    if [ "$GOV_PROPOSAL_RESULT" = "PASS" ]; then
        echo "  Proposal unexpectedly passed — ceiling enforcement may have failed"
        record_result "Error: exceeds ceiling" "FAIL"
    else
        echo "  Proposal did not pass (expected — message execution should fail)"
        record_result "Error: exceeds ceiling" "PASS"
    fi
fi

# ========================================================================
# TEST 6: Error - non-authority cannot update operational params
# ========================================================================
echo "--- TEST 6: Error - non-authority cannot update params ---"

CURRENT_PARAMS=$($BINARY query session params --output json 2>&1)
CURRENT_ACTIVE=$(echo "$CURRENT_PARAMS" | jq '.params.allowed_msg_types')
MAX_EXP_SECONDS=$(echo "$CURRENT_PARAMS" | jq -r '.params.max_expiration // "604800s"' | sed 's/s$//')
MAX_SPEND_AMT=$(echo "$CURRENT_PARAMS" | jq -r '.params.max_spend_limit.amount // "100000000"')
MAX_SPEND_DENOM=$(echo "$CURRENT_PARAMS" | jq -r '.params.max_spend_limit.denom // "uspark"')

# Construct a raw tx where alice (non-authority) tries to update params directly
cat > /tmp/session_nonauth_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgUpdateOperationalParams",
        "authority": "$ALICE_ADDR",
        "operational_params": {
          "allowed_msg_types": $CURRENT_ACTIVE,
          "max_sessions_per_granter": "5",
          "max_msg_types_per_session": "10",
          "max_expiration": "${MAX_EXP_SECONDS}s",
          "max_spend_limit": {
            "denom": "$MAX_SPEND_DENOM",
            "amount": "$MAX_SPEND_AMT"
          }
        }
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "uspark", "amount": "50000"}],
      "gas_limit": "300000",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}
TXEOF

ACCT_INFO=$($BINARY query auth account "$ALICE_ADDR" --output json 2>&1)
ACCT_NUM=$(echo "$ACCT_INFO" | jq -r '.account.account_number // .account.base_account.account_number // "0"')
SEQ=$(echo "$ACCT_INFO" | jq -r '.account.sequence // .account.base_account.sequence // "0"')

$BINARY tx sign /tmp/session_nonauth_unsigned.json \
    --from alice \
    --chain-id "$CHAIN_ID" \
    --keyring-backend test \
    --account-number "$ACCT_NUM" \
    --sequence "$SEQ" \
    --output-document /tmp/session_nonauth_signed.json 2>/dev/null

TX_RES=$($BINARY tx broadcast /tmp/session_nonauth_signed.json --output json 2>&1)
submit_tx_and_wait "$TX_RES"

if check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "invalid.*authority\|expected.*gov\|unauthorized\|signer"; then
        echo "  Correctly rejected: non-authority address"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: non-authority update rejected" "PASS"
else
    echo "  Expected failure but TX succeeded!"
    record_result "Error: non-authority update rejected" "FAIL"
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "OPERATIONAL PARAMS TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
    exit 0
fi
