#!/bin/bash

# Module-bypass allowlist coverage for x/session.
#
# The M3 phase of the RecurringSpend migration seeds the commons
# module address into `params.authorized_grant_creators` at genesis,
# so the x/commons RecurringSpend wrapper can host council schedules
# in the unified registry from block 0 without a post-launch gov
# proposal.
#
# This test verifies the genesis-bootstrap state landed correctly:
#   1. `query session params` includes a non-empty
#      `authorized_grant_creators` list.
#   2. The commons module address (from `auth module-account commons`)
#      is in the allowlist — confirming the deterministic
#      `authtypes.NewModuleAddress("commons").String()` seed.
#   3. The allowlist contains exactly the expected genesis entries
#      (currently: just commons). This catches accidental drift if a
#      future PR adds a new module to the bypass surface without
#      updating the migration plan.
#
# Coverage map:
#   - Unit tests (x/session/keeper/public_api_test.go) cover the
#     allowlist gate (TestCreateGrantOnBehalfOf_CallerNotInAllowlist,
#     TestCreateGrantOnBehalfOf_BypassDisabledWhenAllowlistEmpty,
#     TestDeclineGrantInternal_CallerNotAuthorized,
#     TestClaimRecurringPullForGrantee_CallerNotAuthorized,
#     TestDefaultParams_SeedsCommonsModuleInAllowlist). The unit suite
#     is authoritative for the gate logic.
#   - This e2e suite covers the production genesis-bootstrap invariant
#     end-to-end on a live chain.
#   - The cross-module bypass happy-path (commons wrapper → session
#     storage → session query) is covered by
#     test/commons/recurring_spend_session_visibility_test.sh.

set -u

echo "--- TESTING: MODULE-BYPASS ALLOWLIST GENESIS BOOTSTRAP ---"
echo ""

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing) — run setup first"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

PASS_COUNT=0
FAIL_COUNT=0

record_result() {
    local NAME=$1
    local RESULT=$2
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}

# ========================================================================
# TEST 1: authorized_grant_creators is non-empty at genesis
# ========================================================================
echo "--- TEST 1: authorized_grant_creators is non-empty ---"

PARAMS=$($BINARY query session params --output json 2>&1)
ALLOWLIST_LEN=$(echo "$PARAMS" | jq '.params.authorized_grant_creators | length')
if [ "$ALLOWLIST_LEN" -ge 1 ]; then
    echo "  authorized_grant_creators has $ALLOWLIST_LEN entry/entries"
    record_result "Allowlist non-empty at genesis" "PASS"
else
    echo "  authorized_grant_creators is empty — M3 genesis default missing"
    echo "  Raw: $(echo "$PARAMS" | jq '.params.authorized_grant_creators')"
    record_result "Allowlist non-empty at genesis" "FAIL"
fi

# ========================================================================
# TEST 2: commons module address is in the allowlist
# ========================================================================
echo "--- TEST 2: commons module address is in the allowlist ---"

COMMONS_MODULE_ADDR=$($BINARY query auth module-account commons --output json 2>&1 | jq -r '.account.value.address // .account.base_account.address // .account.address // empty')
if [ -z "$COMMONS_MODULE_ADDR" ] || [ "$COMMONS_MODULE_ADDR" == "null" ]; then
    echo "  Could not resolve commons module address from x/auth"
    record_result "commons module addr in allowlist" "FAIL"
else
    echo "  commons module address: $COMMONS_MODULE_ADDR"
    HAS_COMMONS=$(echo "$PARAMS" | jq -r --arg addr "$COMMONS_MODULE_ADDR" '.params.authorized_grant_creators[] | select(. == $addr)' | head -n1)
    if [ "$HAS_COMMONS" == "$COMMONS_MODULE_ADDR" ]; then
        echo "  Allowlist contains commons module address"
        record_result "commons module addr in allowlist" "PASS"
    else
        echo "  commons module address not in authorized_grant_creators"
        echo "  Allowlist: $(echo "$PARAMS" | jq -r '.params.authorized_grant_creators[]')"
        record_result "commons module addr in allowlist" "FAIL"
    fi
fi

# ========================================================================
# TEST 3: Allowlist size matches the expected genesis-default
# ========================================================================
# At genesis the only allowlisted module is commons (the M3 default
# `DefaultAuthorizedGrantCreators()` returns a single-element slice).
# If this test fails with size > 1, either:
#   - a follow-up migration added a new module to the bypass surface
#     and forgot to update this test, OR
#   - a gov proposal post-launch widened the allowlist (in which case
#     the chain isn't pristine and this isn't a fresh genesis snapshot)
# Both warrant a human eyeball. The bypass surface is a strict trust
# grant and accidental drift here matters.
echo "--- TEST 3: Allowlist size matches genesis default (expect 1) ---"

if [ "$ALLOWLIST_LEN" -eq 1 ]; then
    echo "  authorized_grant_creators has exactly 1 entry (expected genesis default)"
    record_result "Allowlist size = 1" "PASS"
elif [ "$ALLOWLIST_LEN" -gt 1 ]; then
    echo "  WARNING: authorized_grant_creators has $ALLOWLIST_LEN entries — drift detected:"
    echo "$PARAMS" | jq -r '.params.authorized_grant_creators[]' | sed 's/^/    /'
    echo "  Either a follow-up migration added a new bypass caller (update this test),"
    echo "  or this isn't a fresh-genesis snapshot."
    record_result "Allowlist size = 1" "FAIL"
else
    echo "  authorized_grant_creators is empty (covered by TEST 1)"
    record_result "Allowlist size = 1" "FAIL"
fi

# ========================================================================
# TEST 4: Allowlist entries are all valid bech32 addresses
# ========================================================================
# Defense-in-depth: every entry in the allowlist must be a parseable
# bech32 address with the chain's prefix. A malformed entry here would
# brick the bypass surface (CreateGrantOnBehalfOf compares
# callerModuleAddr by string equality; a non-bech32 entry would only
# match an equally non-bech32 caller, which can't happen).
echo "--- TEST 4: Allowlist entries are valid bech32 ---"

ALL_VALID=true
while IFS= read -r addr; do
    if [ -z "$addr" ]; then continue; fi
    # Bech32 must start with the chain prefix (sprkdrm). The chain
    # rejects malformed addresses at param-update time, so we just
    # do a coarse prefix check here.
    if [[ "$addr" != sprkdrm* ]]; then
        echo "  Invalid bech32 entry: '$addr'"
        ALL_VALID=false
    fi
done < <(echo "$PARAMS" | jq -r '.params.authorized_grant_creators[]')

if [ "$ALL_VALID" == "true" ]; then
    echo "  All $ALLOWLIST_LEN allowlist entries have the sprkdrm prefix"
    record_result "Allowlist entries are valid bech32" "PASS"
else
    record_result "Allowlist entries are valid bech32" "FAIL"
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "BYPASS ALLOWLIST TEST RESULTS"
echo "============================================"
echo "Passed: $PASS_COUNT"
echo "Failed: $FAIL_COUNT"

if [ "$FAIL_COUNT" -gt 0 ]; then
    exit 1
fi
exit 0
