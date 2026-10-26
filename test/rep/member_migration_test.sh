#!/bin/bash
# Member key migration e2e: the three authorization paths (key, recovery,
# council), the alias contract, the tombstone ante backstop, the SPARK redirect
# and the ZK key change discipline. docs/x-rep-spec.md, "Member key migration".
#
# Test-build timings (x/rep/types/params_vals_testparams.go): key delay 20s,
# recovery delay 10s, council delay 40s, accept window 120s, cooldown 30s,
# reveal min age 2 blocks, ZK activation 2 epochs (epoch_blocks 10 in config.yml).

echo "--- TESTING: MEMBER KEY MIGRATION (KEY, RECOVERY, COUNCIL PATHS) ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
REP_ENV="$SCRIPT_DIR/.test_env"
if [ -f "$REP_ENV" ]; then
    source "$REP_ENV"
else
    echo "Test environment not found at $REP_ENV"
    echo "   Run: bash test/rep/setup_test_accounts.sh"
    exit 1
fi

PASS=0
FAIL=0
pass() { echo "  [ OK ] $1"; PASS=$((PASS + 1)); }
fail() { echo "  [FAIL] $1"; FAIL=$((FAIL + 1)); }
TXFLAGS=(--chain-id "$CHAIN_ID" --keyring-backend test -y --output json)
DEFAULT_FEE="5000${BOND_DENOM}"
# FEE overrides the default fee for one run_tx call (set FEE=... before it).
FEE=""

wait_for_tx() {
    local hash=$1 attempt=0
    while [ $attempt -lt 30 ]; do
        local res
        res=$($BINARY q tx "$hash" --output json 2>&1)
        if echo "$res" | jq -e '.code' > /dev/null 2>&1; then echo "$res"; return 0; fi
        attempt=$((attempt + 1)); sleep 1
    done
    echo '{"code":-1,"raw_log":"tx not found"}'
    return 1
}

# run_tx <description> <expected: ok|fail> <tx args...>
# Broadcasts, waits for delivery and checks the code. Prints the delivered
# tx JSON on LAST_TX for event extraction. A tx refused at CheckTx (ante)
# never gets a hash; that counts as a failure code too.
LAST_TX=""
run_tx() {
    local desc=$1 expect=$2; shift 2
    local res hash code
    local fee="${FEE:-$DEFAULT_FEE}"; FEE=""
    res=$($BINARY tx "$@" "${TXFLAGS[@]}" --fees "$fee" 2>&1)
    hash=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
    code=$(echo "$res" | jq -r '.code // 1' 2>/dev/null)
    if [ -n "$hash" ] && [ "$code" = "0" ]; then
        LAST_TX=$(wait_for_tx "$hash")
        code=$(echo "$LAST_TX" | jq -r '.code')
    else
        LAST_TX="$res"
    fi
    if [ "$expect" = "ok" ]; then
        if [ "$code" = "0" ]; then pass "$desc"; return 0; fi
        local detail
        detail=$(echo "$LAST_TX" | jq -r '.raw_log // .' 2>/dev/null | head -c 300)
        [ -z "$detail" ] && detail=$(echo "$LAST_TX" | head -c 300)
        fail "$desc (code $code): $detail"
        return 1
    fi
    if [ "$code" != "0" ]; then pass "$desc (refused as expected)"; return 0; fi
    fail "$desc: expected refusal but tx succeeded"
    return 1
}

event_attr() { echo "$LAST_TX" | jq -r ".events[] | select(.type==\"$1\") | .attributes[] | select(.key==\"$2\") | .value" | head -1; }

migration_status() { $BINARY query rep member-migration "$1" --output json 2>/dev/null | jq -r '.migration.status // empty'; }

# wait_status <id> <status> <seconds>
wait_status() {
    local id=$1 want=$2 budget=$3 waited=0 st
    while [ $waited -lt "$budget" ]; do
        st=$(migration_status "$id")
        [ "$st" = "$want" ] && return 0
        sleep 2; waited=$((waited + 2))
    done
    echo "      (migration $id is $st after ${budget}s, wanted $want)"
    return 1
}

spark_balance() { $BINARY query bank balance "$1" "$BOND_DENOM" --output json 2>/dev/null | jq -r '.balance.amount // "0"'; }

sha256hex() { printf '%s' "$1" | sha256sum | cut -d' ' -f1; }
tohex() { printf '%s' "$1" | xxd -p | tr -d '\n'; }

COMMIT_DOMAIN="sparkdream/recovery/commit/v1"
SEAL_DOMAIN="sparkdream/recovery/seal/v1"

# --- 0. Accounts ---------------------------------------------------------
echo ""
echo "Step 0: fresh keys and funding"
# Fresh keys per run (suffix = epoch seconds), so a rerun against a chain that
# already saw an earlier attempt never collides with its tombstones.
RUN_TAG=$(date +%s)
KA="migrant_a_$RUN_TAG"; KB="migrant_b_$RUN_TAG"; KC="migrant_c_$RUN_TAG"; KD="migrant_d_$RUN_TAG"; KX="migrant_x_$RUN_TAG"; KR="revealer_$RUN_TAG"
for k in $KA $KB $KC $KD $KX $KR; do
    $BINARY keys add $k --keyring-backend test > /dev/null 2>&1
done
A=$($BINARY keys show $KA -a --keyring-backend test)
B=$($BINARY keys show $KB -a --keyring-backend test)
C=$($BINARY keys show $KC -a --keyring-backend test)
D=$($BINARY keys show $KD -a --keyring-backend test)
X=$($BINARY keys show $KX -a --keyring-backend test)
REVEALER=$($BINARY keys show $KR -a --keyring-backend test)
echo "  A=$A"; echo "  B=$B"; echo "  C=$C"; echo "  D=$D"; echo "  X=$X"; echo "  revealer=$REVEALER"

# One multi-send: sequential sends from alice would race her account sequence.
run_tx "fund the six accounts" ok bank multi-send "$ALICE_ADDR" $A $B $C $D $X $REVEALER "30000000${BOND_DENOM}" --gas 400000 --from alice
[ "$(spark_balance $A)" = "30000000" ] && pass "accounts funded" || fail "funding A (balance $(spark_balance $A))"

# --- 1. Onboarding with a recovery commitment ----------------------------
echo ""
echo "Step 1: alice invites migrant_a, who joins with a recovery commitment"
PREIMAGE="e2e-recovery-code-one"
COMMITMENT=$(sha256hex "${COMMIT_DOMAIN}${PREIMAGE}")
# Pick an inviter that still has an invitation credit and the DREAM to stake:
# this test runs last in the suite, after the others spent alice's credits.
INVITER=""; INVITER_ADDR=""
for cand in alice:$ALICE_ADDR sentinel1:$SENTINEL1_ADDR sentinel2:$SENTINEL2_ADDR moderator:$MODERATOR_ADDR poster1:$POSTER1_ADDR; do
    name=${cand%%:*}; addr=${cand#*:}
    [ -z "$addr" ] && continue
    m=$($BINARY query rep get-member "$addr" --output json 2>/dev/null)
    credits=$(echo "$m" | jq -r '.member.invitation_credits // "0"')
    dream=$(echo "$m" | jq -r '.member.dream_balance // "0"')
    stake=$($BINARY query rep required-invitation-stake "$addr" --output json 2>/dev/null | jq -r '.required_stake // "200000000"')
    [ "$stake" -lt 200000000 ] && stake=200000000
    if [ "$credits" != "0" ] && [ "$dream" -ge "$stake" ]; then INVITER=$name; INVITER_ADDR=$addr; REQUIRED_STAKE=$stake; break; fi
done
if [ -z "$INVITER" ]; then fail "no inviter with credits and DREAM left"; else echo "  inviter: $INVITER ($INVITER_ADDR), stake $REQUIRED_STAKE"; fi
run_tx "invite migrant_a" ok rep invite-member --gas 400000 "$A" "$REQUIRED_STAKE" --from "$INVITER"
INV_ID=$(event_attr "create_invitation" "invitation_id")
echo "  invitation id: $INV_ID"
run_tx "accept invitation with recovery_commitment" ok rep accept-invitation --gas 400000 "$INV_ID" CC0-1.0 --recovery-commitment "$COMMITMENT" --from $KA
N=$($BINARY query rep recovery-commitments "$A" --output json 2>/dev/null | jq -r '.set.commitments | length')
EFF=$($BINARY query rep recovery-commitments "$A" --output json 2>/dev/null | jq -r '.set.commitments[0].effective_at // "0"')
if [ "$N" = "1" ] && [ "$EFF" = "0" ]; then pass "onboarding commitment registered and immediately active"; else fail "commitments: n=$N effective_at=$EFF"; fi

ZK1="01$(printf '0%.0s' $(seq 1 62))"
run_tx "register zk public key (first registration is immediate)" ok rep register-zk-public-key --gas 300000 "$ZK1" --from $KA
ZK_NOW=$($BINARY query rep get-member "$A" --output json 2>/dev/null | jq -r '.member.zk_public_key // empty')
[ -n "$ZK_NOW" ] && pass "zk key active on A" || fail "zk key missing on A"

# --- 2. Key path ----------------------------------------------------------
echo ""
echo "Step 2: key path A -> B"
REP_MODULE_ADDR=$($BINARY query auth module-account rep --output json 2>/dev/null | jq -r '[.. | .address? | select(type=="string")] | first // empty')
if [ -n "$REP_MODULE_ADDR" ]; then
    run_tx "request migration to a reserved (module) address is refused" fail rep request-migration --gas 300000 "$REP_MODULE_ADDR" --from $KA
else
    fail "could not resolve the rep module account address"
fi
run_tx "request migration A -> B" ok rep request-migration --gas 300000 "$B" --from $KA
MIG1=$(event_attr "member_migration_requested" "migration_id")
echo "  migration id: $MIG1"
[ "$(migration_status $MIG1)" = "MIGRATION_STATUS_REQUESTED" ] && pass "status REQUESTED" || fail "status $(migration_status $MIG1)"
PEND=$($BINARY query rep pending-migration "$A" --output json 2>/dev/null | jq -r '.migration.id')
[ "$PEND" = "$MIG1" ] && pass "pending-migration query" || fail "pending-migration returned $PEND"
PEND_B=$($BINARY query rep pending-migration-by-target "$B" --output json 2>/dev/null | jq -r '.migration.id')
[ "$PEND_B" = "$MIG1" ] && pass "pending-migration-by-target query" || fail "pending-migration-by-target returned $PEND_B"
run_tx "a second request while one is pending is refused" fail rep request-migration --gas 300000 "$X" --from $KA
run_tx "only the new address may accept" fail rep accept-migration --gas 300000 "$MIG1" --from $KC
run_tx "B accepts" ok rep accept-migration --gas 300000 "$MIG1" --from $KB
[ "$(migration_status $MIG1)" = "MIGRATION_STATUS_ACCEPTED" ] && pass "status ACCEPTED" || fail "status $(migration_status $MIG1)"

A_BAL_BEFORE=$(spark_balance $A)
B_BAL_BEFORE=$(spark_balance $B)
echo "  waiting for the 20s key delay..."
wait_status "$MIG1" "MIGRATION_STATUS_FINALIZED" 60 && pass "finalized after the delay" || fail "not finalized"

OLDADDR=$($BINARY query rep get-member "$A" --output json 2>/dev/null | jq -r '.member.address')
[ "$OLDADDR" = "$B" ] && pass "get-member(A) resolves to B's record" || fail "get-member(A) address $OLDADDR"
NEWADDR=$($BINARY query rep get-member "$B" --output json 2>/dev/null | jq -r '.member.address')
[ "$NEWADDR" = "$B" ] && pass "Member[B] carries the record" || fail "Member[B] address $NEWADDR"
CAN=$($BINARY query rep canonical "$A" --output json 2>/dev/null)
[ "$(echo "$CAN" | jq -r '.canonical')" = "$B" ] && [ "$(echo "$CAN" | jq -r '.tombstoned')" = "true" ] && pass "canonical(A) = B, tombstoned" || fail "canonical query: $CAN"
ALIASES=$($BINARY query rep aliases "$B" --output json 2>/dev/null | jq -r '.addresses | join(",")')
[ "$ALIASES" = "$B,$A" ] && pass "aliases(B) = [B, A]" || fail "aliases: $ALIASES"
A_BAL_AFTER=$(spark_balance $A)
B_BAL_AFTER=$(spark_balance $B)
if [ "$A_BAL_AFTER" = "0" ] && [ "$B_BAL_AFTER" -gt "$B_BAL_BEFORE" ]; then pass "A's SPARK swept to B ($A_BAL_BEFORE -> B)"; else fail "sweep: A=$A_BAL_AFTER B=$B_BAL_BEFORE->$B_BAL_AFTER"; fi
ZK_B=$($BINARY query rep get-member "$B" --output json 2>/dev/null | jq -r '.member.zk_public_key // empty')
[ -n "$ZK_B" ] && pass "key path keeps the zk key" || fail "zk key lost on key path"

# Tombstone backstop: a KEY tombstone may still MsgSend, nothing else.
$BINARY tx bank send "$ALICE_ADDR" "$A" "2000000${BOND_DENOM}" --from alice "${TXFLAGS[@]}" --fees "$DEFAULT_FEE" > /dev/null 2>&1; sleep 4
B_BEFORE_REDIRECT=$(spark_balance $B)
[ "$(spark_balance $A)" = "0" ] && pass "send to tombstone A redirected (A still 0)" || fail "A received $(spark_balance $A) despite redirect"
$BINARY tx bank send "$ALICE_ADDR" "$B" "2000000${BOND_DENOM}" --from alice "${TXFLAGS[@]}" --fees "$DEFAULT_FEE" > /dev/null 2>&1; sleep 4
run_tx "tombstone A cannot create content" fail rep create-interim --gas 300000 other 0 "tombstone" standard 999999999 --from $KA
run_tx "tombstone A cannot request another migration" fail rep request-migration --gas 300000 "$X" --from $KA
run_tx "B cannot migrate again inside the cooldown" fail rep request-migration --gas 300000 "$X" --from $KB

# --- 3. Recovery path -----------------------------------------------------
echo ""
echo "Step 3: recovery path B -> C (thief holds the key)"
echo "  waiting out the 30s cooldown so the thief can start a key migration..."
sleep 32
run_tx "thief (holding B's key) requests a key migration to X" ok rep request-migration --gas 300000 "$X" --from $KB
THIEF_MIG=$(event_attr "member_migration_requested" "migration_id")
run_tx "thief cannot swap in a commitment that is active now (delayed 30s)" ok rep set-recovery-commitment --gas 300000 "$(sha256hex "${COMMIT_DOMAIN}thief-code")" "" --from $KB
SEAL=$(sha256hex "${SEAL_DOMAIN}${PREIMAGE}${B}${C}")
run_tx "revealer seals (preimage, B, C) with a 1 SPARK deposit" ok rep commit-recovery --gas 300000 "$SEAL" --from $KR
REV_BAL_SEALED=$(spark_balance $REVEALER)
# The seal's minimum age (2 blocks in the test build) is already exceeded by
# the time the commit tx is confirmed, so the too-young refusal is left to the
# keeper unit tests; here we only wait for the age and reveal.
sleep 4
run_tx "reveal against a different target than sealed is refused" fail rep reveal-recovery --gas 400000 "$B" "$X" "$(tohex "$PREIMAGE")" --from $KR
run_tx "reveal B -> C by any account" ok rep reveal-recovery --gas 400000 "$B" "$C" "$(tohex "$PREIMAGE")" --from $KR
MIG2=$(event_attr "recovery_revealed" "migration_id")
echo "  recovery migration id: $MIG2"
[ "$(migration_status $THIEF_MIG)" = "MIGRATION_STATUS_CANCELLED" ] && pass "thief's key migration cancelled by precedence" || fail "thief migration $(migration_status $THIEF_MIG)"
[ "$(spark_balance $REVEALER)" -gt "$REV_BAL_SEALED" ] && pass "seal deposit refunded to the revealer" || fail "deposit not refunded"
N=$($BINARY query rep recovery-commitments "$B" --output json 2>/dev/null | jq -r '.set.commitments | length')
[ "$N" = "0" ] && pass "revealed commitment consumed, thief's pending one voided" || fail "commitments left: $N"
run_tx "thief cannot cancel a recovery migration" fail rep cancel-migration --gas 300000 "$MIG2" --from $KB
run_tx "C accepts" ok rep accept-migration --gas 300000 "$MIG2" --from $KC
wait_status "$MIG2" "MIGRATION_STATUS_FINALIZED" 40 && pass "recovery finalized after 10s" || fail "recovery not finalized"
ZK_C=$($BINARY query rep get-member "$C" --output json 2>/dev/null | jq -r '.member.zk_public_key // empty')
[ -z "$ZK_C" ] && pass "recovery path cleared the presumed-compromised zk key" || fail "zk key survived recovery"
EXEMPT=$($BINARY query rep get-member "$C" --output json 2>/dev/null | jq -r '.member.zk_key_cooldown_exempt')
[ "$EXEMPT" = "true" ] && pass "next zk registration is cooldown-exempt" || fail "zk_key_cooldown_exempt=$EXEMPT"
[ "$(echo "$($BINARY query rep canonical "$A" --output json 2>/dev/null)" | jq -r '.canonical')" = "$C" ] && pass "path compression: canonical(A) = C" || fail "canonical(A) not compressed to C"
$BINARY tx bank send "$ALICE_ADDR" "$B" "1000000${BOND_DENOM}" --from alice "${TXFLAGS[@]}" --fees "$DEFAULT_FEE" > /dev/null 2>&1; sleep 4
run_tx "RECOVERY tombstone B cannot MsgSend" fail bank send "$B" "$ALICE_ADDR" "100${BOND_DENOM}" --from $KB
run_tx "only the canonical address sweeps a tombstone" fail rep sweep-tombstone --gas 300000 "$B" --from $KX
run_tx "C sweeps tombstone B" ok rep sweep-tombstone --gas 300000 "$B" --from $KC

# --- 4. Council path -------------------------------------------------------
echo ""
echo "Step 4: council path C -> D (HR committee), objection, jury"
HR_POLICY=$($BINARY query commons get-group "Commons Governance Committee" --output json 2>/dev/null | jq -r '.group.policy_address')
PROPOSAL_FEE=$($BINARY query commons params --output json 2>/dev/null | jq -r '.params.proposal_fee')
PROP_DIR=$(mktemp -d)
cat > "$PROP_DIR/force.json" <<EOF
{
  "policy_address": "$HR_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.rep.v1.MsgForceMigration",
      "authority": "$HR_POLICY",
      "member": "$C",
      "new_address": "$D",
      "evidence_uri": "ipfs://e2e-evidence"
    }
  ],
  "metadata": "e2e council migration"
}
EOF
run_tx "a plain member cannot force a migration" fail rep force-migration --gas 300000 "$C" "$D" "ipfs://x" --from $KX
FEE="$PROPOSAL_FEE" run_tx "HR committee proposal: force migration C -> D" ok commons submit-proposal "$PROP_DIR/force.json" --from alice
PROP_ID=$(event_attr "submit_proposal" "proposal_id")
echo "  proposal id: $PROP_ID"
run_tx "alice votes yes" ok commons vote-proposal "$PROP_ID" yes --from alice
sleep 3
FEE="5000000${BOND_DENOM}" run_tx "execute the proposal" ok commons execute-proposal "$PROP_ID" --gas 2000000 --from alice
MIG3=$($BINARY query rep pending-migration "$C" --output json 2>/dev/null | jq -r '.migration.id')
echo "  council migration id: $MIG3"
[ "$($BINARY query rep pending-migration "$C" --output json 2>/dev/null | jq -r '.migration.path')" = "MIGRATION_PATH_COUNCIL" ] && pass "council migration pending" || fail "no council migration for C"
run_tx "objection before accept is refused (not yet in its delay)" fail rep object-migration --gas 400000 "$MIG3" "not me" --from $KC
run_tx "D accepts" ok rep accept-migration --gas 300000 "$MIG3" --from $KD
C_BAL=$(spark_balance $C)
run_tx "C objects (10 SPARK bond)" ok rep object-migration --gas 600000 "$MIG3" "I never lost my key" --from $KC
REVIEW_ID=$(event_attr "member_migration_objected" "jury_review_id")
[ "$(migration_status $MIG3)" = "MIGRATION_STATUS_OBJECTED" ] && pass "status OBJECTED, jury review $REVIEW_ID" || fail "status $(migration_status $MIG3)"
[ "$(spark_balance $C)" -lt "$C_BAL" ] && pass "objection bond escrowed" || fail "bond not taken"

# The jury is drawn from every eligible member, so map seats back to local
# key names through the keyring rather than a fixed juror1/2/3 list.
declare -A JUROR_NAME_BY_ADDR
while IFS=$'\t' read -r kname kaddr; do
    [ -n "$kaddr" ] && JUROR_NAME_BY_ADDR["$kaddr"]="$kname"
done < <($BINARY keys list --keyring-backend test --output json 2>/dev/null | jq -r '.[] | [.name, .address] | @tsv')
JURY_ADDRS=$($BINARY query rep get-jury-review "$REVIEW_ID" --output json 2>/dev/null | jq -r '.jury_review.jurors[]')
D_BAL_BEFORE=$(spark_balance $D)
VOTED=0
for JADDR in $JURY_ADDRS; do
    JNAME="${JUROR_NAME_BY_ADDR[$JADDR]:-}"
    [ -z "$JNAME" ] && { echo "  juror $JADDR has no local key; skipping"; continue; }
    # The supermajority vote tallies immediately; later seats are refused.
    VERDICT_NOW=$($BINARY query rep get-jury-review "$REVIEW_ID" --output json 2>/dev/null | jq -r '.jury_review.verdict // "VERDICT_PENDING"')
    if [ "$VERDICT_NOW" != "VERDICT_PENDING" ] && [ "$VERDICT_NOW" != "null" ]; then echo "  review already resolved ($VERDICT_NOW); no more votes needed"; break; fi
    # The deciding vote also tallies: it settles the objection bond, re-queues
    # the migration and rewards every juror, so it needs more gas than a vote.
    run_tx "$JNAME votes reject-challenge (objection fails)" ok rep submit-juror-vote --gas 1200000 "$REVIEW_ID" reject-challenge 0.9 "evidence convincing" --from "$JNAME" && VOTED=$((VOTED + 1))
done
echo "  votes cast: $VOTED"
if wait_status "$MIG3" "MIGRATION_STATUS_FINALIZED" 60; then
    pass "council migration finalized after the jury rejected the objection"
    [ "$(spark_balance $D)" -gt "$D_BAL_BEFORE" ] && pass "objection bond paid to D" || fail "bond not paid to D"
else
    fail "council migration did not finalize (jury votes: $VOTED, status $(migration_status $MIG3))"
fi
[ "$(echo "$($BINARY query rep canonical "$A" --output json 2>/dev/null)" | jq -r '.canonical')" = "$D" ] && pass "canonical(A) = D after three hops" || fail "canonical(A) != D"
NREC=$($BINARY query rep aliases "$D" --output json 2>/dev/null | jq -r '.recent | length')
[ "$NREC" = "4" ] && pass "aliases(D).recent lists D and three predecessors" || fail "recent aliases: $NREC"

# --- 5. ZK key change discipline -----------------------------------------
echo ""
echo "Step 5: zk key change discipline on D"
ZK2="02$(printf '0%.0s' $(seq 1 62))"
ZK3="03$(printf '0%.0s' $(seq 1 62))"
run_tx "D registers after the clear: parked for activation, cooldown exempt" ok rep register-zk-public-key --gas 300000 "$ZK2" --from $KD
PENDING=$($BINARY query rep get-member "$D" --output json 2>/dev/null | jq -r '.member.pending_zk_public_key // empty')
[ -n "$PENDING" ] && pass "key parked as pending" || fail "key not pending"
run_tx "a second change while one is pending is refused" fail rep register-zk-public-key --gas 300000 "$ZK3" --from $KD
echo "  waiting for the activation epoch (2 epochs x 10 blocks)..."
ACTIVATED=""
for i in $(seq 1 60); do
    ACTIVATED=$($BINARY query rep get-member "$D" --output json 2>/dev/null | jq -r '.member.zk_public_key // empty')
    [ -n "$ACTIVATED" ] && break
    sleep 2
done
[ -n "$ACTIVATED" ] && pass "pending key activated by the EndBlocker" || fail "key never activated"
run_tx "a change in the same season is refused (once per season)" fail rep register-zk-public-key --gas 300000 "$ZK3" --from $KD

rm -f "$PROP_DIR/force.json"; rmdir "$PROP_DIR" 2>/dev/null

echo ""
echo "Member migration: $PASS passed, $FAIL failed"
[ $FAIL -eq 0 ] && exit 0 || exit 1
