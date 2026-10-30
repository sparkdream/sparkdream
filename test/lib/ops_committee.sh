# Commons Operations Committee helper for e2e scripts.
#
# Season transition control and the season calendar (MsgExtendSeason,
# MsgSetNextSeasonInfo, MsgAbortSeasonTransition, MsgSkipTransitionPhase,
# MsgRetrySeasonTransition) are accepted only from a committee or council
# policy address, never from an individual committee member. Scripts that
# used to sign them as `alice` route them through a committee proposal here:
# alice submits, votes (the e2e committee threshold is one vote) and
# executes.
#
# Usage:
#   source "$TEST_DIR/lib/ops_committee.sh"
#   ops_committee_exec '{"@type":"/sparkdream.season.v1.MsgExtendSeason","extension_epochs":"1","reason":"x"}'
#   ops_committee_exec '{"@type":"/sparkdream.rep.v1.MsgResolveMemberReport","member":"...","action":"0","reason":"x"}' creator
#
# The message's signer field (`authority` unless a second argument names
# another, e.g. `creator` or `resolver`) is filled in with the committee
# policy address. x/rep member-report and gov-action-appeal resolutions
# (MsgResolveMemberReport, MsgResolveGovActionAppeal), interim approvals
# (MsgApproveInterim) and ADJUDICATION verdicts (MsgCompleteInterim) are also
# accepted only from the policy address.
#
#   ops_approve_interim <interim_id> [comments] [true|false]
#   ops_complete_adjudication <interim_id> <UPHOLD|REJECT> [notes]
# Uses $BINARY (default sparkdreamd), $CHAIN_ID (default: the chain's own)
# and $BOND_DENOM (default: the staking bond denom). Prints progress to
# stderr and returns 0 only when the proposal reaches EXECUTED. On failure
# OPS_COMMITTEE_ERROR holds a short reason (for a message the handler
# refused, the execute tx's raw_log). OPS_COMMITTEE_PROPOSAL_ID holds the
# proposal id and OPS_COMMITTEE_TX the last execute tx JSON.

OPS_COMMITTEE_NAME="${OPS_COMMITTEE_NAME:-Commons Operations Committee}"

# _ops_wait_tx <bin> <txhash>: echo the delivered tx JSON (empty on timeout).
_ops_wait_tx() {
    local bin=$1 hash=$2 i out
    for i in $(seq 1 30); do
        sleep 1
        out=$("$bin" query tx "$hash" --output json 2>/dev/null) || continue
        if [ -n "$(echo "$out" | jq -r '.code // empty' 2>/dev/null)" ]; then
            echo "$out"
            return 0
        fi
    done
    return 1
}

ops_committee_exec() {
    local msg_json=$1
    local signer_field="${2:-authority}"
    local bin="${BINARY:-sparkdreamd}"
    local chain_id="${CHAIN_ID:-}"
    local denom="${BOND_DENOM:-}"
    OPS_COMMITTEE_ERROR=""
    OPS_COMMITTEE_PROPOSAL_ID=""
    OPS_COMMITTEE_TX=""

    [ -z "$chain_id" ] && chain_id=$("$bin" status 2>/dev/null | jq -r '.node_info.network // empty')
    [ -z "$chain_id" ] && chain_id="sparkdream"
    [ -z "$denom" ] && denom=$("$bin" query staking params --output json 2>/dev/null | jq -r '.params.bond_denom // empty')
    [ -z "$denom" ] && denom="uspark"

    local policy
    policy=$("$bin" query commons get-group "$OPS_COMMITTEE_NAME" --output json 2>/dev/null | jq -r '.group.policy_address // empty')
    if [ -z "$policy" ]; then
        OPS_COMMITTEE_ERROR="committee '$OPS_COMMITTEE_NAME' not found"
        return 1
    fi

    local file
    file=$(mktemp "${TMPDIR:-/tmp}/ops_committee_XXXXXX.json")
    jq -n --arg policy "$policy" --arg field "$signer_field" --argjson msg "$msg_json" '{
        policy_address: $policy,
        metadata: "e2e: Operations Committee action",
        messages: [($msg + {($field): $policy})]
    }' > "$file"

    local res hash tx prop_id
    res=$("$bin" tx commons submit-proposal "$file" --from alice -y --chain-id "$chain_id" \
        --keyring-backend test --gas 600000 --fees "5000000${denom}" --output json 2>&1)
    rm -f "$file"
    hash=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$hash" ]; then
        OPS_COMMITTEE_ERROR="submit broadcast failed: $(echo "$res" | head -c 200)"
        return 1
    fi
    tx=$(_ops_wait_tx "$bin" "$hash")
    if [ "$(echo "$tx" | jq -r '.code // 1')" != "0" ]; then
        OPS_COMMITTEE_ERROR="submit failed: $(echo "$tx" | jq -r '.raw_log // "timeout"' | head -c 200)"
        return 1
    fi
    prop_id=$(echo "$tx" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"' | head -1)
    if [ -z "$prop_id" ]; then
        OPS_COMMITTEE_ERROR="no proposal_id in submit tx"
        return 1
    fi
    OPS_COMMITTEE_PROPOSAL_ID=$prop_id

    # The vote that reaches threshold accepts the proposal in the same tx.
    res=$("$bin" tx commons vote-proposal "$prop_id" yes --from alice -y --chain-id "$chain_id" \
        --keyring-backend test --gas 600000 --fees "5000000${denom}" --output json 2>&1)
    hash=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
    [ -n "$hash" ] && _ops_wait_tx "$bin" "$hash" > /dev/null

    local attempt status
    for attempt in 1 2 3; do
        res=$("$bin" tx commons execute-proposal "$prop_id" --from alice -y --chain-id "$chain_id" \
            --keyring-backend test --gas 2000000 --fees "5000000${denom}" --output json 2>&1)
        hash=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
        [ -n "$hash" ] && tx=$(_ops_wait_tx "$bin" "$hash")
        OPS_COMMITTEE_TX=$tx
        status=$("$bin" query commons get-proposal "$prop_id" --output json 2>/dev/null | jq -r '.proposal.status // empty')
        if [ "$status" = "PROPOSAL_STATUS_EXECUTED" ]; then
            return 0
        fi
        sleep 3
    done
    OPS_COMMITTEE_ERROR="proposal $prop_id not executed (status ${status:-unknown}): $(echo "$tx" | jq -r '.raw_log // empty' 2>/dev/null | head -c 400)"
    return 1
}

# ops_approve_interim <interim_id> [comments] [approved=true]: approve (pay)
# or reject an interim through a committee proposal (MsgApproveInterim is
# refused from a single committee member).
ops_approve_interim() {
    local id=$1 comments="${2:-approved by the Operations Committee}" approved="${3:-true}"
    ops_committee_exec "$(jq -nc --arg id "$id" --arg c "$comments" --argjson a "$approved" \
        '{"@type":"/sparkdream.rep.v1.MsgApproveInterim",interim_id:$id,approved:$a,comments:$c}')" creator
}

# ops_complete_adjudication <interim_id> <UPHOLD|REJECT> [notes]: settle an
# ADJUDICATION interim through a committee proposal.
ops_complete_adjudication() {
    local id=$1 decision=$2 notes="${3:-committee verdict}"
    ops_committee_exec "$(jq -nc --arg id "$id" --arg d "ADJUDICATION_DECISION_$decision" --arg n "$notes" \
        '{"@type":"/sparkdream.rep.v1.MsgCompleteInterim",interim_id:$id,completion_notes:$n,decision:$d}')" creator
}
