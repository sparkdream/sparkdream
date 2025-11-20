#!bin/bash

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.value.address')

# --- 1. SUBMIT HOSTILE GOV HANDOVER PROPOSAL (Using file pipe) ---
echo "Submitting Hostile Handover Proposal..."

# Generate the JSON content using the discovered address
echo '{"messages": [{"@type": "/sparkdream.split.v1.MsgUpdateParams", "authority": "'$GOV_ADDR'", "params": {"commons_council_address": "'$ALICE_ADDR'"}}], "deposit": "10000000uspark", "title": "Handover to Council", "summary": "Setting the split module authority address."}' | tee gov_handover_hostile.json

# Submit Proposal (Using cat/pipe for robust file reading)
$BINARY tx gov submit-proposal gov_handover_hostile.json --from alice -y --chain-id sparkdream --keyring-backend test

sleep 3

# --- 2. Vote & Pass Gov Proposal (ID 2) ---
$BINARY tx gov vote 2 yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for voting period to end (20s)..."
sleep 25 # Wait slightly longer than the configured voting period

echo "Checking Params..."
$BINARY query split params