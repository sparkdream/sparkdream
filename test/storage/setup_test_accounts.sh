#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/storage Tests"
echo "=================================================="
echo ""

# Cast (docs/x-storage-spec.md):
#   storop1, storop2  storage-arweave operators: active x/rep members, bonded
#                     in x/service under the Commons Operations Committee,
#                     approved for the arweave backend by that committee.
#   storscan1         content-scanner operator, approved the same way.
#   bob, carol        genesis members who request, add sources and dispute.
# The committee is the controller because the e2e committees seat alice
# alone, so one vote passes a proposal (test/lib/ops_committee.sh).

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_lib.sh"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
echo "Genesis member (Alice): $ALICE_ADDR"

if [ -f "$SCRIPT_DIR/.test_env" ]; then
    rm -f "$SCRIPT_DIR/.test_env"
fi

# ============================================================================
# 1. Keys
# ============================================================================
echo "Step 1: Creating test account keys..."
ACCOUNTS=("storop1" "storop2" "storscan1")
for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show "$ACCOUNT" --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add "$ACCOUNT" --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $ACCOUNT"
    fi
done
STOROP1_ADDR=$($BINARY keys show storop1 -a --keyring-backend test)
STOROP2_ADDR=$($BINARY keys show storop2 -a --keyring-backend test)
STORSCAN1_ADDR=$($BINARY keys show storscan1 -a --keyring-backend test)

# ============================================================================
# 2. Funds: the operator bond is 1000 SPARK, the scanner bond 100 SPARK.
# ============================================================================
echo "Step 2: Funding test accounts..."
fund() { # <address> <amount>
    submit_and_wait "$($BINARY tx bank send alice "$1" "$2${BOND_DENOM}" $TXFLAGS 2>&1)"
    if [ "$(tx_code "$TX_RESULT")" != "0" ]; then
        echo "  ERROR: funding $1 failed: $(raw_log "$TX_RESULT")"
        exit 1
    fi
}
fund "$STOROP1_ADDR" 1500000000
fund "$STOROP2_ADDR" 1500000000
fund "$STORSCAN1_ADDR" 300000000
echo "  funded storop1, storop2, storscan1"

# ============================================================================
# 3. Membership: a storage operator must be an active x/rep member.
# ============================================================================
echo "Step 3: Inviting the operators as members..."
for ACCOUNT in storop1 storop2; do
    ADDR=$($BINARY keys show "$ACCOUNT" -a --keyring-backend test)
    if $BINARY query rep get-member "$ADDR" --output json > /dev/null 2>&1; then
        echo "  $ACCOUNT is already a member"
        continue
    fi
    STAKE=$($BINARY query rep required-invitation-stake "$ALICE_ADDR" --output json 2>/dev/null | jq -r '.required_stake // "100000000"')
    submit_and_wait "$($BINARY tx rep invite-member "$ADDR" "$STAKE" --from alice $TXFLAGS 2>&1)"
    if [ "$(tx_code "$TX_RESULT")" != "0" ]; then
        echo "  ERROR: inviting $ACCOUNT failed: $(raw_log "$TX_RESULT")"
        exit 1
    fi
    INVITATION_ID=$(event_attr "$TX_RESULT" create_invitation invitation_id)
    submit_and_wait "$($BINARY tx rep accept-invitation "$INVITATION_ID" CC0-1.0 --from "$ACCOUNT" $TXFLAGS 2>&1)"
    if [ "$(tx_code "$TX_RESULT")" != "0" ]; then
        echo "  ERROR: $ACCOUNT could not accept invitation $INVITATION_ID: $(raw_log "$TX_RESULT")"
        exit 1
    fi
    echo "  $ACCOUNT joined (invitation #$INVITATION_ID)"
done

# ============================================================================
# 4. x/service registration under the Commons Operations Committee
# ============================================================================
echo "Step 4: Registering the operators and the scanner in x/service..."
OPS_POLICY=$($BINARY query commons get-group "Commons Operations Committee" --output json | jq -r '.group.policy_address // empty')
if [ -z "$OPS_POLICY" ]; then
    echo "  ERROR: Commons Operations Committee not found"
    exit 1
fi
register() { # <key> <service type> <metadata json>
    local key=$1 st=$2 addr bond
    addr=$($BINARY keys show "$key" -a --keyring-backend test)
    if [ -n "$($BINARY query service operator "$addr" "$st" --output json 2>/dev/null | jq -r '.operator.status // empty')" ]; then
        echo "  $key already registered as $st"
        return 0
    fi
    bond=$($BINARY query service service-type "$st" --output json | jq -r '.config.min_bond_amount // empty')
    if [ -z "$bond" ]; then
        echo "  ERROR: service type $st not found"
        exit 1
    fi
    submit_and_wait "$($BINARY tx service register-operator "$st" "$OPS_POLICY" "$bond" "$(printf '%s' "$3" | base64 -w0)" --from "$key" $TXFLAGS 2>&1)"
    if [ "$(tx_code "$TX_RESULT")" != "0" ]; then
        echo "  ERROR: registering $key as $st failed: $(raw_log "$TX_RESULT")"
        exit 1
    fi
    echo "  $key registered as $st (bond $bond)"
}
register storop1 storage-arweave '{"v":1,"backend":"arweave","wallets":["zenith-wallet-1"]}'
register storop2 storage-arweave '{"v":1,"backend":"arweave","wallets":["zenith-wallet-2"]}'
register storscan1 content-scanner "$(printf '{"v":1,"verdict_key":"%s","feed_url":"https://feeds.example/aurora","ruleset_version":"test"}' "$(head -c 32 /dev/zero | base64 -w0)")"

# ============================================================================
# 5. Approval by the controller: without it, registration counts for nothing.
# ============================================================================
echo "Step 5: The committee approves its operators and scanner..."
approve() { # <label> <msg json>
    if ops_committee_exec "$2"; then
        echo "  approved $1"
    else
        echo "  ERROR: approving $1 failed: $OPS_COMMITTEE_ERROR"
        exit 1
    fi
}
approve storop1 "$(jq -nc --arg o "$STOROP1_ADDR" '{"@type":"/sparkdream.storage.v1.MsgApproveOperator",backend:"arweave",operator:$o}')"
approve storop2 "$(jq -nc --arg o "$STOROP2_ADDR" '{"@type":"/sparkdream.storage.v1.MsgApproveOperator",backend:"arweave",operator:$o}')"
approve storscan1 "$(jq -nc --arg s "$STORSCAN1_ADDR" '{"@type":"/sparkdream.storage.v1.MsgApproveScanner",scanner:$s}')"

cat > "$SCRIPT_DIR/.test_env" <<ENV
export ALICE_ADDR=$ALICE_ADDR
export BOB_ADDR=$BOB_ADDR
export CAROL_ADDR=$CAROL_ADDR
export STOROP1_ADDR=$STOROP1_ADDR
export STOROP2_ADDR=$STOROP2_ADDR
export STORSCAN1_ADDR=$STORSCAN1_ADDR
export OPS_POLICY=$OPS_POLICY
ENV

echo ""
echo "Setup complete: $SCRIPT_DIR/.test_env"
