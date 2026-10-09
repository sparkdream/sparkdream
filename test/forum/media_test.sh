#!/bin/bash

echo "--- TESTING: FORUM MEDIA LABELS, WITHHOLDING, POST-CONTENT, MEDIA RULES ---"
# docs/content-scanning.md §3 (labels/withholding) and §9 (media rules).

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

wait_for_tx() {
    local ATTEMPT=0
    while [ $ATTEMPT -lt 20 ]; do
        RESULT=$($BINARY q tx $1 --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done
    return 1
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash' 2>/dev/null)
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    if [ "$(echo "$TX_RES" | jq -r '.code // "0"')" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

tx_code() { echo "$1" | jq -r '.code // "1"' 2>/dev/null; }
raw_log() { echo "$1" | jq -r '.raw_log // empty' 2>/dev/null; echo "$1" | grep -o 'media not permitted[^"]*' | head -1; }
extract_event_value() {
    echo "$1" | jq -r ".events[] | select(.type==\"$2\") | .attributes[] | select(.key==\"$3\") | .value" 2>/dev/null | tr -d '"' | head -1
}
b64_sha256() { printf '%s' "$1" | sha256sum | cut -d' ' -f1 | xxd -r -p | base64; }

PASS_COUNT=0
FAIL_COUNT=0
pass() { echo "  [ OK ] $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "  [FAIL] $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
warn() { echo "  [WARN] $1"; }

CATEGORY_ID=$($BINARY query commons list-category --output json 2>&1 | jq -r '.category[0].category_id // "0"')

create_post() { # from content content_type [extra flags...]
    local FROM=$1 CONTENT=$2 CT=$3
    shift 3
    $BINARY tx forum create-post "$CATEGORY_ID" 0 "$CONTENT" --content-type "$CT" "$@" \
        --from "$FROM" --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} --gas 500000 -y --output json 2>&1
}

PARAMS=$($BINARY query forum params --output json 2>&1)
MIN_TRUST=$(echo "$PARAMS" | jq -r '.params.media_min_trust_level // "0"')
echo "  category=$CATEGORY_ID media_min_trust_level=$MIN_TRUST"

# ========================================================================
# TEST 1: inline data URI labelled, withheld, readable via post-content
# ========================================================================
echo "--- TEST 1: Inline data URI is labelled and withheld ---"
INLINE='<p>phoenix</p><img src="data:image/gif;base64,R0lGODlhAQABAAAAACw=">'
TX_RES=$(create_post alice "$INLINE" html)
POST_ID=""
if submit_tx_and_wait "$TX_RES" && [ "$(tx_code "$TX_RESULT")" == "0" ]; then
    POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
fi
if [ -z "$POST_ID" ]; then
    fail "create media post: $(raw_log "$TX_RESULT")"
else
    GOT=$($BINARY query forum get-post "$POST_ID" --output json 2>&1)
    FLAGS=$(echo "$GOT" | jq -r '.post.media_flags // "0"')
    CONTENT=$(echo "$GOT" | jq -r '.post.content // ""')
    HASH=$(echo "$GOT" | jq -r '.post.body_hash // ""')
    if [ "$FLAGS" == "1" ] && [ -z "$CONTENT" ] && [ "$HASH" == "$(b64_sha256 "$INLINE")" ]; then
        pass "get-post: media_flags=1, content withheld, body_hash matches"
    else
        fail "get-post: flags=$FLAGS content='$CONTENT' hash=$HASH"
    fi

    THREAD=$($BINARY query forum thread "$POST_ID" --output json 2>&1 | jq -r '.posts[0].content // ""')
    if [ -z "$THREAD" ]; then
        pass "thread withholds the content"
    else
        fail "thread returned the content"
    fi

    FULL=$($BINARY query forum post-content "$POST_ID" --output json 2>&1)
    if [ "$(echo "$FULL" | jq -r '.content // ""')" == "$INLINE" ]; then
        pass "post-content returns the stored content"
    else
        fail "post-content: $(echo "$FULL" | head -c 300)"
    fi

    # Editing back to plain text clears the label.
    TX_RES=$($BINARY tx forum edit-post "$POST_ID" "plain words now" --content-type text \
        --from alice --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --gas 500000 -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && [ "$(tx_code "$TX_RESULT")" == "0" ]; then
        GOT=$($BINARY query forum get-post "$POST_ID" --output json 2>&1)
        if [ "$(echo "$GOT" | jq -r '.post.media_flags // "0"')" == "0" ] && [ "$(echo "$GOT" | jq -r '.post.content // ""')" == "plain words now" ]; then
            pass "label cleared on edit"
        else
            fail "after edit: $(echo "$GOT" | jq -c '.post | {media_flags, content}')"
        fi
    else
        warn "edit-post not delivered (edit window or pause): $(raw_log "$TX_RESULT")"
    fi
fi

# ========================================================================
# TEST 2: media rules for a low-trust member
# ========================================================================
echo "--- TEST 2: Media from a low-trust member without a bond is refused ---"
TRUST_NAME=$($BINARY query rep get-member "$POSTER2_ADDR" --output json 2>/dev/null | jq -r '.member.trust_level // "TRUST_LEVEL_NEW"')
case "$TRUST_NAME" in
    TRUST_LEVEL_NEW) TRUST=0 ;; TRUST_LEVEL_PROVISIONAL) TRUST=1 ;; TRUST_LEVEL_ESTABLISHED) TRUST=2 ;;
    TRUST_LEVEL_TRUSTED) TRUST=3 ;; TRUST_LEVEL_CORE) TRUST=4 ;; *) TRUST=0 ;;
esac
if [ "$TRUST" -ge "$MIN_TRUST" ]; then
    warn "poster2 is $TRUST_NAME (>= $MIN_TRUST); gate not exercisable here"
else
    TX_RES=$(create_post poster2 "bafyzenith" ipfs)
    submit_tx_and_wait "$TX_RES"
    if [ "$(tx_code "$TX_RESULT")" != "0" ] && raw_log "$TX_RESULT" | grep -q "media not permitted"; then
        pass "unbonded media from $TRUST_NAME rejected"
    else
        fail "unbonded media from $TRUST_NAME: code=$(tx_code "$TX_RESULT") log=$(raw_log "$TX_RESULT")"
    fi
fi

echo ""
echo "Media tests: $PASS_COUNT passed, $FAIL_COUNT failed"
[ "$FAIL_COUNT" -eq 0 ]
