package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ZeroingHook adapts the keeper to x/rep's MemberZeroingHook.
type ZeroingHook struct{ k *Keeper }

// NewZeroingHook returns the hook app.go registers with x/rep.
func NewZeroingHook(k *Keeper) ZeroingHook { return ZeroingHook{k: k} }

// AfterMemberZeroed drops a zeroed member's DREAM lock records. x/rep
// burned the DREAM behind them with the rest of the member's balance; kept,
// their later release would fail against a lock that no longer exists, or,
// once an overturned zeroing gave the member a balance back, release an
// unrelated lock (REV-101).
//
//   - Nomination stakes: kept, they would go on adding conviction to the
//     nominations they back and the RETURN phase would try to unlock DREAM
//     that is gone. Each backed nomination's total and conviction are
//     recomputed without the stake.
//   - Display-name report and appeal stakes (SEA-101): the record goes, the
//     moderation case stays open and resolves as usual; its resolution
//     finds no stake for the zeroed side and settles only the other one.
//     Kept, every resolution path failed on the zeroed side's unlock or
//     burn, the case stayed active for good and the target's name stayed
//     cleared.
//
// Each read goes through a by-staker index, so the hook costs what the
// member holds, not what the table holds (SEA-104).
func (h ZeroingHook) AfterMemberZeroed(ctx context.Context, member string, addresses []string) error {
	k := h.k
	nominations, err := k.dropZeroedNominationStakes(ctx, addresses)
	if err != nil {
		return err
	}
	reports, appeals, err := k.dropZeroedDisplayNameStakes(ctx, addresses)
	if err != nil {
		return err
	}
	if nominations+reports+appeals > 0 {
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
			"season_zeroed_stakes_dropped",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("stakes_dropped", fmt.Sprintf("%d", nominations)),
			sdk.NewAttribute("display_name_report_stakes_dropped", fmt.Sprintf("%d", reports)),
			sdk.NewAttribute("display_name_appeal_stakes_dropped", fmt.Sprintf("%d", appeals)),
		))
	}
	return nil
}

// indexedKeys collects the primary keys an index holds under ref.
func indexedKeys[V any](ctx context.Context, idx *indexes.Multi[string, string, V], ref string) ([]string, error) {
	var keys []string
	err := idx.Walk(ctx, collections.NewPrefixedPairRange[string, string](ref), func(_ string, pk string) (bool, error) {
		keys = append(keys, pk)
		return false, nil
	})
	return keys, err
}

// dropZeroedNominationStakes removes the stakes held under addresses and
// takes them out of their nominations' totals. Returns the count dropped.
func (k Keeper) dropZeroedNominationStakes(ctx context.Context, addresses []string) (int, error) {
	dropped := 0
	for _, addr := range addresses {
		keys, err := indexedKeys(ctx, k.NominationStake.Indexes.ByStaker, addr)
		if err != nil {
			return dropped, err
		}
		for _, key := range keys {
			s, err := k.NominationStake.Get(ctx, key)
			if err != nil {
				if errors.Is(err, collections.ErrNotFound) {
					// An index hit without a record cannot happen with the
					// IndexedMap; if it ever does, there is no lock to drop,
					// and aborting the whole hook (which x/rep logs and
					// discards) would leave the member's other stakes in
					// place and reopen the SEA-101 wedge.
					continue
				}
				return dropped, err
			}
			if err := k.NominationStake.Remove(ctx, key); err != nil {
				return dropped, err
			}
			dropped++
			nom, err := k.Nomination.Get(ctx, s.NominationId)
			if err != nil {
				continue
			}
			nom.TotalStaked = nom.TotalStaked.Sub(s.Amount)
			if nom.TotalStaked.IsNegative() {
				nom.TotalStaked = math.LegacyZeroDec()
			}
			conviction, err := k.CalculateNominationConviction(ctx, nom)
			if err != nil {
				return dropped, err
			}
			nom.Conviction = conviction
			if err := k.SetNomination(ctx, nom); err != nil {
				return dropped, err
			}
		}
	}
	return dropped, nil
}

// dropZeroedDisplayNameStakes removes the display-name report and appeal
// stakes held under addresses. The moderation records are left as they are.
func (k Keeper) dropZeroedDisplayNameStakes(ctx context.Context, addresses []string) (reports, appeals int, err error) {
	for _, addr := range addresses {
		keys, err := indexedKeys(ctx, k.DisplayNameReportStake.Indexes.ByReporter, addr)
		if err != nil {
			return reports, appeals, err
		}
		for _, key := range keys {
			if err := k.DisplayNameReportStake.Remove(ctx, key); err != nil {
				return reports, appeals, err
			}
			reports++
		}
		keys, err = indexedKeys(ctx, k.DisplayNameAppealStake.Indexes.ByAppellant, addr)
		if err != nil {
			return reports, appeals, err
		}
		for _, key := range keys {
			if err := k.DisplayNameAppealStake.Remove(ctx, key); err != nil {
				return reports, appeals, err
			}
			appeals++
		}
	}
	return reports, appeals, nil
}
