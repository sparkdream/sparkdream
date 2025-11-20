#!bin/bash

echo "--- TESTING SPEND: SUCCESSFUL TREASURY SPEND (2/3 VOTES) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Discover the Commons Council Address
COMMONS_COUNCIL_ADDR=$($BINARY query split params --output json | jq -r '.params.commons_council_address')

echo "Commons Council Address: $COMMONS_COUNCIL_ADDR"
echo "--- CHECKING BOB'S INITIAL BALANCE ---"
$BINARY query bank balances $BOB_ADDR

# --- 1. Create the Proposal JSON (Fixed for Repeated Coins) ---
echo '{
  "group_policy_address": "'$COMMONS_COUNCIL_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Test Spend",
  "summary": "Send 1 SPARK to Bob",
  "messages": [
    {
      "@type": "/sparkdream.split.v1.MsgSpendFromCommons",
      "authority": "'$COMMONS_COUNCIL_ADDR'",
      "recipient": "'$BOB_ADDR'",
      "amount": [
        {
          "denom": "uspark",
          "amount": "1000000"
        }
      ] 
    }
  ]
}' > msg_spend_test.json

# --- 2. Submit Proposal ---
echo "Submitting proposal..."
$BINARY tx group submit-proposal msg_spend_test.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark

sleep 3

# --- 3. Vote from Bob ---
# We assume this is Group Proposal #1
echo "Voting..."
$BINARY tx group vote 1 $BOB_ADDR VOTE_OPTION_YES "Agreed" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for voting period to end (20s)..."
sleep 25 

# --- 4. Execute the Passed Proposal ---
echo "Executing..."
$BINARY tx group exec 1 --from alice -y --chain-id $CHAIN_ID --keyring-backend test

sleep 3

echo "--- VERIFYING BOB'S BALANCE ---"
$BINARY query bank balances $BOB_ADDR