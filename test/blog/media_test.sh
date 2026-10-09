#!/bin/bash

echo "--- TESTING: BLOG MEDIA LABELS, WITHHOLDING, BODY QUERY, MEDIA RULES ---"
# docs/content-scanning.md §3 (labels/withholding) and §9 (media rules).

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

wait_for_tx() {
    local TXHASH=$1
    local ATTEMPT=0
    while [ $ATTEMPT -lt 20 ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done
    return 1
}

# submit_tx_and_wait sets TX_RESULT to the delivered tx (or the broadcast
# response when the tx was rejected at CheckTx).
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

# sha256 of a string, base64 (the LCD/CLI JSON shape of a proto bytes field).
b64_sha256() { printf '%s' "$1" | sha256sum | cut -d' ' -f1 | xxd -r -p | base64; }

PASS_COUNT=0
FAIL_COUNT=0
pass() { echo "  [ OK ] $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "  [FAIL] $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
warn() { echo "  [WARN] $1"; }

create_post() { # creator title body content_type [extra flags...]
    local FROM=$1 TITLE=$2 BODY=$3 CT=$4
    shift 4
    $BINARY tx blog create-post "$TITLE" "$BODY" --content-type "$CT" "$@" \
        --from "$FROM" --chain-id $CHAIN_ID --keyring-backend test \
        --fees 50000${BOND_DENOM} --gas 400000 -y --output json 2>&1
}

PARAMS=$($BINARY query blog params --output json 2>&1)
MIN_TRUST=$(echo "$PARAMS" | jq -r '.params.media_min_trust_level // "0"')
BOND_MIN=$(echo "$PARAMS" | jq -r '.params.media_author_bond_min // "0"')
SCAN_FEE=$(echo "$PARAMS" | jq -r '.params.media_scan_fee // "0"')
echo "  media_min_trust_level=$MIN_TRUST media_author_bond_min=$BOND_MIN media_scan_fee=$SCAN_FEE"

# ========================================================================
# TEST 1: an inline data URI is labelled INLINE_DATA and withheld
# ========================================================================
echo "--- TEST 1: Inline data URI is labelled and withheld ---"
INLINE_BODY='<p>aurora</p><img src="data:image/png;base64,iVBORw0KGgo=">'
TX_RES=$(create_post alice "Media Aurora" "$INLINE_BODY" html)
MEDIA_POST_ID=""
if submit_tx_and_wait "$TX_RES" && [ "$(tx_code "$TX_RESULT")" == "0" ]; then
    MEDIA_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
fi
if [ -z "$MEDIA_POST_ID" ]; then
    fail "create media post: $(raw_log "$TX_RESULT")"
else
    SHOW=$($BINARY query blog show-post "$MEDIA_POST_ID" --output json 2>&1)
    FLAGS=$(echo "$SHOW" | jq -r '.post.media_flags // "0"')
    BODY=$(echo "$SHOW" | jq -r '.post.body // ""')
    VERSION=$(echo "$SHOW" | jq -r '.post.media_rules_version // "0"')
    HASH=$(echo "$SHOW" | jq -r '.post.body_hash // ""')
    if [ "$FLAGS" == "1" ] && [ -z "$BODY" ] && [ "$VERSION" == "1" ] && [ "$HASH" == "$(b64_sha256 "$INLINE_BODY")" ]; then
        pass "show-post: media_flags=1, body withheld, rules v1, body_hash matches"
    else
        fail "show-post: flags=$FLAGS version=$VERSION body='$BODY' hash=$HASH"
    fi

    LISTED=$($BINARY query blog list-post --output json 2>&1 | jq -r --arg id "$MEDIA_POST_ID" '.post[] | select((.id // "0") == $id) | .body // ""')
    if [ -z "$LISTED" ]; then
        pass "list-post withholds the body"
    else
        fail "list-post returned the body"
    fi

    FULL=$($BINARY query blog post-body "$MEDIA_POST_ID" --output json 2>&1)
    if [ "$(echo "$FULL" | jq -r '.body // ""')" == "$INLINE_BODY" ] && [ "$(echo "$FULL" | jq -r '.body_hash // ""')" == "$HASH" ]; then
        pass "post-body returns the stored body and its hash"
    else
        fail "post-body: $(echo "$FULL" | head -c 300)"
    fi
fi

# ========================================================================
# TEST 2: prose mentioning "metadata:" stays plain and visible
# ========================================================================
echo "--- TEST 2: Plain text is not labelled ---"
PLAIN_BODY="metadata: nothing inline here, Data: 5 records"
TX_RES=$(create_post alice "Plain Zenith" "$PLAIN_BODY" text)
PLAIN_ID=""
if submit_tx_and_wait "$TX_RES" && [ "$(tx_code "$TX_RESULT")" == "0" ]; then
    PLAIN_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
fi
if [ -n "$PLAIN_ID" ]; then
    SHOW=$($BINARY query blog show-post "$PLAIN_ID" --output json 2>&1)
    if [ "$(echo "$SHOW" | jq -r '.post.media_flags // "0"')" == "0" ] && [ "$(echo "$SHOW" | jq -r '.post.body // ""')" == "$PLAIN_BODY" ]; then
        pass "plain post unlabelled and shown in full"
    else
        fail "plain post: $(echo "$SHOW" | jq -c '.post | {media_flags, body}')"
    fi
else
    fail "create plain post: $(raw_log "$TX_RESULT")"
fi

# ========================================================================
# TEST 3: editing media into plain text clears the label
# ========================================================================
echo "--- TEST 3: Editing to plain text clears the label ---"
if [ -n "$MEDIA_POST_ID" ]; then
    TX_RES=$($BINARY tx blog update-post "Media Aurora" "now just words" "$MEDIA_POST_ID" --content-type text \
        --from alice --chain-id $CHAIN_ID --keyring-backend test --fees 50000${BOND_DENOM} --gas 400000 -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && [ "$(tx_code "$TX_RESULT")" == "0" ]; then
        SHOW=$($BINARY query blog show-post "$MEDIA_POST_ID" --output json 2>&1)
        if [ "$(echo "$SHOW" | jq -r '.post.media_flags // "0"')" == "0" ] && [ "$(echo "$SHOW" | jq -r '.post.body // ""')" == "now just words" ]; then
            pass "label cleared on edit"
        else
            fail "after edit: $(echo "$SHOW" | jq -c '.post | {media_flags, body}')"
        fi
    else
        fail "update-post: $(raw_log "$TX_RESULT")"
    fi
else
    warn "skipped (no media post)"
fi

# ========================================================================
# TEST 4: media rules — low trust needs an author bond
# ========================================================================
echo "--- TEST 4: Media from a low-trust member needs a bond ---"
TRUST_NAME=$($BINARY query rep get-member "$BLOGGER2_ADDR" --output json 2>/dev/null | jq -r '.member.trust_level // "TRUST_LEVEL_NEW"')
case "$TRUST_NAME" in
    TRUST_LEVEL_NEW) TRUST=0 ;; TRUST_LEVEL_PROVISIONAL) TRUST=1 ;; TRUST_LEVEL_ESTABLISHED) TRUST=2 ;;
    TRUST_LEVEL_TRUSTED) TRUST=3 ;; TRUST_LEVEL_CORE) TRUST=4 ;; *) TRUST=0 ;;
esac
if [ "$TRUST" -ge "$MIN_TRUST" ]; then
    warn "blogger2 is $TRUST_NAME (>= media_min_trust_level $MIN_TRUST); gate not exercisable here"
else
    TX_RES=$(create_post blogger2 "Gzip Phoenix" "H4sIAAAAAAAA" gzip)
    submit_tx_and_wait "$TX_RES"
    if [ "$(tx_code "$TX_RESULT")" != "0" ] && raw_log "$TX_RESULT" | grep -q "media not permitted"; then
        pass "unbonded media from $TRUST_NAME rejected (media not permitted)"
    else
        fail "unbonded media from $TRUST_NAME: code=$(tx_code "$TX_RESULT") log=$(raw_log "$TX_RESULT")"
    fi

    if [ "$BOND_MIN" == "0" ]; then
        warn "media_author_bond_min is 0: bond path disabled"
    else
        DREAM=$($BINARY query bank balance "$BLOGGER2_ADDR" "$DREAM_DENOM" --output json 2>/dev/null | jq -r '.balance.amount // "0"')
        if [ "$DREAM" -lt "$BOND_MIN" ] 2>/dev/null; then
            warn "blogger2 holds $DREAM $DREAM_DENOM < bond $BOND_MIN; bond path not exercisable"
        else
            TX_RES=$(create_post blogger2 "Gzip Phoenix Bonded" "H4sIAAAAAAAA" gzip --author-bond "$BOND_MIN")
            if submit_tx_and_wait "$TX_RES" && [ "$(tx_code "$TX_RESULT")" == "0" ]; then
                ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
                FLAGS=$($BINARY query blog show-post "$ID" --output json 2>&1 | jq -r '.post.media_flags // "0"')
                if [ "$FLAGS" == "2" ]; then
                    pass "bonded gzip media accepted and labelled COMPRESSED"
                else
                    fail "bonded gzip media flags=$FLAGS"
                fi
            else
                fail "bonded media rejected: $(raw_log "$TX_RESULT")"
            fi
        fi
    fi
fi

# ========================================================================
# TEST 5: body query for a missing post
# ========================================================================
echo "--- TEST 5: post-body of an unknown id fails ---"
if $BINARY query blog post-body 999999999 --output json > /dev/null 2>&1; then
    fail "post-body returned for an unknown id"
else
    pass "post-body rejects an unknown id"
fi

echo ""
echo "Media tests: $PASS_COUNT passed, $FAIL_COUNT failed"
[ "$FAIL_COUNT" -eq 0 ]
