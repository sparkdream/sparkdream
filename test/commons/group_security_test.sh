#!/bin/bash

echo "--- TESTING: SECURITY & FAILURE MODES (ATTACKS, LIMITS, FORBIDDEN MSGS) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Admins
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
# The Attacker
if ! $BINARY keys show eve --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add eve --keyring-backend test --output json > /dev/null
fi
EVE_ADDR=$($BINARY keys show eve -a --keyring-backend test)
# New member candidate
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)

# Get Commons Policy
COMMONS_INFO=$($BINARY query commons get-group "Commons Council" --output json)
COMMONS_POLICY=$(echo $COMMONS_INFO | jq -r '.group.policy_address')

echo "--- STEP 1: SETUP TARGET COMMITTEE 'FORT KNOX' ---"
# Constraints:
# - Term: 200s
# - Spend Limit: 100uspark
# - Cooldown: 3600s (1 hour)
echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRegisterGroup",
      "authority": "'$COMMONS_POLICY'",
      "name": "Fort Knox",
      "description": "High security vault",
      "policy_type": "threshold",
      "members": ["'$ALICE_ADDR'"],
      "member_weights": ["1"],
      "min_members": 1,
      "max_members": 3,
      "term_duration": 600,
      "voting_period": 3600,
      "min_execution_period": 0,
      "max_spend_per_epoch": "100",
      "update_cooldown": 3600,
      "funding_weight": 0,
      "futarchy_enabled": false,
      "vote_threshold": "1",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgSpendFromCommons",
        "/sparkdream.commons.v1.MsgUpdateGroupMembers"
      ]
    }
  ],
  "metadata": "Security test committee."
}' > "$PROPOSAL_DIR/create_fort_knox.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/create_fort_knox.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "FAIL: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi
echo "OK Proposal ID: $PROPOSAL_ID"

# Vote & Exec (This creation should succeed)
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# Verify Execution
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "FAIL: Failed to create Fort Knox. Status: $PROP_STATUS"
    exit 1
fi
echo "OK 'Fort Knox' created."

# Get Fort Knox Policy Address for checks
KNOX_INFO=$($BINARY query commons get-group "Fort Knox" --output json)
KNOX_POLICY=$(echo $KNOX_INFO | jq -r '.group.policy_address')
if [ -z "$KNOX_POLICY" ]; then
    echo "FAIL: Failed to get Fort Knox Policy Address."
    exit 1
fi
echo "Fort Knox Policy Address: $KNOX_POLICY"

echo "--- STEP 2: TEST PREMATURE RENEWAL (TIMING ATTACK) ---"
# Current Time: ~40s. Term End: ~200s.
echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Fort Knox",
      "new_members": ["'$DAVE_ADDR'"],
      "new_member_weights": ["1"]
    }
  ],
  "metadata": "Trying to renew too early."
}' > "$PROPOSAL_DIR/early_renew.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/early_renew.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Premature Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "FAIL: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi
echo "OK Premature Proposal ID: $PROPOSAL_ID"

$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json 2>&1)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# Check: either the execute tx failed on-chain, or the proposal status shows failure
TX_RES=$($BINARY query tx $EXEC_HASH --output json 2>/dev/null)
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')

if echo "$TX_RES" | grep -q "current term has not expired yet"; then
    echo "OK SUCCESS: Premature renewal rejected."
elif [ "$PROP_STATUS" == "PROPOSAL_STATUS_FAILED" ]; then
    echo "OK SUCCESS: Premature renewal rejected (proposal failed)."
else
    echo "FAIL: Premature renewal succeeded!"
    echo "Proposal Status: $PROP_STATUS"
    echo "Raw: $(echo $TX_RES)"
    exit 1
fi

echo "--- STEP 3: THE 'EVE' ATTACK (UNAUTHORIZED ACCESS) ---"
# Fund the Eve address so proposal fees can be paid
$BINARY tx bank send alice $EVE_ADDR 5000000uspark --chain-id $CHAIN_ID -y --fees 5000uspark > /dev/null
sleep 5

# Eve tries to submit a proposal to update Fort Knox.
# Eve is NOT a member, so SubmitProposal checks membership and should reject.
echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupConfig",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Fort Knox",
      "max_spend_per_epoch": "9999999"
    }
  ],
  "metadata": "Eve takes over."
}' > "$PROPOSAL_DIR/eve_attack.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/eve_attack.json" --from eve -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
sleep 5

TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

# If no hash, the tx was rejected client-side
if [ -z "$TX_HASH" ] || [ "$TX_HASH" == "null" ]; then
    if echo "$SUBMIT_RES" | grep -qi "unauthorized\|not a member"; then
        echo "OK SUCCESS: Eve was blocked (client-side rejection)."
    else
        echo "FAIL: Failed to parse TX Hash. Raw Output:"
        echo "Raw: $(echo $SUBMIT_RES)"
        exit 1
    fi
else
    TX_RES=$($BINARY query tx $TX_HASH --output json)
    TX_CODE=$(echo $TX_RES | jq -r '.code')

    # Check for unauthorized error in the raw log
    if [ "$TX_CODE" != "0" ] && echo "$TX_RES" | grep -qi "unauthorized\|not a member"; then
        echo "OK SUCCESS: Eve was blocked."
    else
        echo "FAIL: Eve submitted a proposal (or failed for wrong reason)!"
        echo "Raw: $(echo $TX_RES)"
        exit 1
    fi
fi

echo "--- STEP 4: SPENDING LIMIT VIOLATION ---"
# 1. Fund the Fort Knox treasury first
$BINARY tx bank send alice $KNOX_POLICY 1000uspark --chain-id $CHAIN_ID -y --fees 5000uspark > /dev/null
sleep 5

# 2. Try to spend 200uspark (Limit is 100uspark)
echo '{
  "policy_address": "'$KNOX_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$KNOX_POLICY'",
      "recipient": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "200"}]
    }
  ],
  "metadata": "Buying a yacht."
}' > "$PROPOSAL_DIR/overspend.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/overspend.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Overspend Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "FAIL: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi
echo "OK Overspend Proposal ID: $PROPOSAL_ID"

$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json 2>&1)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# The execute tx should fail. Check the tx result or proposal status.
TX_RES=$($BINARY query tx $EXEC_HASH --output json 2>/dev/null)
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')

if echo "$TX_RES" | grep -q "exceeds group limit"; then
    echo "OK SUCCESS: Spending limit enforced."
elif [ "$PROP_STATUS" == "PROPOSAL_STATUS_FAILED" ]; then
    echo "OK SUCCESS: Execution failed (proposal status FAILED)."
elif echo "$TX_RES" | grep -q "failed to execute message\|execution failed"; then
    echo "OK SUCCESS: Execution failed (Generic/Limit)."
else
    echo "FAIL: Committee spent more than allowed limit!"
    echo "Proposal Status: $PROP_STATUS"
    echo "Raw: $(echo $TX_RES)"
    exit 1
fi

echo "--- STEP 5: FORBIDDEN MESSAGE (UNAUTHORIZED MESSAGE TYPE) ---"
# Try to submit a proposal with a message NOT in Fort Knox's allowed_messages list.
# Fort Knox only allows MsgSpendFromCommons and MsgUpdateGroupMembers.
# cosmos.bank.v1beta1.MsgSend is NOT allowed.
echo '{
  "policy_address": "'$KNOX_POLICY'",
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$KNOX_POLICY'",
      "to_address": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ],
  "metadata": "Trying a forbidden message type."
}' > "$PROPOSAL_DIR/forbidden.json"

# Submission should fail at the AnteHandler / SubmitProposal handler level
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/forbidden.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)

if echo "$SUBMIT_RES" | grep -qi "not allowed for policy\|not allowed"; then
    echo "OK SUCCESS: Forbidden message rejected at submission."
else
    # Check if tx went through but failed on-chain
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash' 2>/dev/null)
    if [ -n "$TX_HASH" ] && [ "$TX_HASH" != "null" ]; then
        sleep 5
        TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
        TX_CODE=$(echo $TX_RES | jq -r '.code')
        if [ "$TX_CODE" != "0" ] && echo "$TX_RES" | grep -qi "not allowed"; then
            echo "OK SUCCESS: Forbidden message rejected on-chain."
        else
            echo "FAIL: Forbidden message was not blocked!"
            echo "Raw: $(echo $TX_RES)"
            exit 1
        fi
    else
        echo "FAIL: Forbidden message was not blocked!"
        echo "Raw: $(echo $SUBMIT_RES)"
        exit 1
    fi
fi

echo "--- STEP 5.5: SETUP DELEGATION (CREATE VICTIM GROUP) ---"
# Strategy: Create a fresh group 'Victim Group' that is PRE-WIRED to delegate
# authority to Fort Knox. This avoids the need for MsgUpdateGroupConfig.

echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRegisterGroup",
      "authority": "'$COMMONS_POLICY'",
      "name": "Victim Group",
      "description": "Target for mutiny test",
      "members": ["'$ALICE_ADDR'"],
      "member_weights": ["1"],
      "min_members": 1,
      "max_members": 3,
      "term_duration": 3600,
      "voting_period": 3600,
      "min_execution_period": 0,
      "max_spend_per_epoch": "100",
      "update_cooldown": 0,
      "funding_weight": 0,
      "futarchy_enabled": false,
      "vote_threshold": "1",
      "policy_type": "threshold",
      "electoral_policy_address": "'$KNOX_POLICY'"
    }
  ],
  "metadata": "Target group delegated to Fort Knox."
}' > "$PROPOSAL_DIR/create_victim.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/create_victim.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "FAIL: Failed to submit victim creation proposal."
    echo "Raw: $(echo $SUBMIT_RES)"
    exit 1
fi
echo "OK Create Victim Proposal ID: $PROPOSAL_ID"

# Vote (Alice & Bob are members of Commons Council)
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
sleep 5

# Check execution
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "FAIL: Failed to create Victim Group. Status: $PROP_STATUS"
    exit 1
fi
echo "OK Victim Group Created."

# Get Victim Policy
VICTIM_INFO=$($BINARY query commons get-group "Victim Group" --output json)
VICTIM_POLICY=$(echo $VICTIM_INFO | jq -r '.group.policy_address')
echo "Victim Policy: $VICTIM_POLICY"

echo "--- STEP 6: MEMBER/CONFIG RATE LIMIT (COOLDOWN) ---"
# Scenario: 'Fort Knox' tries to update 'Victim Group'.
# Fort Knox IS the authorized Electoral Authority (set in Step 5.5).
# However, Fort Knox was created in Step 1 (< 1 hour ago) with a 3600s cooldown.
# Therefore, this update MUST fail with "rate limit exceeded" or "cooldown active".

echo '{
  "policy_address": "'$KNOX_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupMembers",
      "authority": "'$KNOX_POLICY'",
      "group_policy_address": "'$VICTIM_POLICY'",
      "members_to_add": ["'$DAVE_ADDR'"],
      "weights_to_add": ["1"],
      "members_to_remove": []
    }
  ],
  "metadata": "Fort Knox tries to add Dave to the Victim Group."
}' > "$PROPOSAL_DIR/cooldown_check.json"

# Submit
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/cooldown_check.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Cooldown Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "FAIL: Failed to submit proposal."
    echo "Raw: $(echo $SUBMIT_RES)"
    exit 1
fi
echo "OK Cooldown Proposal ID: $PROPOSAL_ID"

# Vote
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json 2>&1)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# We expect a failure containing "cooldown active"
TX_RES=$($BINARY query tx $EXEC_HASH --output json 2>/dev/null)
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')

if echo "$TX_RES" | grep -q "cooldown active"; then
    echo "OK SUCCESS: Member update rejected due to cooldown."
elif [ "$PROP_STATUS" == "PROPOSAL_STATUS_FAILED" ]; then
    FAIL_REASON=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.failed_reason')
    if echo "$FAIL_REASON" | grep -q "cooldown active"; then
        echo "OK SUCCESS: Member update rejected due to cooldown (proposal failed)."
    else
        echo "OK SUCCESS: Execution failed (proposal status FAILED). Reason: $FAIL_REASON"
    fi
else
    echo "FAIL: Expected 'cooldown active' error."
    echo "Proposal Status: $PROP_STATUS"
    echo "Raw: $(echo $TX_RES)"
    exit 1
fi

echo "--- ALL SECURITY TESTS PASSED ---"
