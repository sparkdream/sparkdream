#!/bin/bash
#
# Run all archival integration tests.
#
# Usage:
#   ./test/archival/run_all_tests.sh                  # run all
#   ./test/archival/run_all_tests.sh pinata           # run only pinata
#   ./test/archival/run_all_tests.sh pinata filebase  # run specific tests
#
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

TESTS=(replay manifest pinata filebase storacha jackal arweave)

# If arguments given, use them as the test list
if [ $# -gt 0 ]; then
    TESTS=("$@")
fi

TOTAL=0
PASSED=0
FAILED=0
SKIPPED=0

for TEST in "${TESTS[@]}"; do
    TEST_FILE="$SCRIPT_DIR/${TEST}_test.sh"
    if [ ! -f "$TEST_FILE" ]; then
        echo "WARNING: Test file not found: $TEST_FILE"
        continue
    fi

    echo ""
    echo "================================================================"
    echo " Running: ${TEST}_test.sh"
    echo "================================================================"
    echo ""

    TOTAL=$(( TOTAL + 1 ))

    if bash "$TEST_FILE"; then
        PASSED=$(( PASSED + 1 ))
    else
        EXIT_CODE=$?
        # Exit code 0 = pass, 1 = fail, anything else = skip/error
        if [ $EXIT_CODE -eq 1 ]; then
            FAILED=$(( FAILED + 1 ))
        fi
    fi
done

echo ""
echo "================================================================"
echo " Archival Integration Test Summary"
echo "================================================================"
echo "  Total:   $TOTAL"
echo "  Passed:  $PASSED"
echo "  Failed:  $FAILED"
echo "================================================================"

if [ "$FAILED" -gt 0 ]; then
    exit 1
fi
