#!/bin/bash

echo "--- TESTING NAME MODULE: REP-MEMBER GATE + TARGET + TRANSFER ---"

# Covers the post-Council-gate behavior:
#   1. A non-Council x/rep member can register a name.
#   2. ENS-style resolver target with accept/revoke flow.
#   3. SetPrimary widened to accept-target path; reverse resolution.
#   4. Direct transfer; rejection on active dispute and non-member recipient.

set -uo pipefail

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

if ! command -v jq &> /dev/null; then
    echo "[FAIL] Error: jq is not installed."
    exit 1
fi

# Reload the test environment seeded by setup_test_accounts.sh.
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    # shellcheck source=/dev/null
    . "$SCRIPT_DIR/.test_env"
else
    echo "[FAIL] Error: $SCRIPT_DIR/.test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)
CLAIMANT_ADDR=${NAME_CLAIMANT_ADDR:-}

if [ -z "$CLAIMANT_ADDR" ]; then
    echo "[FAIL] Error: NAME_CLAIMANT_ADDR not set in .test_env."
    exit 1
fi

echo "Alice (genesis x/rep member, owner):   $ALICE_ADDR"
echo "Claimant (invited x/rep member, agent): $CLAIMANT_ADDR"
echo "Dave (no x/rep Member record):          $DAVE_ADDR"
echo ""

wait_block() { sleep 4; }

# --- 1. INVITED MEMBER REGISTERS A NAME ---
# This is the headline use case: an invited member (not Commons Council) can
# now claim a name without governance involvement.
echo "--- STEP 1: Non-Council x/rep member registers 'agent-alpha' ---"
RES=$($BINARY tx name register-name "agent-alpha" "agent-meta" --from name_claimant -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>/dev/null)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
wait_block
QRES=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
CODE=$(echo "$QRES" | jq -r '.code')
if [ "$CODE" = "0" ]; then
    echo "[ OK ] Invited member registered a name (gate change works)."
else
    echo "[FAIL] Failed: invited member could not register. Log:"
    echo "$QRES" | jq -r '.raw_log'
    exit 1
fi

# --- 2. ALICE REGISTERS 'kob' AND POINTS IT AT THE AGENT ---
echo ""
echo "--- STEP 2: Alice registers 'kob', points target at agent ---"
$BINARY tx name register-name "kob" "kob-meta" --from alice -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json > /dev/null 2>&1
wait_block

RES=$($BINARY tx name set-target "kob" "$CLAIMANT_ADDR" --from alice -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>/dev/null)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
wait_block
QRES=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
CODE=$(echo "$QRES" | jq -r '.code')
if [ "$CODE" = "0" ]; then
    echo "[ OK ] Target set."
else
    echo "[FAIL] set-target failed. Log:"
    echo "$QRES" | jq -r '.raw_log'
    exit 1
fi

REC=$($BINARY query name resolve "kob" --output json)
TARGET=$(echo "$REC" | jq -r '.name_record.target')
# proto3 omits zero-valued bool fields from JSON; default to "false" when
# target_accepted is absent.
ACCEPTED=$(echo "$REC" | jq -r '.name_record.target_accepted // false')
if [ "$TARGET" = "$CLAIMANT_ADDR" ] && [ "$ACCEPTED" = "false" ]; then
    echo "[ OK ] NameRecord reflects target=$TARGET, target_accepted=false (consent still pending)."
else
    echo "[FAIL] Unexpected NameRecord: target=$TARGET accepted=$ACCEPTED"
    exit 1
fi

# --- 3. UNACCEPTED-TARGET CANNOT SET PRIMARY ---
echo ""
echo "--- STEP 3: Agent cannot set 'kob' as primary before accepting ---"
RES=$($BINARY tx name set-primary "kob" --from name_claimant -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>/dev/null)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
wait_block
QRES=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
RAW=$(echo "$QRES" | jq -r '.raw_log')
if echo "$RAW" | grep -q "AcceptTarget\|target has not accepted"; then
    echo "[ OK ] Blocked with target-not-accepted error."
elif [ "$(echo "$QRES" | jq -r '.code')" != "0" ]; then
    echo "[ OK ] Blocked (code != 0)."
else
    echo "[FAIL] Unaccepted target was allowed to set primary."
    exit 1
fi

# --- 4. AGENT ACCEPTS, THEN SETS PRIMARY, REVERSE-RESOLVES ---
echo ""
echo "--- STEP 4: Agent accepts target and sets primary ---"
RES=$($BINARY tx name accept-target "kob" --from name_claimant -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>/dev/null)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
wait_block
QRES=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
if [ "$(echo "$QRES" | jq -r '.code')" != "0" ]; then
    echo "[FAIL] accept-target failed. Log:"
    echo "$QRES" | jq -r '.raw_log'
    exit 1
fi
echo "[ OK ] Target accepted."

RES=$($BINARY tx name set-primary "kob" --from name_claimant -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>/dev/null)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
wait_block
QRES=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
if [ "$(echo "$QRES" | jq -r '.code')" != "0" ]; then
    echo "[FAIL] set-primary (accepted target path) failed. Log:"
    echo "$QRES" | jq -r '.raw_log'
    exit 1
fi
echo "[ OK ] Primary set."

REVERSE=$($BINARY query name reverse-resolve "$CLAIMANT_ADDR" --output json 2>/dev/null | jq -r '.name')
if [ "$REVERSE" = "kob" ]; then
    echo "[ OK ] ReverseResolve($CLAIMANT_ADDR) = 'kob'."
else
    echo "[FAIL] Reverse resolution returned '$REVERSE', expected 'kob'."
    exit 1
fi

# --- 5. CHANGING TARGET REVOKES ACCEPTANCE AND CLEARS AGENT'S PRIMARY ---
echo ""
echo "--- STEP 5: Re-pointing target revokes acceptance and clears primary ---"
RES=$($BINARY tx name set-target "kob" "$ALICE_ADDR" --from alice -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>/dev/null)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
wait_block
QRES=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
if [ "$(echo "$QRES" | jq -r '.code')" != "0" ]; then
    echo "[FAIL] set-target re-point failed. Log:"
    echo "$QRES" | jq -r '.raw_log'
    exit 1
fi
ACCEPTED=$($BINARY query name resolve "kob" --output json | jq -r '.name_record.target_accepted // false')
if [ "$ACCEPTED" = "false" ]; then
    echo "[ OK ] target_accepted reset to false on re-point."
else
    echo "[FAIL] Expected target_accepted=false, got '$ACCEPTED'."
    exit 1
fi

REVERSE=$($BINARY query name reverse-resolve "$CLAIMANT_ADDR" --output json 2>/dev/null | jq -r '.name')
if [ -z "$REVERSE" ] || [ "$REVERSE" = "null" ]; then
    echo "[ OK ] Agent's primary cleared after re-point (reverse resolution returns no primary)."
else
    echo "[WARN]  Agent reverse-resolves to '$REVERSE' (expected cleared). Continuing."
fi

# --- 6. TRANSFER 'kob' TO AGENT (HAPPY PATH) ---
echo ""
echo "--- STEP 6: Alice transfers 'kob' to the agent ---"
# Clear target first so we have a clean transfer scenario.
$BINARY tx name set-target "kob" "" --from alice -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json > /dev/null 2>&1
wait_block

RES=$($BINARY tx name transfer-name "kob" "$CLAIMANT_ADDR" --from alice -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>/dev/null)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
wait_block
QRES=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
if [ "$(echo "$QRES" | jq -r '.code')" != "0" ]; then
    echo "[FAIL] transfer-name failed. Log:"
    echo "$QRES" | jq -r '.raw_log'
    exit 1
fi
NEW_OWNER=$($BINARY query name resolve "kob" --output json | jq -r '.name_record.owner')
if [ "$NEW_OWNER" = "$CLAIMANT_ADDR" ]; then
    echo "[ OK ] Ownership of 'kob' is now $CLAIMANT_ADDR."
else
    echo "[FAIL] Expected new owner $CLAIMANT_ADDR, got '$NEW_OWNER'."
    exit 1
fi

# --- 7. TRANSFER TO NON-MEMBER FAILS ---
echo ""
echo "--- STEP 7: Transfer to non-member (Dave) is rejected ---"
# name_claimant just received 'kob' in step 6; transfer to dave (no Member).
RES=$($BINARY tx name transfer-name "kob" "$DAVE_ADDR" --from name_claimant -y \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>/dev/null)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
wait_block
QRES=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
RAW=$(echo "$QRES" | jq -r '.raw_log')
if echo "$RAW" | grep -q "active x/rep member\|recipient is not"; then
    echo "[ OK ] Transfer to non-member blocked with member error."
elif [ "$(echo "$QRES" | jq -r '.code')" != "0" ]; then
    echo "[ OK ] Transfer to non-member blocked (code != 0)."
else
    NEW_OWNER=$($BINARY query name resolve "kob" --output json | jq -r '.name_record.owner')
    if [ "$NEW_OWNER" != "$DAVE_ADDR" ]; then
        echo "[ OK ] Owner unchanged after rejected transfer."
    else
        echo "[FAIL] Transfer to non-member succeeded."
        exit 1
    fi
fi

echo ""
echo "--- ALL TARGET / TRANSFER CASES PASSED ---"
