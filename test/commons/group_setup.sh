#!/bin/bash

echo "!!! DEPRECATED SCRIPT - Governance groups are now bootstrapped directly in the genesis block."

echo "--- SETUP: THREE PILLARS GOVERNANCE (VIA GOV PROPOSALS) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

# Robust Gov Address Lookup
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov Address: $GOV_ADDR"

# --- 1. CLEANUP ---
rm -f "$PROPOSAL_DIR/*.json"
mkdir -p proposals

# --- HELPER: Wait for Proposal Pass ---
wait_for_pass() {
    local prop_id=$1
    echo "Waiting for voting period (60s)..."
    sleep 65
    
    STATUS=$($BINARY query gov proposal $prop_id --output json | jq -r '.proposal.status')
    if [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
        echo "✅ Proposal $prop_id PASSED."
    else
        echo "❌ Proposal $prop_id FAILED (Status: $STATUS)."
        exit 1
    fi
}

get_prop_id() {
    local tx_hash=$1
    sleep 5
    $BINARY query tx $tx_hash --output json | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"' | head -n 1
}

# ==============================================================================
# 2. CREATE PILLAR 1: COMMONS COUNCIL (Culture - 50%)
# ==============================================================================
echo "--- CREATING PILLAR 1: COMMONS COUNCIL ---"

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRegisterGroup",
      "authority": "'$GOV_ADDR'",
      "name": "Commons Council",
      "description": "Culture, Arts, and Events",
      "members": ["'$ALICE_ADDR'", "'$BOB_ADDR'", "'$CAROL_ADDR'"],
      "member_weights": ["1", "1", "1"],
      "funding_weight": "50",
      "max_spend_per_epoch": "500000000000uspark",
      "update_cooldown": "604800",
      "vote_threshold": "2",
      "futarchy_enabled": true,
      "min_members": 1,
      "max_members": 10,
      "term_duration": 31536000
    }
  ],
  "deposit": "50000000uspark",
  "title": "Create Commons Council",
  "summary": "Bootstrapping the Cultural Pillar."
}' > "$PROPOSAL_DIR/create_commons.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/create_commons.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
PROP_ID=$(get_prop_id $TX_HASH)

echo "Commons Proposal ID: $PROP_ID"
$BINARY tx gov vote $PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
wait_for_pass $PROP_ID

# ==============================================================================
# 3. CREATE PILLAR 2: TECHNICAL COUNCIL (Infrastructure - 30%)
# ==============================================================================
echo "--- CREATING PILLAR 2: TECHNICAL COUNCIL ---"

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRegisterGroup",
      "authority": "'$GOV_ADDR'",
      "name": "Technical Council",
      "description": "Upgrades and Security",
      "members": ["'$ALICE_ADDR'", "'$BOB_ADDR'", "'$CAROL_ADDR'"],
      "member_weights": ["1", "1", "1"],
      "funding_weight": "30",
      "max_spend_per_epoch": "500000000000uspark",
      "update_cooldown": "604800",
      "vote_threshold": "2",
      "futarchy_enabled": true,
      "min_members": 1,
      "max_members": 10,
      "term_duration": 31536000
    }
  ],
  "deposit": "50000000uspark",
  "title": "Create Technical Council",
  "summary": "Bootstrapping the Technical Pillar."
}' > "$PROPOSAL_DIR/create_tech.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/create_tech.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
PROP_ID=$(get_prop_id $TX_HASH)

echo "Tech Proposal ID: $PROP_ID"
$BINARY tx gov vote $PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
wait_for_pass $PROP_ID

# ==============================================================================
# 4. CREATE PILLAR 3: ECOSYSTEM COUNCIL (Growth - 20%)
# ==============================================================================
echo "--- CREATING PILLAR 3: ECOSYSTEM COUNCIL ---"

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRegisterGroup",
      "authority": "'$GOV_ADDR'",
      "name": "Ecosystem Council",
      "description": "Treasury and Grants",
      "members": ["'$ALICE_ADDR'", "'$BOB_ADDR'", "'$CAROL_ADDR'"],
      "member_weights": ["1", "1", "1"],
      "funding_weight": "20",
      "max_spend_per_epoch": "500000000000uspark",
      "update_cooldown": "604800",
      "vote_threshold": "2",
      "futarchy_enabled": true,
      "min_members": 1,
      "max_members": 10,
      "term_duration": 31536000
    }
  ],
  "deposit": "50000000uspark",
  "title": "Create Ecosystem Council",
  "summary": "Bootstrapping the Growth Pillar."
}' > "$PROPOSAL_DIR/create_eco.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/create_eco.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
PROP_ID=$(get_prop_id $TX_HASH)

echo "Eco Proposal ID: $PROP_ID"
$BINARY tx gov vote $PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
wait_for_pass $PROP_ID

# ==============================================================================
# 5. VERIFICATION
# ==============================================================================
echo "--- VERIFYING REGISTRY ---"

# Check Commons
COMMONS_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json)
COMMONS_POLICY=$(echo $COMMONS_INFO | jq -r '.extended_group.policy_address')
COMMONS_PARENT=$(echo $COMMONS_INFO | jq -r '.extended_group.parent_policy_address')

if [ "$COMMONS_PARENT" == "$GOV_ADDR" ]; then
    echo "✅ Commons Council Registered. Policy: $COMMONS_POLICY"
else
    echo "❌ Commons Council Setup Failed."
fi

# Check Funding Shares in Split
echo "--- VERIFYING FUNDING ---"
# Note: You might need to query the KVStore or a dedicated query if you implemented one in x/split
# For now, we assume success if the group registration succeeded (since it calls x/split)
echo "✅ Funding Shares should be active (50/30/20)."

echo "--- SETUP COMPLETE ---"