#!/bin/bash
# ============================================================================
# SHARED DENOM RESOLUTION FOR E2E TESTS
# ============================================================================
# Resolves BOND_DENOM and DREAM_DENOM from the running chain's x/identity
# module so tests do not hardcode "uspark" / "udream" and stay portable
# across federated chains (Sparkdream, Phoenix, Aurora, sparkdream-test-1,
# etc.).
#
# Resolution order (first match wins):
#   1. BOND_DENOM / DREAM_DENOM already exported by the parent shell
#   2. `sparkdreamd q identity bond-denom` / `dream-denom` against a live
#      chain (the common case — `run_all_tests.sh` sources this after the
#      chain is up)
#   3. Hardcoded fallback (`uspark` / `udream`) for environments where
#      neither chain nor identity module is available — keeps unit-tested
#      scripts working without a chain
#
# Usage from a test script:
#   SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
#   source "$SCRIPT_DIR/../lib/denoms.sh"
#   $BINARY tx bank send alice bob 100${BOND_DENOM} --fees 5000${BOND_DENOM} ...
#
# Spec reference: docs/x-identity-spec.md §13.3
# ============================================================================

# Allow caller-set values to win; otherwise probe the chain, otherwise fall
# back to the canonical sparkdream defaults. Probes use --output json + jq
# rather than the raw query so we get the inner .denom string.
_BINARY="${BINARY:-sparkdreamd}"

if [ -z "${BOND_DENOM:-}" ]; then
    BOND_DENOM=$("$_BINARY" q identity bond-denom --output json 2>/dev/null | jq -r '.denom // ""' 2>/dev/null)
    if [ -z "$BOND_DENOM" ] || [ "$BOND_DENOM" = "null" ]; then
        BOND_DENOM="uspark.sparkdream"
    fi
fi

if [ -z "${DREAM_DENOM:-}" ]; then
    DREAM_DENOM=$("$_BINARY" q identity dream-denom --output json 2>/dev/null | jq -r '.denom // ""' 2>/dev/null)
    if [ -z "$DREAM_DENOM" ] || [ "$DREAM_DENOM" = "null" ]; then
        DREAM_DENOM="udream.sparkdream"
    fi
fi

export BOND_DENOM DREAM_DENOM
unset _BINARY
