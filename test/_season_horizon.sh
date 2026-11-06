# Season-horizon helper, sourced by test/run_parallel.sh and test/_auto_snapshot.sh.
#
# The test genesis (config.yml) ends season 1 at block 1001 so the x/season
# suite can exercise a transition in minutes. A season transition archives and
# resets every member's reputation, XP and titles (x-season-spec, Seasonal
# Resets), and since the gamification hooks landed every active member has a
# season profile, so the rollover hits every test account rather than only the
# three genesis profiles it used to touch. The non-season suites run for
# thousands of blocks and assume reputation persists, so they must stay inside
# one season: before their setup starts, ensure_season_horizon extends season 1
# through MsgExtendSeason executed by a Commons Operations Committee proposal
# (alice is the committee's member at genesis; a single member can no longer
# sign it directly). config.yml pins season.max_extension_epochs high enough for one
# extension to cover a whole suite.
#
# The x/season suite must NOT call this: its tests wait for block 1001.
#
# Idempotent: skips when the season already ends far enough away, when the
# season is not ACTIVE, or when the module is season/legacy/multichain. Never
# fails the caller; a failed extension is reported as [WARN] and the suite
# proceeds (the old behaviour).

# Minimum headroom (blocks) a non-season suite must have before its setup.
SEASON_HORIZON_MIN_BLOCKS="${SEASON_HORIZON_MIN_BLOCKS:-200000}"

# ensure_season_horizon <module>
#   Uses $BINARY (default: sparkdreamd on PATH; the parallel suite wrapper
#   injects --home), the chain's own chain-id and bond denom, and the
#   `alice` key from the test keyring.
ensure_season_horizon() {
    local module="${1:-}"
    local bin="${BINARY:-sparkdreamd}"
    case "$module" in
        season|legacy|multichain|"") return 0 ;;
    esac

    local status_json height chain_id
    status_json=$("$bin" status 2>/dev/null) || { echo "  [WARN] season horizon: chain status unavailable; skipping"; return 0; }
    height=$(echo "$status_json" | jq -r '.sync_info.latest_block_height // "0"')
    chain_id=$(echo "$status_json" | jq -r '.node_info.network // empty')
    [ -z "$chain_id" ] && chain_id="${CHAIN_ID:-sparkdream}"

    local season_json end_block season_status
    season_json=$("$bin" query season current-season --output json 2>/dev/null) || { echo "  [WARN] season horizon: current-season query failed; skipping"; return 0; }
    end_block=$(echo "$season_json" | jq -r '.end_block // "0"')
    # proto3 omits a zero enum; ACTIVE is 1 in SeasonStatus.
    season_status=$(echo "$season_json" | jq -r '.status // "0"')

    if [ "$((end_block - height))" -ge "$SEASON_HORIZON_MIN_BLOCKS" ] 2>/dev/null; then
        echo "  [ OK ] season horizon: season ends at block $end_block (height $height); no extension needed"
        return 0
    fi
    # MsgExtendSeason accepts ACTIVE (1) and NOMINATION (5): the nomination
    # window is still season 1, so extending inside it is valid.
    case "$season_status" in
        1|SEASON_STATUS_ACTIVE|5|SEASON_STATUS_NOMINATION) ;;
        *)
            echo "  [WARN] season horizon: season status $season_status is not ACTIVE or NOMINATION; cannot extend"
            return 0
            ;;
    esac

    local max_epochs denom
    max_epochs=$("$bin" query season params --output json 2>/dev/null | jq -r '.params.max_extension_epochs // "0"')
    if [ -z "$max_epochs" ] || [ "$max_epochs" = "0" ] || [ "$max_epochs" = "null" ]; then
        echo "  [WARN] season horizon: max_extension_epochs unreadable; skipping"
        return 0
    fi
    denom=$("$bin" query staking params --output json 2>/dev/null | jq -r '.params.bond_denom // empty')
    [ -z "$denom" ] && denom="${BOND_DENOM:-uspark}"

    echo "  Extending season 1 by $max_epochs epochs so the $module suite stays inside one season (height $height, end $end_block)..."
    # MsgExtendSeason is accepted only from a committee/council policy
    # address (x/season SEA-10), so it goes through an Operations Committee
    # proposal that alice submits, votes and executes.
    # shellcheck source=./lib/ops_committee.sh
    source "$(dirname "${BASH_SOURCE[0]}")/lib/ops_committee.sh"
    if ! BINARY="$bin" CHAIN_ID="$chain_id" BOND_DENOM="$denom" ops_committee_exec \
        "{\"@type\":\"/sparkdream.season.v1.MsgExtendSeason\",\"extension_epochs\":\"$max_epochs\",\"reason\":\"e2e horizon for $module suite\"}"; then
        echo "  [WARN] season horizon: extend-season proposal failed: $OPS_COMMITTEE_ERROR"
        return 0
    fi
    end_block=$("$bin" query season current-season --output json 2>/dev/null | jq -r '.end_block // "0"')
    echo "  [ OK ] season horizon: season 1 now ends at block $end_block"
    return 0
}
