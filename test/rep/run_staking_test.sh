#!/bin/bash

set -e

echo "=========================================="
echo "Staking Test - Full Run with Chain Restore"
echo "=========================================="
echo ""

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# 1. Restore chain
echo "→ Restoring chain snapshot..."
cd "$SCRIPT_DIR/snapshots/post-setup"
bash restore.sh > /dev/null 2>&1
echo "✅ Chain restored"
echo ""

# 2. Start chain
echo "→ Starting chain..."
cd "$SCRIPT_DIR/../.."
pkill -9 sparkdreamd 2>/dev/null || true
sleep 2
sparkdreamd start --home ~/.sparkdream > /tmp/sparkdreamd.log 2>&1 &
CHAIN_PID=$!
echo "✅ Chain starting in background (PID: $CHAIN_PID)"
echo ""

# 3. Wait for chain
echo "→ Waiting for chain to initialize..."
for i in {1..30}; do
    BLOCK=$(sparkdreamd status 2>&1 | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
    if [ "$BLOCK" != "0" ] && [ "$BLOCK" != "null" ]; then
        echo "✅ Chain ready at block $BLOCK"
        echo ""
        break
    fi
    echo -n "."
    sleep 1
done

# Verify chain is actually running
BLOCK=$(sparkdreamd status 2>&1 | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
if [ "$BLOCK" = "0" ] || [ "$BLOCK" = "null" ]; then
    echo ""
    echo "❌ Chain failed to start within 30 seconds"
    echo "Check logs: tail -50 /tmp/sparkdreamd.log"
    exit 1
fi

echo ""

# 4. Run test
echo "→ Running staking test..."
echo ""
cd "$SCRIPT_DIR"
bash staking_test.sh 2>&1 | tee staking_test_output.log

# 5. Summary
echo ""
echo "=========================================="
echo "Test Results"
echo "=========================================="
SUCCESSES=$(grep "✅.*stake #" staking_test_output.log 2>/dev/null | wc -l || echo "0")
FAILURES=$(grep "❌.*creation failed" staking_test_output.log 2>/dev/null | wc -l || echo "0")

echo "Successful stakes: $SUCCESSES/6"
echo "Failed stakes: $FAILURES"
echo ""

if [ "$SUCCESSES" = "6" ]; then
    echo "✅✅✅ ALL TESTS PASSED ✅✅✅"
    echo ""
    echo "Stake creations:"
    grep "✅.*stake #" staking_test_output.log
    echo ""
    exit 0
else
    echo "⚠️  Some stakes failed - check details above"
    echo ""
    if [ "$FAILURES" -gt 0 ]; then
        echo "Failed stakes:"
        grep "❌" staking_test_output.log | head -10
    fi
    echo ""
    exit 1
fi
