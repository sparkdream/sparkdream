#!/bin/bash

echo "--- TESTING NAME MODULE: DISPLAY NAME ---"

# --- 0. SETUP & CONFIG ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if ! command -v jq &> /dev/null; then
    echo "[FAIL] Error: jq is not installed."
    exit 1
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Fresh address that does not own a handle. name_claimant is created by
# setup_test_accounts.sh and exists in the keyring without a registered
# x/name record — perfect for proving "no handle required".
NO_HANDLE_ACCT="name_claimant"
NO_HANDLE_ADDR=$($BINARY keys show $NO_HANDLE_ACCT -a --keyring-backend test 2>/dev/null)
if [ -z "$NO_HANDLE_ADDR" ]; then
    echo "[FAIL] Error: $NO_HANDLE_ACCT key not found. Run setup_test_accounts.sh first."
    exit 1
fi

echo "Alice:           $ALICE_ADDR"
echo "Bob:             $BOB_ADDR"
echo "name_claimant:   $NO_HANDLE_ADDR  (no handle owned)"
echo ""

# --- 1. BOOTSTRAP SEEDING ---
# Genesis bootstrap should have seeded display_name for every entry in
# GenesisNames (Alice, Bob, Carol on testparams). Verify Alice's.
echo "--- STEP 1: Genesis Bootstrap Seeded Display Names ---"

INFO=$($BINARY query name owner-info $ALICE_ADDR --output json)
SEEDED=$(echo "$INFO" | jq -r '.owner_info.display_name')
echo "Alice display_name (seeded): '$SEEDED'"

if [ "$SEEDED" == "Alice" ]; then
    echo "[ OK ] SUCCESS: Bootstrap seeded Alice's display_name."
else
    echo "[FAIL] FAILURE: Expected 'Alice', got '$SEEDED'."
    exit 1
fi

# --- 2. SET DISPLAY NAME (HAS HANDLE) ---
echo "--- STEP 2: Alice updates her display name ---"

RES=$($BINARY tx name set-display-name "Alice the Great" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
sleep 4

QUERY_RES=$($BINARY query tx $TX_HASH --output json)
CODE=$(echo "$QUERY_RES" | jq -r '.code')
if [ "$CODE" != "0" ]; then
    echo "[FAIL] FAILURE: set-display-name failed (code=$CODE)."
    echo "Log: $(echo "$QUERY_RES" | jq -r '.raw_log')"
    exit 1
fi

INFO=$($BINARY query name owner-info $ALICE_ADDR --output json)
DISPLAY=$(echo "$INFO" | jq -r '.owner_info.display_name')
if [ "$DISPLAY" == "Alice the Great" ]; then
    echo "[ OK ] SUCCESS: Alice's display_name is 'Alice the Great'."
else
    echo "[FAIL] FAILURE: Expected 'Alice the Great', got '$DISPLAY'."
    exit 1
fi

# --- 3. SET DISPLAY NAME (NO HANDLE) ---
# A user with no registered handle can still set a display name; an OwnerInfo
# is created on the fly.
echo "--- STEP 3: Address with no handle sets display name ---"

RES=$($BINARY tx name set-display-name "Mystery Voter" --from $NO_HANDLE_ACCT -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
sleep 4

QUERY_RES=$($BINARY query tx $TX_HASH --output json)
CODE=$(echo "$QUERY_RES" | jq -r '.code')
if [ "$CODE" != "0" ]; then
    echo "[FAIL] FAILURE: set-display-name (no handle) failed (code=$CODE)."
    echo "Log: $(echo "$QUERY_RES" | jq -r '.raw_log')"
    exit 1
fi

INFO=$($BINARY query name owner-info $NO_HANDLE_ADDR --output json)
DISPLAY=$(echo "$INFO" | jq -r '.owner_info.display_name // ""')
PRIMARY=$(echo "$INFO" | jq -r '.owner_info.primary_name // ""')
# Earlier suite tests (target_and_transfer_test) may have registered a
# handle for name_claimant and assigned it as primary. The point of this
# step is just that set-display-name works for a non-handle-owner; primary
# may or may not be empty depending on test order.
if [ "$DISPLAY" == "Mystery Voter" ]; then
    echo "[ OK ] SUCCESS: name_claimant has display_name='Mystery Voter' (primary='$PRIMARY')."
else
    echo "[FAIL] FAILURE: display='$DISPLAY' primary='$PRIMARY' (expected display='Mystery Voter')."
    exit 1
fi

# --- 4. CLEAR DISPLAY NAME ---
echo "--- STEP 4: Alice clears her display name (empty string) ---"

RES=$($BINARY tx name set-display-name "" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
sleep 4

INFO=$($BINARY query name owner-info $ALICE_ADDR --output json)
DISPLAY=$(echo "$INFO" | jq -r '.owner_info.display_name // ""')
if [ "$DISPLAY" == "" ]; then
    echo "[ OK ] SUCCESS: Alice's display_name cleared."
else
    echo "[FAIL] FAILURE: Expected empty, got '$DISPLAY'."
    exit 1
fi

# --- 5. VALIDATION REJECTIONS ---
# Spot-check a few rejection classes; the keeper unit tests cover the full
# matrix (control chars, newlines, NFC, surrogates, etc.).
echo "--- STEP 5: Validation rejects malformed display names ---"

reject_case() {
    local DESC=$1
    local VAL=$2

    RES=$($BINARY tx name set-display-name "$VAL" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
    CODE=$(echo "$RES" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "[ OK ] SUCCESS: $DESC blocked at CheckTx."
        return
    fi

    TX_HASH=$(echo "$RES" | jq -r '.txhash')
    sleep 4
    QUERY_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
    FINAL_CODE=$(echo "$QUERY_RES" | jq -r '.code')
    if [ "$FINAL_CODE" != "0" ]; then
        echo "[ OK ] SUCCESS: $DESC blocked on-chain."
    else
        echo "[FAIL] FAILURE: $DESC was accepted!"
        exit 1
    fi
}

reject_case "leading whitespace" " Bob"
reject_case "33+ chars" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
reject_case "embedded tab" $'Bob\tHero'
reject_case "embedded newline" $'Bob\nHero'

# --- 6. UNKNOWN ADDRESS RETURNS GRACEFUL EMPTY ---
# A bech32-valid address with no OwnerInfo record should return an echo
# with all other fields empty, not an error.
echo "--- STEP 6: owner-info for unknown address returns empty fields ---"

# Generate a throwaway address that has no record by deriving from a
# deterministic-but-unused string.
GHOST_ADDR="sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe"  # burn address; never gets an OwnerInfo

INFO=$($BINARY query name owner-info $GHOST_ADDR --output json)
ECHOED=$(echo "$INFO" | jq -r '.owner_info.address')
DISPLAY=$(echo "$INFO" | jq -r '.owner_info.display_name // ""')
PRIMARY=$(echo "$INFO" | jq -r '.owner_info.primary_name // ""')

if [ "$ECHOED" == "$GHOST_ADDR" ] && [ "$DISPLAY" == "" ] && [ "$PRIMARY" == "" ]; then
    echo "[ OK ] SUCCESS: Unknown address returns echo with empty fields."
else
    echo "[FAIL] FAILURE: address='$ECHOED' display='$DISPLAY' primary='$PRIMARY'."
    exit 1
fi

echo ""
echo "--- ALL DISPLAY NAME TESTS PASSED ---"
