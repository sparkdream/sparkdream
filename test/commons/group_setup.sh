#!/bin/bash

echo "--- SETUP: COMMONS COUNCIL & HANDOVER ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

# Robust Gov Address Lookup
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov Address: $GOV_ADDR"

# --- 1. CLEANUP ---
rm -f proposals/*.json
mkdir -p proposals

# --- 2. CREATE MEMBERS FILE ---
echo '{"members": [
  {"address": "'$ALICE_ADDR'", "weight": "1", "metadata": "Alice"}, 
  {"address": "'$BOB_ADDR'", "weight": "1", "metadata": "Bob"}, 
  {"address": "'$CAROL_ADDR'", "weight": "1", "metadata": "Carol"}
]}' > proposals/members.json

# --- 4. CREATE GROUP (ID 1) ---
echo "Creating Commons Council Group..."
$BINARY tx group create-group $ALICE_ADDR "Commons Council" proposals/members.json \
  --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json

sleep 3

# --- 5. GENERATE POLICY FILES ---
# Standard Policy (25%)
echo '{"@type":"/cosmos.group.v1.PercentageDecisionPolicy", "percentage":"0.25", "windows":{"voting_period":"30s", "min_execution_period":"0s"}}' > proposals/policy_std.json
# Veto Policy (50%)
echo '{"@type":"/cosmos.group.v1.PercentageDecisionPolicy", "percentage":"0.50", "windows":{"voting_period":"10s", "min_execution_period":"0s"}}' > proposals/policy_veto.json

# --- 6. CREATE STANDARD POLICY ---
echo "Creating Standard Policy (25%)..."
$BINARY tx group create-group-policy $ALICE_ADDR 1 "standard" proposals/policy_std.json \
  --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json

sleep 3

# --- 7. CREATE VETO POLICY ---
echo "Creating Veto Policy (50%)..."
$BINARY tx group create-group-policy $ALICE_ADDR 1 "veto" proposals/policy_veto.json \
  --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json

sleep 3

# --- 8. DISCOVER ADDRESS ---
STANDARD_ADDR=$($BINARY query group group-policies-by-group 1 -o json | jq -r '.group_policies[] | select(.metadata == "standard") | .address' | head -n 1 | tr -d '"')

echo "Standard Policy Address: $STANDARD_ADDR"

# --- 9. SUBMIT GOV HANDOVER PROPOSAL ---
echo "Submitting Handover Proposal..."

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateParams",
      "authority": "'$GOV_ADDR'",
      "params": {
        "commons_council_address": "'$STANDARD_ADDR'"
      }
    }
  ],
  "deposit": "50000000uspark",
  "title": "Handover to Council",
  "summary": "Setting the split module authority address."
}' > proposals/gov_handover.json

$BINARY tx gov submit-proposal proposals/gov_handover.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test

sleep 3

# --- 10. Vote & Pass Gov Proposal (ID 1) ---
$BINARY tx gov vote 1 yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for voting period to end (60s)..."
sleep 65 

echo "Checking Params..."
$BINARY query commons params

# --- 11. SECURE THE GROUP (UPDATE ADMIN) ---
echo "--- SECURING GROUP: TRANSFERRING ADMIN RIGHTS ---"
echo "Current Admin: Alice"
echo "New Admin:     $STANDARD_ADDR"

$BINARY tx group update-group-admin $ALICE_ADDR 1 $STANDARD_ADDR \
  --from alice -y \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000000uspark

sleep 3

# Verify Admin Update
NEW_ADMIN_INFO=$($BINARY query group group-info 1 --output json | jq -r '.info.admin')
if [ "$NEW_ADMIN_INFO" == "$STANDARD_ADDR" ]; then
    echo "✅ SUCCESS: Group Admin is now the Standard Policy Address."
else
    echo "❌ FAILURE: Group Admin is still $NEW_ADMIN_INFO"
fi

echo "--- COMMONS COUNCIL GROUP SETUP COMPLETE ---"