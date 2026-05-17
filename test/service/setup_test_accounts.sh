#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/service Tests"
echo "=================================================="
echo ""

# ============================================================================
# Configuration
# ============================================================================
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"

# Service-type key used throughout the suite. Genesis-default has no
# service types — the gov proposal in step 4 enables this one so the
# rest of the tests have something to register against.
TEST_SERVICE_TYPE="test-akash"

# ============================================================================
# Helper Functions (mirrors forum/setup_test_accounts.sh)
# ============================================================================

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0

    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
    return 1
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
        return 1
    fi
    return 0
}

# Get alice (genesis member who pays gas + invites).
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
echo "Genesis member (Alice): $ALICE_ADDR"
echo ""

# Delete stale .test_env so it is regenerated from the current keyring.
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Removing stale .test_env (will be regenerated at end of setup)..."
    rm -f "$SCRIPT_DIR/.test_env"
fi

# ============================================================================
# 1. Create test account keys
# ============================================================================
echo "Step 1: Creating test account keys..."

# operator1 = the primary registered operator across most tests.
# operator2 = a second operator for max_active_operators_per_address /
#             multi-registration paths.
# reporter1 = files reports against operator1; MUST NOT be a member of
#             operator1's controller group (the reporter-not-controller-
#             member rule from §3.4.6). Since our controller is the
#             Commons Council and alice is the sole genesis member,
#             reporter1 just needs to not be alice — any non-Commons
#             account works.
# non_member = an address that is NOT an x/rep member; used to verify
#              the reporter-trust-level gate rejects non-members.
ACCOUNTS=("operator1" "operator2" "reporter1" "non_member")

for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $ACCOUNT"
    else
        echo "  Key exists: $ACCOUNT"
    fi
done

OPERATOR1_ADDR=$($BINARY keys show operator1 -a --keyring-backend test)
OPERATOR2_ADDR=$($BINARY keys show operator2 -a --keyring-backend test)
REPORTER1_ADDR=$($BINARY keys show reporter1 -a --keyring-backend test)
NON_MEMBER_ADDR=$($BINARY keys show non_member -a --keyring-backend test)

echo ""

# ============================================================================
# 2. Fund test accounts with SPARK
# ============================================================================
# Bond min is 1000 SPARK (test-akash service type — see step 4). Each
# operator needs:
#   - 1000 SPARK initial bond
#   - 500 SPARK top-up headroom
#   - 100 SPARK gas budget (~20 txs * 5000 uspark)
# So 2000 SPARK per operator is plenty. Reporters need ~50 SPARK for
# report_deposit (10 SPARK) + gas.
echo "Step 2: Funding test accounts with SPARK..."

declare -A FUND_AMOUNT
FUND_AMOUNT["operator1"]="2000000000"   # 2000 SPARK
FUND_AMOUNT["operator2"]="2000000000"
FUND_AMOUNT["reporter1"]="100000000"    # 100 SPARK
FUND_AMOUNT["non_member"]="50000000"    # 50 SPARK (gas only)

for ACCOUNT in "${ACCOUNTS[@]}"; do
    ADDR_VAR=$(echo "${ACCOUNT}_ADDR" | tr '[:lower:]' '[:upper:]')
    ADDR="${!ADDR_VAR}"
    AMT="${FUND_AMOUNT[$ACCOUNT]}"

    echo "  Sending $AMT uspark to $ACCOUNT ($ADDR)..."
    TX_RES=$($BINARY tx bank send alice $ADDR ${AMT}uspark \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  ERROR: Failed to fund $ACCOUNT"
        continue
    fi
    sleep 6
done
echo ""

# ============================================================================
# 3. Invite test accounts to x/rep (skip non_member intentionally)
# ============================================================================
echo "Step 3: Inviting test accounts to become x/rep members..."

# non_member is deliberately excluded so we can test that reporting
# from a non-member is rejected (reporter trust-level gate).
TO_INVITE=("operator1" "operator2" "reporter1")
INVITATION_IDS=()

for i in "${!TO_INVITE[@]}"; do
    ACCOUNT="${TO_INVITE[$i]}"
    ADDR_VAR=$(echo "${ACCOUNT}_ADDR" | tr '[:lower:]' '[:upper:]')
    ADDR="${!ADDR_VAR}"

    MEMBER_INFO=$($BINARY query rep get-member $ADDR --output json 2>&1)
    if ! echo "$MEMBER_INFO" | grep -q "not found"; then
        echo "  $ACCOUNT is already a member, skipping invitation"
        INVITATION_IDS+=("")
        continue
    fi

    echo "  Inviting $ACCOUNT ($ADDR)..."
    REQUIRED_STAKE=$($BINARY query rep required-invitation-stake "$ALICE_ADDR" --output json 2>/dev/null \
        | jq -r '.required_stake // "100000000"')
    TX_RES=$($BINARY tx rep invite-member $ADDR "$REQUIRED_STAKE" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark -y --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  ERROR: Failed to invite $ACCOUNT"
        INVITATION_IDS+=("")
        continue
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        INVITATION_ID=$(extract_event_value "$TX_RESULT" "create_invitation" "invitation_id")
        if [ -z "$INVITATION_ID" ]; then
            INVITATION_ID=$((i + 1))
        fi
        INVITATION_IDS+=("$INVITATION_ID")
        echo "  Invited $ACCOUNT (invitation #$INVITATION_ID)"
    else
        INVITATION_IDS+=("")
    fi
done
echo ""

# Accept invitations.
echo "Step 3b: Accepting invitations..."
for i in "${!TO_INVITE[@]}"; do
    ACCOUNT="${TO_INVITE[$i]}"
    INVITATION_ID="${INVITATION_IDS[$i]}"
    if [ -z "$INVITATION_ID" ]; then
        continue
    fi
    echo "  $ACCOUNT accepting invitation #$INVITATION_ID..."
    TX_RES=$($BINARY tx rep accept-invitation $INVITATION_ID \
        --from $ACCOUNT --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark -y --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
    fi
done
echo ""

# ============================================================================
# 4. Submit gov proposal enabling the test-akash service type
# ============================================================================
echo "Step 4: Submitting gov proposal to enable '$TEST_SERVICE_TYPE' service type..."

# Skip if already enabled (idempotent re-runs).
EXISTING_CFG=$($BINARY query service service-type "$TEST_SERVICE_TYPE" --output json 2>&1 || true)
if echo "$EXISTING_CFG" | jq -e '.config.service_type' > /dev/null 2>&1; then
    EXISTING_ENABLED=$(echo "$EXISTING_CFG" | jq -r '.config.enabled')
    if [ "$EXISTING_ENABLED" = "true" ]; then
        echo "  Service type '$TEST_SERVICE_TYPE' already enabled — skipping proposal"
        SKIP_PROPOSAL=true
    fi
fi

if [ "$SKIP_PROPOSAL" != "true" ]; then
    GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
    if [ -z "$GOV_ADDR" ] || [ "$GOV_ADDR" == "null" ]; then
        echo "  ERROR: gov module address not found"
        exit 1
    fi
    echo "  Gov authority: $GOV_ADDR"

    # Render the proposal JSON with the live gov address substituted in.
    # Bundles two messages so we only pay one 65s voting period:
    #   1. MsgUpdateServiceTypeConfig — enables the synthetic
    #      "test-akash" service type with short timing knobs.
    #   2. MsgUpdateParams — lowers min_reporter_trust_level from the
    #      default ESTABLISHED to NEW. Newly-invited members start at
    #      TRUST_LEVEL_NEW, and the genesis ladder requires real
    #      reputation+interim work to climb out — far too much to bake
    #      into a chain-startup script. All other params keep their
    #      defaults.
    mkdir -p "$PROPOSAL_DIR"
    cat > "$PROPOSAL_DIR/enable_test_service_type.json" <<EOF
{
  "messages": [
    {
      "@type": "/sparkdream.service.v1.MsgUpdateServiceTypeConfig",
      "authority": "$GOV_ADDR",
      "config": {
        "service_type": "$TEST_SERVICE_TYPE",
        "description": "Synthetic service type for E2E tests of x/service. Mirrors the akash-funding shape: SPARK-bonded operators paid via recurring spend.",
        "min_bond": {"denom": "uspark", "amount": "1000000000"},
        "unbonding_period_blocks": "20",
        "unilateral_slash_cap_bps": 500,
        "tier1_window_blocks": "1000",
        "tier1_aggregate_cap_bps": 1500,
        "tier1_cooldown_blocks": "10",
        "underfunded_grace_blocks": "10",
        "enabled": true
      }
    },
    {
      "@type": "/sparkdream.service.v1.MsgUpdateParams",
      "authority": "$GOV_ADDR",
      "params": {
        "default_unbonding_period_blocks": "241920",
        "default_unilateral_slash_cap_bps": 500,
        "default_tier1_window_blocks": "1555200",
        "default_tier1_aggregate_cap_bps": 1500,
        "default_tier1_cooldown_blocks": "120960",
        "default_underfunded_grace_blocks": "120960",
        "report_contest_window_blocks": "17280",
        "max_pending_blocks": "518400",
        "max_escalated_blocks": "1036800",
        "report_refile_cooldown_blocks": "518400",
        "report_deposit": {"denom": "uspark", "amount": "10000000"},
        "min_reporter_trust_level": "TRUST_LEVEL_NEW",
        "max_reports_per_reporter_per_operator_per_window": 3,
        "reporter_rate_limit_window_blocks": "518400",
        "endblocker_sweep_limit": 100,
        "max_metadata_bytes": 4096,
        "max_reason_bytes": 512,
        "max_active_operators_per_address": 16,
        "reputation_grant_per_bond_block": "0.000000000001",
        "default_pagination_limit": 100,
        "max_pagination_limit": 1000
      }
    }
  ],
  "deposit": "50000000uspark",
  "title": "Enable test-akash service type + lower reporter trust gate for E2E tests",
  "summary": "Enables the test-akash service type with 1000-SPARK min_bond and short timing knobs (20-block unbonding, 10-block tier-1 cooldown), and lowers min_reporter_trust_level to TRUST_LEVEL_NEW so freshly-invited test reporters can file reports without a full reputation ladder."
}
EOF

    SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/enable_test_service_type.json" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark -y --output json 2>&1)
    PROP_TX_HASH=$(echo "$SUBMIT_RES" | jq -r '.txhash')
    sleep 5
    PROP_TX_RESULT=$(wait_for_tx "$PROP_TX_HASH")
    if ! check_tx_success "$PROP_TX_RESULT"; then
        echo "  ERROR: gov submit-proposal failed"
        exit 1
    fi
    PROP_ID=$(extract_event_value "$PROP_TX_RESULT" "submit_proposal" "proposal_id")
    if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
        echo "  ERROR: could not extract proposal_id"
        exit 1
    fi
    echo "  Submitted proposal $PROP_ID"

    # Vote yes.
    echo "  Alice voting YES..."
    VOTE_RES=$($BINARY tx gov vote "$PROP_ID" yes \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark -y --output json 2>&1)
    sleep 6

    # Wait for the voting period (config.yml: 60s).
    echo "  Waiting 65s for voting period..."
    sleep 65

    # Verify it passed and the service type is now enabled.
    PROP_STATUS=$($BINARY query gov proposal "$PROP_ID" --output json 2>&1 | jq -r '.proposal.status // "MISSING"')
    if [ "$PROP_STATUS" != "PROPOSAL_STATUS_PASSED" ]; then
        echo "  ERROR: proposal status is $PROP_STATUS, expected PROPOSAL_STATUS_PASSED"
        exit 1
    fi
    echo "  Proposal passed"
fi

sleep 3
SERVICE_CFG=$($BINARY query service service-type "$TEST_SERVICE_TYPE" --output json 2>&1)
if ! echo "$SERVICE_CFG" | jq -e '.config.enabled == true' > /dev/null 2>&1; then
    echo "  ERROR: service type '$TEST_SERVICE_TYPE' is not enabled after proposal"
    echo "$SERVICE_CFG"
    exit 1
fi
echo "  Service type '$TEST_SERVICE_TYPE' enabled (min_bond: $(echo $SERVICE_CFG | jq -r '.config.min_bond.amount') uspark)"
echo ""

# ============================================================================
# 5. Resolve the Commons Council group policy address (operator controller)
# ============================================================================
# Operators registered in tests will use this as their controller. The
# Commons Council is genesis-seeded with alice as a member, so alice is
# the only address that could potentially conflict with the
# reporter-not-controller-member rule — reporter1 (separate account)
# is safe.
echo "Step 5: Resolving Commons Council group policy address..."

COMMONS_GROUP_INFO=$($BINARY query commons get-group "Commons Council" --output json 2>&1)
COMMONS_POLICY=$(echo "$COMMONS_GROUP_INFO" | jq -r '.group.policy_address // empty')
if [ -z "$COMMONS_POLICY" ] || [ "$COMMONS_POLICY" == "null" ]; then
    echo "  ERROR: Could not resolve Commons Council policy address"
    echo "  $COMMONS_GROUP_INFO"
    exit 1
fi
echo "  Commons Council policy: $COMMONS_POLICY"
echo ""

# ============================================================================
# 6. Export environment
# ============================================================================
cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables for x/service tests
export ALICE_ADDR=$ALICE_ADDR
export OPERATOR1_ADDR=$OPERATOR1_ADDR
export OPERATOR2_ADDR=$OPERATOR2_ADDR
export REPORTER1_ADDR=$REPORTER1_ADDR
export NON_MEMBER_ADDR=$NON_MEMBER_ADDR
export TEST_SERVICE_TYPE=$TEST_SERVICE_TYPE
export COMMONS_POLICY=$COMMONS_POLICY
EOF

echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "Environment variables saved to: $SCRIPT_DIR/.test_env"
echo "  ALICE_ADDR:           $ALICE_ADDR"
echo "  OPERATOR1_ADDR:       $OPERATOR1_ADDR"
echo "  OPERATOR2_ADDR:       $OPERATOR2_ADDR"
echo "  REPORTER1_ADDR:       $REPORTER1_ADDR"
echo "  NON_MEMBER_ADDR:      $NON_MEMBER_ADDR"
echo "  TEST_SERVICE_TYPE:    $TEST_SERVICE_TYPE"
echo "  COMMONS_POLICY:       $COMMONS_POLICY"
echo ""
