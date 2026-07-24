#!/bin/bash
# ============================================================================
# X/IDENTITY MODULE: GENESIS CLI TESTS
# ============================================================================
# Validates the `sparkdreamd genesis identity ...` helper subcommands. Uses
# an isolated --home directory so the running chain's genesis is left
# untouched. See docs/x-identity-spec.md §15.
# ============================================================================

set -e

BINARY="sparkdreamd"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Isolated test home so we don't disturb the live chain config.
TEST_HOME=$(mktemp -d "/tmp/identity-cli-test.XXXXXX")
trap 'rm -rf "$TEST_HOME"' EXIT

echo "=================================================="
echo "TEST: sparkdreamd genesis identity"
echo "Test home: $TEST_HOME"
echo "=================================================="
echo ""

# ----------------------------------------------------------------------
# 1. Initialize a fresh genesis under the test home.
# ----------------------------------------------------------------------
$BINARY init test-node --chain-id phoenix-1 --home "$TEST_HOME" 2>&1 | tail -3
echo ""

# ----------------------------------------------------------------------
# 2. genesis identity init — derives denoms from chain-name + ticker.
# `sparkdreamd init` now seeds app_state.identity with the binary's
# DefaultChainIdentity, so we MUST pass --force --i-mean-it to overwrite it.
# Spec §15 / x/identity/types/genesis.go.
# ----------------------------------------------------------------------
echo "[1/4] genesis identity init"
$BINARY genesis identity init \
    --chain-name Phoenix \
    --ticker-prefix PHX \
    --bond-symbol PSPK \
    --dream-symbol PDRM \
    --home "$TEST_HOME" \
    --force --i-mean-it

# Show should now print the just-set identity.
SHOWN=$($BINARY genesis identity show --home "$TEST_HOME")
echo "$SHOWN"

GOT_BOND_DENOM=$(echo "$SHOWN" | jq -r '.identity.bond_denom')
GOT_DREAM_DENOM=$(echo "$SHOWN" | jq -r '.identity.dream_denom')
if [ "$GOT_BOND_DENOM" != "upspk.phoenix" ]; then
    echo "FAIL: derived bond_denom = $GOT_BOND_DENOM, want upspk.phoenix"
    exit 1
fi
if [ "$GOT_DREAM_DENOM" != "udream.phoenix" ]; then
    echo "FAIL: derived dream_denom = $GOT_DREAM_DENOM, want udream.phoenix"
    exit 1
fi
echo "PASS: derived denoms"
echo ""

# ----------------------------------------------------------------------
# 3. genesis identity validate — accepts good identity.
# ----------------------------------------------------------------------
echo "[2/4] genesis identity validate (happy path)"
$BINARY genesis identity validate --home "$TEST_HOME"
echo "PASS: validation succeeded"
echo ""

# ----------------------------------------------------------------------
# 4. genesis identity init without overwrite flags must be rejected.
# ----------------------------------------------------------------------
echo "[3/4] init without --force --i-mean-it must be rejected on existing identity"
set +e
$BINARY genesis identity init \
    --chain-name Aurora \
    --ticker-prefix AUR \
    --bond-symbol ASPK \
    --dream-symbol ADRM \
    --home "$TEST_HOME" 2>/tmp/identity-init-fail.log
RC=$?
set -e
if [ $RC -eq 0 ]; then
    echo "FAIL: overwrite without force unexpectedly succeeded"
    cat /tmp/identity-init-fail.log
    exit 1
fi
grep -qi "force" /tmp/identity-init-fail.log || (
    echo "FAIL: overwrite-rejection message did not mention --force"
    cat /tmp/identity-init-fail.log
    exit 1
)
echo "PASS: overwrite refused without --force --i-mean-it"
echo ""

# ----------------------------------------------------------------------
# 5. Overwrite WITH --force --i-mean-it succeeds (and changes denoms).
# ----------------------------------------------------------------------
echo "[4/4] init with --force --i-mean-it overwrites"
$BINARY genesis identity init \
    --chain-name Aurora \
    --ticker-prefix AUR \
    --bond-symbol ASPK \
    --dream-symbol ADRM \
    --home "$TEST_HOME" \
    --force --i-mean-it 2>/tmp/identity-init-overwrite.log

POST=$($BINARY genesis identity show --home "$TEST_HOME")
POST_BOND_DENOM=$(echo "$POST" | jq -r '.identity.bond_denom')
if [ "$POST_BOND_DENOM" != "uaspk.aurora" ]; then
    echo "FAIL: post-overwrite bond_denom = $POST_BOND_DENOM, want uaspk.aurora"
    exit 1
fi
echo "PASS: overwrite path works with full confirmation"
echo ""

# ----------------------------------------------------------------------
# 6. Non-default decimals require --confirm-non-default-decimals.
# ----------------------------------------------------------------------
TEST_HOME_DEC=$(mktemp -d "/tmp/identity-decimals-test.XXXXXX")
trap 'rm -rf "$TEST_HOME_DEC" "$TEST_HOME"' EXIT
$BINARY init test-node --chain-id phoenix-1 --home "$TEST_HOME_DEC" 2>&1 | tail -1

set +e
$BINARY genesis identity init \
    --chain-name Phoenix --ticker-prefix PHX \
    --bond-symbol PSPK --dream-symbol PDRM \
    --decimals 18 \
    --home "$TEST_HOME_DEC" 2>/tmp/identity-init-decimals.log
RC=$?
set -e
if [ $RC -eq 0 ]; then
    echo "FAIL: non-default decimals accepted without explicit confirmation"
    exit 1
fi
grep -qi "irreversibility\|confirm-non-default-decimals" /tmp/identity-init-decimals.log || (
    echo "FAIL: decimals-refusal message did not surface irreversibility"
    cat /tmp/identity-init-decimals.log
    exit 1
)
echo "PASS: non-default decimals required explicit acknowledgement"
echo ""

# ----------------------------------------------------------------------
# 7. Custom dream-denom prefix. The dream denom follows the same
# u<2-5 letters>.<suffix> shape rule as the bond denom (spec section 3.3);
# udream.<chainname> is only the derived default, so an explicit
# --dream-denom with a different prefix must be accepted. Chain name stays
# Phoenix so `validate` (which also runs the soft chain_human_name vs
# chain_id=phoenix-1 check, unlike init) passes — the point under test is
# the dream-denom prefix, not the chain name.
# ----------------------------------------------------------------------
echo "custom --dream-denom prefix accepted (shared shape rule)"
$BINARY genesis identity init \
    --chain-name Phoenix \
    --ticker-prefix PHX \
    --bond-symbol PSPK \
    --dream-symbol WISH \
    --dream-denom uwish.phoenix \
    --home "$TEST_HOME" \
    --force --i-mean-it

CUSTOM=$($BINARY genesis identity show --home "$TEST_HOME")
GOT_CUSTOM_DREAM=$(echo "$CUSTOM" | jq -r '.identity.dream_denom')
if [ "$GOT_CUSTOM_DREAM" != "uwish.phoenix" ]; then
    echo "FAIL: custom dream_denom = $GOT_CUSTOM_DREAM, want uwish.phoenix"
    exit 1
fi
$BINARY genesis identity validate --home "$TEST_HOME"
echo "PASS: custom dream-denom prefix accepted and validates"
echo ""

# ----------------------------------------------------------------------
# 8. Dream denom violating the shared shape rule (more than 5 letters
# between u and the dot) must be rejected at init.
# ----------------------------------------------------------------------
echo "malformed --dream-denom rejected"
set +e
$BINARY genesis identity init \
    --chain-name Phoenix \
    --ticker-prefix PHX \
    --bond-symbol PSPK \
    --dream-symbol WISH \
    --dream-denom uwishesx.phoenix \
    --home "$TEST_HOME" \
    --force --i-mean-it 2>/tmp/identity-init-baddream.log
RC=$?
set -e
if [ $RC -eq 0 ]; then
    echo "FAIL: malformed dream denom uwishesx.aurora unexpectedly accepted"
    exit 1
fi
grep -qi "dream" /tmp/identity-init-baddream.log || (
    echo "FAIL: rejection message did not mention the dream denom"
    cat /tmp/identity-init-baddream.log
    exit 1
)
echo "PASS: malformed dream denom rejected"
echo ""

echo "=================================================="
echo "TEST PASSED: sparkdreamd genesis identity"
echo "=================================================="
