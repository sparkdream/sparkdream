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
# Constraints (chosen so STEP 6 can verify cooldown enforcement before
# the group expires under it):
# - Term: 180s   (test runtime ~65s under parallel load — see 22*sleep3
#                + per-tx block time; 3x headroom for further slowdowns)
# - Spend Limit: 100uspark   (intentionally tiny so STEP 4's 200uspark trips it)
# - Cooldown: 3600s (1 hour)
echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Create Fort Knox",
  "summary": "Security test committee.",
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
      "term_duration": 180,
      "voting_period": 3600,
      "min_execution_period": 0,
      "max_spend_per_epoch": "100",
      "update_cooldown": 3600,
      "funding_weight": 0,
      "futarchy_enabled": false,
      "vote_threshold": "1",
      "policy_type": "threshold",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgSpendFromCommons",
        "/sparkdream.commons.v1.MsgUpdateGroupMembers"
      ]
    }
  ]
}' > "$PROPOSAL_DIR/create_fort_knox.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/create_fort_knox.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "[FAIL] ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi
echo "[ OK ] Proposal ID: $PROPOSAL_ID"

# Vote & Exec (This creation should succeed)
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3

# Verify Execution via proposal status — PROPOSAL_STATUS_EXECUTED is the
# canonical success marker.
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] Error: Failed to create Fort Knox (status=$PROP_STATUS)."
    EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json 2>/dev/null)
    echo "Raw: $(echo $EXEC_LOGS | jq -r '.raw_log // empty' 2>/dev/null)"
    exit 1
fi
echo "[ OK ] 'Fort Knox' created."

# Get Fort Knox Policy Address for checks
KNOX_INFO=$($BINARY query commons get-group "Fort Knox" --output json)
KNOX_POLICY=$(echo $KNOX_INFO | jq -r '.group.policy_address')
if [ -z "$KNOX_POLICY" ]; then
    echo "[FAIL] ERROR: Failed to get Fort Knox Policy Address."
    exit 1
fi
echo "Fort Knox Policy Address: $KNOX_POLICY"

echo "--- STEP 2: TEST PREMATURE RENEWAL (TIMING ATTACK) ---"
# Current Time: ~40s. Term End: ~200s.
echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Early Renewal",
  "summary": "Trying to renew too early.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Fort Knox",
      "new_members": ["'$DAVE_ADDR'"],
      "new_member_weights": ["1"]
    }
  ]
}' > "$PROPOSAL_DIR/early_renew.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/early_renew.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

# Get Premature Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "[FAIL] ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi
echo "[ OK ] Premature Proposal ID: $PROPOSAL_ID"

$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)

if echo "$EXEC_LOGS" | grep -q "current term has not expired yet"; then
    echo "[ OK ] SUCCESS: Premature renewal rejected."
else
    echo "[FAIL] FAILURE: Premature renewal succeeded!"
    echo "Raw: $(echo $EXEC_LOGS)"
    exit 1
fi

echo "--- STEP 3: THE 'EVE' ATTACK (UNAUTHORIZED ACCESS) ---"
# Fund the Eve address so proposal fees can be paid
$BINARY tx bank send alice $EVE_ADDR 5000000uspark --chain-id $CHAIN_ID -y --fees 5000uspark > /dev/null
sleep 3

# Eve tries to submit a proposal to update Fort Knox.
echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$EVE_ADDR'"],
  "title": "Hacker Update",
  "summary": "Eve takes over.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupConfig",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Fort Knox",
      "max_spend_per_epoch": "9999999"
    }
  ]
}' > "$PROPOSAL_DIR/eve_attack.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/eve_attack.json" --from eve -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
sleep 3

TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

# Ensure we actually got a hash before querying
if [ -z "$TX_HASH" ] || [ "$TX_HASH" == "null" ]; then
    echo "[FAIL] ERROR: Failed to parse TX Hash. Raw Output:"
    echo "Raw: $(echo $SUBMIT_RES)"
    exit 1
fi

TX_RES=$($BINARY query tx $TX_HASH --output json)

# Check for unauthorized error in the raw log
if echo "$TX_RES" | grep -q "unauthorized"; then
    echo "[ OK ] SUCCESS: Eve was blocked."
else
    echo "[FAIL] FAILURE: Eve submitted a proposal (or failed for wrong reason)!"
    echo "Raw: $(echo $TX_RES)"
    exit 1
fi

echo "--- STEP 4: SPENDING LIMIT VIOLATION ---"
# 1. Fund the Fort Knox treasury first
$BINARY tx bank send alice $KNOX_POLICY 1000uspark --chain-id $CHAIN_ID -y --fees 5000uspark > /dev/null
sleep 3

# 2. Try to spend 200uspark (Limit is 100uspark)
echo '{
  "policy_address": "'$KNOX_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Overspend",
  "summary": "Buying a yacht.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$KNOX_POLICY'",
      "recipient": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "200"}]
    }
  ]
}' > "$PROPOSAL_DIR/overspend.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/overspend.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

# Get Overspend Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "[FAIL] ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi
echo "[ OK ] Overspend Proposal ID: $PROPOSAL_ID"

$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)

if echo "$EXEC_LOGS" | grep -q "exceeds group limit"; then
    echo "[ OK ] SUCCESS: Spending limit enforced."
elif echo "$EXEC_LOGS" | grep -q "failed to execute message"; then
    echo "[ OK ] SUCCESS: Execution failed (Generic/Limit)."
else
    echo "[FAIL] FAILURE: Committee spent more than allowed limit!"
    echo "Raw: $(echo $EXEC_LOGS)"
    exit 1
fi

echo "--- STEP 5: FORBIDDEN MESSAGE (RECURSION ATTACK) ---"
# Try to submit a MsgExecuteProposal inside a proposal — a recursion attack
# where executing one proposal would dispatch another. ExecuteProposal is
# intentionally NOT in any council's allowed_messages allowlist, so the
# submit handler must reject this proposal at the AnteHandler /
# SubmitProposal stage.
echo '{
  "policy_address": "'$KNOX_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgExecuteProposal",
      "executor": "'$KNOX_POLICY'",
      "proposal_id": "1"
    }
  ],
  "metadata": "Recursion attack attempt"
}' > "$PROPOSAL_DIR/forbidden.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/forbidden.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)

# Either submission is rejected directly (broadcast error) or the tx is
# included with a non-zero code and a "not allowed for policy" raw_log.
SUBMIT_HASH=$(echo "$SUBMIT_RES" | jq -r '.txhash // empty' 2>/dev/null)
sleep 3
RAW_LOG=""
if [ -n "$SUBMIT_HASH" ]; then
    RAW_LOG=$($BINARY query tx "$SUBMIT_HASH" --output json 2>/dev/null | jq -r '.raw_log // empty' 2>/dev/null)
fi

if echo "$SUBMIT_RES$RAW_LOG" | grep -qiE "MsgExecuteProposal not allowed for policy|not allowed for policy"; then
    echo "[ OK ] SUCCESS: Forbidden message rejected at submission."
else
    echo "[FAIL] FAILURE: MsgExecuteProposal was not rejected!"
    echo "Broadcast: $SUBMIT_RES" | head -10
    echo "Tx raw_log: $RAW_LOG"
    exit 1
fi

echo "--- STEP 5b: NON-ALLOWLISTED EXTERNAL MESSAGE (cosmos.bank.v1beta1.MsgSend) ---"
# Independent allowlist invariant: even non-recursive external messages
# (e.g. raw bank.MsgSend) must be rejected when their type URL is not on
# the policy's allowed_messages list. STEP 5 tested the recursion-class
# attack; this step tests the "wrong external message" class so a future
# refactor that loosens the allowlist on one path doesn't accidentally
# loosen it on the other.
echo '{
  "policy_address": "'$KNOX_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Bypass spend gate via raw bank send",
  "summary": "Direct bank.MsgSend, not whitelisted on Fort Knox.",
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$KNOX_POLICY'",
      "to_address": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ],
  "metadata": "Out-of-allowlist external message"
}' > "$PROPOSAL_DIR/external_forbidden.json"

EXT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/external_forbidden.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
EXT_HASH=$(echo "$EXT_RES" | jq -r '.txhash // empty' 2>/dev/null)
sleep 3
EXT_LOG=""
if [ -n "$EXT_HASH" ]; then
    EXT_LOG=$($BINARY query tx "$EXT_HASH" --output json 2>/dev/null | jq -r '.raw_log // empty' 2>/dev/null)
fi

if echo "$EXT_RES$EXT_LOG" | grep -qiE "MsgSend not allowed for policy|not allowed for policy|not in allowed messages"; then
    echo "[ OK ] SUCCESS: Out-of-allowlist external bank.MsgSend rejected."
else
    echo "[FAIL] FAILURE: External bank.MsgSend bypassed the allowlist!"
    echo "Broadcast: $(echo "$EXT_RES" | head -c 400)"
    echo "Tx raw_log: $EXT_LOG"
    exit 1
fi

echo "--- STEP 5.5: SETUP DELEGATION (CREATE VICTIM GROUP) ---"
# Strategy: Create a fresh group 'Victim Group' that is PRE-WIRED to delegate
# authority to Fort Knox. This avoids the need for MsgUpdateGroupConfig.

echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Create Victim Group",
  "summary": "Target group delegated to Fort Knox.",
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
  ]
}' > "$PROPOSAL_DIR/create_victim.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/create_victim.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "[FAIL] ERROR: Failed to submit victim creation proposal."
    echo "Raw: $(echo $SUBMIT_RES)"
    exit 1
fi
echo "[ OK ] Create Victim Proposal ID: $PROPOSAL_ID"

# Vote (Alice & Bob are members of Commons Council)
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
sleep 3
# Check execution via proposal status
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] ERROR: Failed to create Victim Group (status=$PROP_STATUS)."
    EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
    EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json 2>/dev/null)
    echo "Raw: $(echo $EXEC_LOGS | jq -r '.raw_log // empty' 2>/dev/null)"
    exit 1
fi
echo "[ OK ] Victim Group Created."

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
  "metadata": "Mutiny attempt: Fort Knox tries to add Dave to Victim Group"
}' > "$PROPOSAL_DIR/cooldown_check.json"

# Submit
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/cooldown_check.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

# Get Cooldown Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "[FAIL] ERROR: Failed to submit proposal."
    echo "Raw: $(echo $SUBMIT_RES)"
    exit 1
fi
echo "[ OK ] Cooldown Proposal ID: $PROPOSAL_ID"

# Vote
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

echo "Votes cast. Attempting Execution..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)

# We expect a failure containing "cooldown active"
if echo "$EXEC_LOGS" | grep -q "cooldown active"; then
    echo "[ OK ] SUCCESS: Member update rejected due to cooldown."
else
    echo "[FAIL] FAILURE: Expected 'cooldown active' error."
    echo "Raw: $(echo $EXEC_LOGS)"
    exit 1
fi

echo "--- ALL SECURITY TESTS PASSED ---"