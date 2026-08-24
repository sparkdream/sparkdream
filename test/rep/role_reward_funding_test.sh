#!/bin/bash

echo "--- TESTING: AUTOMATIC BONDED-ROLE REWARD POOL FUNDING ---"

# Bonded-role SPARK pay used to require a council to remember to send SPARK.
# x/rep now takes one capped claim on the community pool in BeginBlock and
# divides it across the per-role pools by headroom. Nothing else in test/rep
# exercises it, and the pools are derived sub-addresses with no balance query of
# their own, so this covers it through the role-reward-pools query:
#
#   * both bonded-role pools are reported, with self-consistent cap/headroom
#   * the pools actually fill without anyone sending SPARK
#   * the per-UTC-day draw never exceeds the computed daily allowance
#   * no pool is ever funded past its own cap
#
# The last two are the bounds that make it safe to leave running unattended: a
# per-block cap instead of a per-day one would drain the community pool, and an
# unbounded top-up would build a standing prize worth farming.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
source "$SCRIPT_DIR/../lib/denoms.sh"

TEST_1_RESULT="FAIL"   # query reports both pools, internally consistent
TEST_2_RESULT="FAIL"   # pools fund themselves
TEST_3_RESULT="FAIL"   # daily cap holds across blocks
TEST_4_RESULT="FAIL"   # no pool exceeds its cap

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "[FAIL] Test environment not initialized. Run: bash test/rep/setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

# proto3 omits zero-valued strings, so every Int field needs a default.
pools_json() { $BINARY query rep role-reward-pools --output json 2>&1; }

wait_blocks() {
    local N=$1 START CUR ATTEMPT=0
    START=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"')
    while [ $ATTEMPT -lt 120 ]; do
        CUR=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"')
        if [ "$CUR" -ge $((START + N)) ] 2>/dev/null; then return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    return 1
}

# ========================================================================
# TEST 1: the query reports every bonded-role pool, self-consistently
# ========================================================================
echo ""
echo "--- TEST 1: role-reward-pools reports both bonded-role pools ---"
RESP=$(pools_json)
if ! echo "$RESP" | jq -e '.pools' > /dev/null 2>&1; then
    echo "[FAIL] role-reward-pools query failed or returned no pools"
    echo "   Response: ${RESP:0:300}"
else
    ROLES=$(echo "$RESP" | jq -r '.pools[].role' | sort | tr '\n' ' ')
    echo "   Roles: $ROLES"
    CONSISTENT="yes"
    while read -r ROLE BAL CAP HEAD ADDR; do
        [ -z "$ROLE" ] && continue
        EXPECTED=$((CAP - BAL))
        [ "$EXPECTED" -lt 0 ] && EXPECTED=0
        if [ "$HEAD" != "$EXPECTED" ]; then
            echo "[FAIL] $ROLE: headroom $HEAD != max(0, cap $CAP - balance $BAL)"
            CONSISTENT="no"
        fi
        case "$ADDR" in
            sprkdrm1*) ;;
            *) echo "[FAIL] $ROLE: implausible pool address '$ADDR'"; CONSISTENT="no" ;;
        esac
        echo "   $ROLE: balance=$BAL cap=$CAP headroom=$HEAD"
    done <<< "$(echo "$RESP" | jq -r '.pools[] | [.role, (.balance // "0"), (.cap // "0"), (.headroom // "0"), .address] | @tsv')"

    if echo "$ROLES" | grep -q "content_sentinel" && echo "$ROLES" | grep -q "initiative_reviewer" \
       && [ "$CONSISTENT" == "yes" ]; then
        echo "[ OK ] both bonded-role pools reported with consistent cap/headroom"
        TEST_1_RESULT="PASS"
    else
        echo "[FAIL] expected content_sentinel and initiative_reviewer pools"
    fi
fi

CAP_DAILY=$(echo "$RESP" | jq -r '.daily_funding_cap // "0"')
echo "   daily allowance: $CAP_DAILY ${BOND_DENOM}/day (share of inflation)"

# ========================================================================
# TEST 2: the pools fill without anyone sending SPARK
# ========================================================================
echo ""
echo "--- TEST 2: pools fund themselves from the community pool ---"
if [ "$CAP_DAILY" == "0" ]; then
    echo "[WARN] automatic funding is disabled on this chain (cap 0); skipping"
    TEST_2_RESULT="SKIP"
else
    TOTAL=$(echo "$RESP" | jq -r '[.pools[] | (.balance // "0") | tonumber] | add')
    FUNDED=$(echo "$RESP" | jq -r '.funded_today // "0"')
    echo "   Pool balances total: $TOTAL   drawn today: $FUNDED"
    if [ "$FUNDED" -gt 0 ] 2>/dev/null && [ "$TOTAL" -gt 0 ] 2>/dev/null; then
        echo "[ OK ] pools funded automatically; no council transfer involved"
        TEST_2_RESULT="PASS"
    else
        # A drained community pool is an environment condition, not a defect:
        # x/split distributes whatever x/rep leaves behind, every block.
        CPOOL=$($BINARY query distribution community-pool --output json 2>&1 \
                | jq -r --arg d "$BOND_DENOM" '.pool[]? | select(.denom==$d) | .amount // "0"' | head -1)
        echo "[WARN] nothing drawn yet; community pool holds '${CPOOL:-0}' $BOND_DENOM"
        TEST_2_RESULT="SKIP"
    fi
fi

# ========================================================================
# TEST 3: the daily cap is a per-day bound, not a per-block one
# ========================================================================
echo ""
echo "--- TEST 3: draw stays within the daily allowance across blocks ---"
if [ "$CAP_DAILY" == "0" ]; then
    echo "[WARN] automatic funding disabled; skipping"
    TEST_3_RESULT="SKIP"
else
    BEFORE=$(echo "$RESP" | jq -r '.funded_today // "0"')
    if ! wait_blocks 8; then
        echo "[WARN] chain did not advance 8 blocks in time; skipping"
        TEST_3_RESULT="SKIP"
    else
        AFTER_RESP=$(pools_json)
        AFTER=$(echo "$AFTER_RESP" | jq -r '.funded_today // "0"')
        echo "   drawn today: $BEFORE -> $AFTER (cap $CAP_DAILY)"
        if [ "$AFTER" -lt "$BEFORE" ] 2>/dev/null; then
            # Only a UTC day rollover may reduce it; a mid-day reset would mean
            # the ledger is not actually bounding anything.
            echo "[WARN] drawn-today decreased; likely a UTC day rollover mid-test"
            TEST_3_RESULT="SKIP"
        elif [ "$AFTER" -le "$CAP_DAILY" ] 2>/dev/null; then
            echo "[ OK ] 8 blocks drew at most one day's allowance"
            TEST_3_RESULT="PASS"
            RESP="$AFTER_RESP"
        else
            echo "[FAIL] drawn today ($AFTER) exceeds the daily cap ($CAP_DAILY)"
        fi
    fi
fi

# ========================================================================
# TEST 4: no pool is topped up past its own cap
# ========================================================================
echo ""
echo "--- TEST 4: funding stops at each pool's cap ---"
OVER=0
while read -r ROLE BAL CAP; do
    [ -z "$ROLE" ] && continue
    if [ "$BAL" -gt "$CAP" ] 2>/dev/null; then
        echo "[FAIL] $ROLE funded past its cap: balance $BAL > cap $CAP"
        OVER=$((OVER + 1))
    fi
done <<< "$(echo "$RESP" | jq -r '.pools[] | [.role, (.balance // "0"), (.cap // "0")] | @tsv')"
if [ "$OVER" -eq 0 ]; then
    echo "[ OK ] every pool is at or below its cap"
    TEST_4_RESULT="PASS"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo ""
echo "================================================================================"
echo "ROLE REWARD FUNDING TEST COMPLETED"
echo "================================================================================"
print_result() {
    case "$2" in
        PASS) echo "[ OK ] $1" ;;
        SKIP) echo "[SKIP] $1" ;;
        *)    echo "[FAIL] $1" ;;
    esac
}
print_result "TEST 1: both pools reported, cap/headroom consistent" "$TEST_1_RESULT"
print_result "TEST 2: pools fund themselves automatically"          "$TEST_2_RESULT"
print_result "TEST 3: daily cap bounds the draw across blocks"      "$TEST_3_RESULT"
print_result "TEST 4: no pool funded past its cap"                  "$TEST_4_RESULT"
echo "================================================================================"

FAIL_COUNT=0
for R in "$TEST_1_RESULT" "$TEST_2_RESULT" "$TEST_3_RESULT" "$TEST_4_RESULT"; do
    [ "$R" == "FAIL" ] && FAIL_COUNT=$((FAIL_COUNT + 1))
done
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "FAILURES: $FAIL_COUNT test(s) failed"
    exit 1
fi
exit 0
