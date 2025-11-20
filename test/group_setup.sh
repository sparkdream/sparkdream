#!bin/bash

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.value.address')

# --- 1. CLEANUP ANY TEMP FILES ---
rm -f members.json policy_std.json policy_veto.json gov_handover.json gov_handover_hostile.json

# --- 2. CREATE MEMBERS FILE ---
echo '{"members": [
  {"address": "'$ALICE_ADDR'", "weight": "1", "metadata": "Alice"}, 
  {"address": "'$BOB_ADDR'", "weight": "1", "metadata": "Bob"}, 
  {"address": "'$CAROL_ADDR'", "weight": "1", "metadata": "Carol"}
]}' > members.json

# --- 3: Get initial account sequence ---
ALICE_SEQ=$($BINARY query auth account $ALICE_ADDR -o json | jq -r '.account.value.sequence' 2>/dev/null || echo 1)
echo "Starting Alice's sequence at: $ALICE_SEQ"

# --- 4. CREATE GROUP (ID 1) ---
echo "Creating Commons Council Group..."
$BINARY tx group create-group $ALICE_ADDR "Commons Council" members.json \
  --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json

sleep 3

# --- 5. GENERATE POLICY JSON FILES ---
# Standard Policy (25%)
echo '{"@type":"/cosmos.group.v1.PercentageDecisionPolicy", "percentage":"0.25", "windows":{"voting_period":"60s", "min_execution_period":"0s"}}' > policy_std.json
# Veto Policy (50%)
echo '{"@type":"/cosmos.group.v1.PercentageDecisionPolicy", "percentage":"0.50", "windows":{"voting_period":"60s", "min_execution_period":"0s"}}' > policy_veto.json

# --- 6. CREATE STANDARD POLICY (Using file path) ---
echo "Creating Standard Policy (25%)..."
$BINARY tx group create-group-policy $ALICE_ADDR 1 "standard" policy_std.json \
  --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json

sleep 3

# --- 7. CREATE VETO POLICY (Using file path) ---
echo "Creating Veto Policy (50%)..."
$BINARY tx group create-group-policy $ALICE_ADDR 1 "veto" policy_veto.json \
  --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json

sleep 3

# --- 8. DISCOVER ADDRESS & HANDOVER PREP ---
STANDARD_ADDR=$($BINARY query group group-policies-by-group 1 -o json | jq -r '.group_policies[] | select(.metadata == "standard") | .address')

echo "Standard Policy Address: $STANDARD_ADDR"

# --- 9. SUBMIT GOV HANDOVER PROPOSAL (Using file pipe) ---
echo "Submitting Handover Proposal..."

# Generate the JSON content using the discovered address
echo '{"messages": [{"@type": "/sparkdream.split.v1.MsgUpdateParams", "authority": "'$GOV_ADDR'", "params": {"commons_council_address": "'$STANDARD_ADDR'"}}], "deposit": "50000000uspark", "title": "Handover to Council", "summary": "Setting the split module authority address."}' | tee gov_handover.json

# Submit Proposal (Using cat/pipe for robust file reading)
$BINARY tx gov submit-proposal gov_handover.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test

sleep 3

# --- 10. Vote & Pass Gov Proposal (ID 1) ---
$BINARY tx gov vote 1 yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for voting period to end (20s)..."
sleep 25 # Wait slightly longer than the configured voting period

echo "Checking Params..."
$BINARY query split params

echo "--- COMMONS COUNCIL GROUP SETUP COMPLETE ---"