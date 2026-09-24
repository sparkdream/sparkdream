#!/bin/bash
# Drive each P2 edit class through determinism_check.sh --phase edits,
# baselining on the hash immediately before each edit.
#
# Env:
#   SP  scratch directory holding: token (a read+write OAuth token for the
#       posting account), img.png (any small image), apcanon (a built
#       apcanon CLI). Required.
#   H   the harness directory, test/federation/mastodon (default: resolved
#       from this script's location in accepted/<run>/edits/).
set -uo pipefail
SP="${SP:?set SP to a scratch dir holding token, img.png and apcanon}"
H="${H:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}"
A="Authorization: Bearer $(cat $SP/token)"; API=http://localhost:3000/api
AP="$SP/apcanon"; export APCANON="$AP" ALLOW_PRIVATE=1
export OUT_DIR="$H/runs/$(date -u +%Y%m%dT%H%M%SZ)-edits"; mkdir -p "$OUT_DIR"
mk()  { curl -sf -H "$A" "$API/v1/statuses" "$@" | jq -r '.id + " " + .uri'; }
ed()  { curl -sf -X PUT -H "$A" "$API/v1/statuses/$1" "${@:2}" -o /dev/null || echo "EDIT FAILED"; }
upl() { curl -sf -H "$A" -F "file=@$SP/img.png" -F "description=$1" $API/v2/media | jq -r .id; }

# Mastodon caches the AS2 body for 3 min, keyed without updated_at, so an
# edit is invisible over AS2 until the entry written before it expires.
# Wait until AS2 updated == the API's edited_at (second precision).
await_as2() {
  local want; want="$(curl -sf -H "$A" "$API/v1/statuses/$1" | jq -r '.edited_at[0:19]')"
  for _ in $(seq 1 80); do
    [[ "$(curl -s -H 'Accept: application/activity+json' "$2" | jq -r '.updated // "" | .[0:19]')" == "$want" ]] && return 0
    sleep 5
  done
  return 1
}

step() { # step <label> <id> <uri> -- edit args...
  local label=$1 id=$2 uri=$3; shift 4
  local before; before="$($AP canon --allow-private "$uri")"
  printf '%s %s\n' "$($AP hash --allow-private "$uri")" "$uri" > "$SP/step_baseline.txt"
  ed "$id" "$@"; await_as2 "$id" "$uri" || echo "== $label: AS2 never caught up with the edit" | tee -a "$OUT_DIR/run.log"
  local after; after="$($AP canon --allow-private "$uri")"
  local moved; moved="$(jq -rn --argjson a "$before" --argjson b "$after" '[$a|keys[] as $k | select($a[$k] != $b[$k]) | $k] | join(",")')"
  echo "== $label: fields moved = [$moved]" | tee -a "$OUT_DIR/run.log"
  BASELINE_FILE="$SP/step_baseline.txt" "$H/determinism_check.sh" --phase edits "$uri" | tail -2
}

read id uri < <(mk -F 'status=edit classes target, original body' -F visibility=public)
echo "target $uri" | tee -a "$OUT_DIR/run.log"
step body     $id $uri -- -F 'status=edit classes target, EDITED body'
step cw-add   $id $uri -- -F 'status=edit classes target, EDITED body' -F 'spoiler_text=aurora cw'
step cw-rm    $id $uri -- -F 'status=edit classes target, EDITED body' -F 'spoiler_text='
m1=$(upl 'phoenix square'); sleep 3
step media-add $id $uri -- -F 'status=edit classes target, EDITED body' -F "media_ids[]=$m1"
step media-alt $id $uri -- -F 'status=edit classes target, EDITED body' -F "media_ids[]=$m1" -F "media_attributes[][id]=$m1" -F 'media_attributes[][description]=zenith square'
step media-rm  $id $uri -- -F 'status=edit classes target, EDITED body'

read pid puri < <(mk -F 'status=poll edit target' -F 'poll[options][]=phoenix' -F 'poll[options][]=aurora' -F 'poll[expires_in]=86400' -F visibility=public)
echo "poll target $puri" | tee -a "$OUT_DIR/run.log"
step poll-options $pid $puri -- -F 'status=poll edit target' -F 'poll[options][]=phoenix' -F 'poll[options][]=zenith' -F 'poll[expires_in]=86400'
echo "OUT_DIR=$OUT_DIR"
