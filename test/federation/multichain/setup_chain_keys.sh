#!/bin/bash
# ------------------------------------------------------------------
# Create distinct test accounts on each chain.
#
# The snapshot we cloned in init_chains.sh is mirrored — alice/bob/carol
# have identical addresses on both chains. That makes identity tests
# meaningless: linking alice@chain-a to alice@chain-b is a self-link.
#
# This script creates fresh keys that exist on ONE chain only:
#   - mc-linker-a  on chain-a only (used by tests as the "claimant")
#   - mc-linker-a2 on chain-a only (alt claimant for "already claimed" test)
#   - mc-owner-b   on chain-b only (used by tests as the remote identity)
#
# Each is funded by alice on the relevant chain and invited to x/rep so
# it has a trust level (required for federation messages).
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/lib_multichain.sh"

echo "=================================================="
echo "  CHAIN-SPECIFIC KEY SETUP"
echo "=================================================="
echo ""

# ------------------------------------------------------------------
# Helpers
# ------------------------------------------------------------------

# Create a key on chain-a only (skip if already present on chain-b).
# Echoes the address of the new key.
create_chain_a_only_key() {
    local NAME=$1
    if keys_a show "$NAME" -a >/dev/null 2>&1; then
        keys_a show "$NAME" -a
        return
    fi
    keys_a add "$NAME" --output json >/dev/null 2>&1
    keys_a show "$NAME" -a
}

create_chain_b_only_key() {
    local NAME=$1
    if keys_b show "$NAME" -a >/dev/null 2>&1; then
        keys_b show "$NAME" -a
        return
    fi
    keys_b add "$NAME" --output json >/dev/null 2>&1
    keys_b show "$NAME" -a
}

# Fund an address from alice on the given chain.
fund_a() {
    local ADDR=$1; local AMOUNT=${2:-100000000uspark}
    local TX
    TX=$(cli_a tx bank send alice "$ADDR" "$AMOUNT" \
        --from alice -y --fees 5000uspark --output json 2>&1)
    submit_and_wait_a "$TX" "fund $ADDR (chain-a)"
}

fund_b() {
    local ADDR=$1; local AMOUNT=${2:-100000000uspark}
    local TX
    TX=$(cli_b tx bank send alice "$ADDR" "$AMOUNT" \
        --from alice -y --fees 5000uspark --output json 2>&1)
    submit_and_wait_b "$TX" "fund $ADDR (chain-b)"
}

# Invite + accept a rep membership for an address on a chain.
# Args: chain (a|b) inviter_key inviter_addr invitee_addr invitee_key
# (DREAM amount is queried per-chain from required-invitation-stake; the
# x/rep micro-DREAM conversion + escalating stake formula made fixed
# amounts brittle.)
invite_and_accept() {
    local CHAIN=$1; local INVITER=$2; local INVITER_ADDR=$3; local ADDR=$4; local KEY=$5

    local cli_x submit_x
    if [ "$CHAIN" = "a" ]; then cli_x="cli_a"; submit_x="submit_and_wait_a"
    else                          cli_x="cli_b"; submit_x="submit_and_wait_b"
    fi

    # Skip if already a member
    if $cli_x query rep get-member "$ADDR" --output json 2>&1 | jq -e '.member' >/dev/null 2>&1; then
        echo "  $ADDR already a rep member on chain-$CHAIN"
        return 0
    fi

    # Use the binary directly for queries — query subcommands do not accept
    # --chain-id, so cli_x's blanket flag list would error out. We still
    # need --node to target the right chain's RPC.
    local DREAM RPC HOME_ARG
    if [ "$CHAIN" = "a" ]; then
        RPC="$CHAIN_A_RPC"; HOME_ARG="$CHAIN_A_HOME"
    else
        RPC="$CHAIN_B_RPC"; HOME_ARG="$CHAIN_B_HOME"
    fi
    DREAM=$("$BINARY" query rep required-invitation-stake "$INVITER_ADDR" \
        --node "$RPC" --home "$HOME_ARG" --output json 2>/dev/null \
        | jq -r '.required_stake // "100000000"')

    echo "  Inviting $ADDR as rep member on chain-$CHAIN (stake=$DREAM micro-DREAM)..."
    local TX
    TX=$($cli_x tx rep invite-member "$ADDR" "$DREAM" \
        --from "$INVITER" -y --fees 5000uspark --output json 2>&1)
    $submit_x "$TX" "invite $ADDR" || return 1

    # Pull the invitation_id from events
    local INVITATION_ID
    INVITATION_ID=$(echo "$TX_RESULT" | jq -r '.events[]
        | select(.type=="create_invitation")
        | .attributes[]
        | select(.key=="invitation_id")
        | .value' | tr -d '"' | head -1)
    if [ -z "$INVITATION_ID" ]; then
        echo "  ERROR: no invitation_id in events"
        return 1
    fi

    echo "  Accepting invitation $INVITATION_ID..."
    TX=$($cli_x tx rep accept-invitation "$INVITATION_ID" \
        --from "$KEY" -y --fees 5000uspark --output json 2>&1)
    $submit_x "$TX" "accept $INVITATION_ID" || return 1
}

# ------------------------------------------------------------------
# Create the keys
# ------------------------------------------------------------------
echo "=== Creating chain-a-only keys ==="
LINKER_A_ADDR=$(create_chain_a_only_key  "mc-linker-a")
LINKER_A2_ADDR=$(create_chain_a_only_key "mc-linker-a2")
echo "  mc-linker-a  -> $LINKER_A_ADDR"
echo "  mc-linker-a2 -> $LINKER_A2_ADDR"

echo ""
echo "=== Creating chain-b-only keys ==="
OWNER_B_ADDR=$(create_chain_b_only_key "mc-owner-b")
echo "  mc-owner-b -> $OWNER_B_ADDR"

# ------------------------------------------------------------------
# Sanity check: addresses must differ between chains
# ------------------------------------------------------------------
# A chain-a-only key should not resolve on chain-b
if keys_b show mc-linker-a -a >/dev/null 2>&1; then
    echo "ERROR: mc-linker-a leaked into chain-b keyring (homes are sharing keyring!)"
    exit 1
fi
if keys_a show mc-owner-b -a >/dev/null 2>&1; then
    echo "ERROR: mc-owner-b leaked into chain-a keyring"
    exit 1
fi
echo ""
echo "  Distinct-key sanity check passed."

# ------------------------------------------------------------------
# Fund + invite
# ------------------------------------------------------------------
# Use the binary directly here — `keys show` does not accept --node /
# --chain-id, so cli_a/cli_b's blanket flag list would error out.
ALICE_A_ADDR=$("$BINARY" keys show alice -a --keyring-backend test --home "$CHAIN_A_HOME")
ALICE_B_ADDR=$("$BINARY" keys show alice -a --keyring-backend test --home "$CHAIN_B_HOME")

echo ""
echo "=== Funding and inviting on chain-a ==="
fund_a "$LINKER_A_ADDR"
fund_a "$LINKER_A2_ADDR"
invite_and_accept a alice "$ALICE_A_ADDR" "$LINKER_A_ADDR"  mc-linker-a
invite_and_accept a alice "$ALICE_A_ADDR" "$LINKER_A2_ADDR" mc-linker-a2

echo ""
echo "=== Funding and inviting on chain-b ==="
fund_b "$OWNER_B_ADDR"
invite_and_accept b alice "$ALICE_B_ADDR" "$OWNER_B_ADDR" mc-owner-b

# ------------------------------------------------------------------
# Persist addresses for downstream tests
# ------------------------------------------------------------------
cat > "$SCRIPT_DIR/.chain_keys" <<EOF
LINKER_A_ADDR=$LINKER_A_ADDR
LINKER_A2_ADDR=$LINKER_A2_ADDR
OWNER_B_ADDR=$OWNER_B_ADDR
EOF

echo ""
echo "=================================================="
echo "  CHAIN KEYS SETUP COMPLETE"
echo "=================================================="
echo ""
echo "  chain-a:"
echo "    mc-linker-a  : $LINKER_A_ADDR"
echo "    mc-linker-a2 : $LINKER_A2_ADDR"
echo "  chain-b:"
echo "    mc-owner-b   : $OWNER_B_ADDR"
echo ""
echo "  Saved to: $SCRIPT_DIR/.chain_keys"
echo ""
