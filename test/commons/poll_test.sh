#!/bin/bash

echo "--- TESTING: COMMONS POLLS ---"
echo ""
echo "Open single-choice polls in the Commons Council: one final vote per"
echo "member, members only, validation, creator cancel before any vote, and"
echo "closing at the deadline (block time)."
echo ""

source "$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/_ballot_helpers.sh"

COUNCIL="Commons Council"
OUTSIDER=$($BINARY keys show eve -a --keyring-backend test 2>/dev/null)

# ---------------------------------------------------------------------------
echo "--- TEST 1: A member opens a poll ---"
# 120 s: long enough that the poll is still open through tests 2-5 (each
# tx waits ~7 s), short enough for test 6 to see it close.
tx alice commons open-poll "$COUNCIL" 120 "Where should the meetup be?" "phoenix hall" "aurora park" "zenith dome"
POLL_ID=$(extract_event_value "$TX_RESULT" "poll_opened" "poll_id")
POLL=$($BINARY query commons get-poll "$POLL_ID" --output json 2>/dev/null)
STATUS=$(echo "$POLL" | jq -r '.poll.status')
DEADLINE=$(echo "$POLL" | jq -r '.poll.deadline')
echo "  poll=$POLL_ID status=$STATUS deadline=$DEADLINE"
if check_tx_success "$TX_RESULT" && [ "$STATUS" == "POLL_STATUS_OPEN" ]; then
    record_result "Poll opened" "PASS"
else
    echo "  $(raw_log | head -c 200)"
    record_result "Poll opened" "FAIL"
    print_summary "COMMONS POLL TEST"
fi

# ---------------------------------------------------------------------------
echo "--- TEST 2: Members vote once ---"
tx alice commons vote-poll "$POLL_ID" 1
A_OK=$(check_tx_success "$TX_RESULT" && echo yes)
tx bob commons vote-poll "$POLL_ID" 1
B_OK=$(check_tx_success "$TX_RESULT" && echo yes)
if [ "$A_OK" == "yes" ] && [ "$B_OK" == "yes" ]; then
    record_result "Members vote" "PASS"
else
    echo "  $(raw_log | head -c 200)"
    record_result "Members vote" "FAIL"
fi
tx alice commons vote-poll "$POLL_ID" 2
expect_refusal "Second vote refused" "already voted on this poll"

# ---------------------------------------------------------------------------
echo "--- TEST 3: Non-members and bad options are refused ---"
if [ -n "$OUTSIDER" ]; then
    # Fund the outsider so the refusal is about membership, not fees.
    tx alice bank send "$ALICE_ADDR" "$OUTSIDER" 5000000${BOND_DENOM}
    tx eve commons vote-poll "$POLL_ID" 0
    expect_refusal "Non-member vote refused" "is not a member of"
fi
tx bob commons vote-poll "$POLL_ID" 7
expect_refusal "Out-of-range option refused" "out of range"
tx alice commons open-poll "$COUNCIL" 30 "One option?" "only"
expect_refusal "Single-option poll refused" "invalid poll"

# ---------------------------------------------------------------------------
echo "--- TEST 4: The creator cancels a poll nobody voted on ---"
tx alice commons open-poll "$COUNCIL" 30 "Cancel me?" "yes" "no"
CANCEL_ID=$(extract_event_value "$TX_RESULT" "poll_opened" "poll_id")
tx bob commons cancel-poll "$CANCEL_ID"
expect_refusal "Non-creator cancel refused" "only the poll's creator"
tx alice commons cancel-poll "$CANCEL_ID"
STATUS=$($BINARY query commons get-poll "$CANCEL_ID" --output json 2>/dev/null | jq -r '.poll.status')
if check_tx_success "$TX_RESULT" && [ "$STATUS" == "POLL_STATUS_CANCELLED" ]; then
    record_result "Creator cancels" "PASS"
else
    record_result "Creator cancels" "FAIL"
fi
tx alice commons cancel-poll "$POLL_ID"
expect_refusal "Poll with votes cannot be cancelled" "cannot be cancelled"

# ---------------------------------------------------------------------------
echo "--- TEST 5: Queries list the poll and its votes ---"
VOTES=$($BINARY query commons get-poll-votes "$POLL_ID" --output json 2>/dev/null | jq -r '.votes | length')
LISTED=$($BINARY query commons list-polls --council-name "$COUNCIL" --status open --output json 2>/dev/null | jq -r --arg id "$POLL_ID" '[.polls[] | select(.id==$id)] | length')
echo "  votes=$VOTES listed_open=$LISTED"
if [ "$VOTES" == "2" ] && [ "$LISTED" == "1" ]; then
    record_result "Poll and votes queryable" "PASS"
else
    record_result "Poll and votes queryable" "FAIL"
fi

# ---------------------------------------------------------------------------
echo "--- TEST 6: The poll closes at its deadline ---"
# Poll block time, not wall-clock: the EndBlocker closes the poll in the
# first block after the deadline.
STATUS=""
for _ in $(seq 1 120); do
    STATUS=$($BINARY query commons get-poll "$POLL_ID" --output json 2>/dev/null | jq -r '.poll.status')
    [ "$STATUS" == "POLL_STATUS_CLOSED" ] && break
    sleep 2
done
TALLIES=$($BINARY query commons get-poll "$POLL_ID" --output json 2>/dev/null | jq -c '[.poll.tallies[] | tonumber]')
echo "  status=$STATUS tallies=$TALLIES"
if [ "$STATUS" == "POLL_STATUS_CLOSED" ] && [ "$TALLIES" == "[0,2,0]" ]; then
    record_result "Poll closed with final tallies" "PASS"
else
    record_result "Poll closed with final tallies" "FAIL"
fi
tx bob commons vote-poll "$POLL_ID" 0
expect_refusal "Vote after close refused" "poll is not open for voting"

print_summary "COMMONS POLL TEST"
