package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/session/types"
)

// MigrationHooks implements x/rep's MemberMigrationHooks for x/session
// (member-key-migration plan §8, session row).
//
// Grants with the old address as GRANTER are path-aware. On the KEY path they
// are re-keyed to the new address: the requester controls the old key
// whoever they are, so revoking protects nothing, and for a multisig member
// the grants are the whole daily setup (session hot keys, SignIn grants). On
// the RECOVERY and COUNCIL paths they are revoked, deposits refunded through
// the bank redirect: a compromised old key may have created them.
//
// Grants with the old address as GRANTEE: session keys are revoked on every
// path (a hot key that could act as the old identity must not act as the new
// one unasked); every other type (recurring pulls and allowances paying the
// member, sign-ins) is re-keyed so money and permissions keep reaching the
// member.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/session migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// BeforeMemberMigration refuses a target that is already a granter: those
// grants carry identity. Grantee-side grants (a fee grant or allowance from
// a friend that pays a fresh address's gas) are allowed (plan §4.4).
func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	grants, err := h.k.ListGrantsByGranter(ctx, to.String(), types.GrantType_GRANT_TYPE_UNSPECIFIED)
	if err != nil {
		return err
	}
	if len(grants) > 0 {
		return fmt.Errorf("x/session: %s is the granter of %d grant(s)", to.String(), len(grants))
	}
	return nil
}

// AfterMemberMigration re-keys or revokes the member's grants.
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	k := h.k
	fromStr, toStr := from.String(), to.String()
	keyPath := path == reptypes.MigrationPath_MIGRATION_PATH_KEY

	asGranter, err := k.ListGrantsByGranter(ctx, fromStr, types.GrantType_GRANT_TYPE_UNSPECIFIED)
	if err != nil {
		return err
	}
	for _, g := range asGranter {
		if keyPath {
			if err := k.rekeyGrant(ctx, g, toStr, g.Grantee); err != nil {
				return err
			}
			continue
		}
		if err := k.revokeForMigration(ctx, g, fromStr, toStr); err != nil {
			return err
		}
	}

	asGrantee, err := k.ListGrantsByGrantee(ctx, fromStr, types.GrantType_GRANT_TYPE_UNSPECIFIED)
	if err != nil {
		return err
	}
	for _, g := range asGrantee {
		if g.Granter == toStr {
			continue // already handled above as a re-keyed granter grant
		}
		if g.Type == types.GrantType_GRANT_TYPE_SESSION_KEY {
			if err := k.revokeForMigration(ctx, g, fromStr, toStr); err != nil {
				return err
			}
			continue
		}
		if err := k.rekeyGrant(ctx, g, g.Granter, toStr); err != nil {
			return err
		}
	}

	// PENDING grants naming the old address as grantee are on no index
	// ListGrantsByGrantee walks; apply the grantee-side policy to them too:
	// a pending session key is dropped (its deposit goes back to its
	// granter, who is not the migrating member), a pending sign-in is
	// re-keyed and stays pending for the new address to accept.
	pending, err := k.listPendingGrantsForGrantee(ctx, fromStr)
	if err != nil {
		return err
	}
	for _, g := range pending {
		if g.Granter == toStr {
			continue
		}
		if g.Type == types.GrantType_GRANT_TYPE_SESSION_KEY {
			if err := k.dropPendingForMigration(ctx, g, fromStr, toStr); err != nil {
				return err
			}
			continue
		}
		if err := k.rekeyGrant(ctx, g, g.Granter, toStr); err != nil {
			return err
		}
	}

	// Allowed-granter entries follow the member's granter-side grants
	// (SES-102): re-keyed on the KEY path, dropped on RECOVERY / COUNCIL so
	// a compromised old key's consents do not carry over. The grantee side
	// is never re-keyed: hot keys are not members.
	return k.migrateAllowedGranters(ctx, fromStr, toStr, keyPath)
}

// migrateAllowedGranters rewrites or removes every (grantee, from) consent
// entry, walking the granter-keyed reverse index.
func (k Keeper) migrateAllowedGranters(ctx context.Context, fromStr, toStr string, keyPath bool) error {
	var grantees []string
	rng := collections.NewPrefixedPairRange[string, string](fromStr)
	if err := k.AllowedGranteesByGranter.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		grantees = append(grantees, key.K2())
		return false, nil
	}); err != nil {
		return err
	}
	for _, grantee := range grantees {
		if err := k.removeAllowedGranter(ctx, grantee, fromStr); err != nil {
			return err
		}
		if keyPath && grantee != toStr {
			if err := k.setAllowedGranter(ctx, grantee, toStr); err != nil {
				return err
			}
		}
	}
	return nil
}

// listPendingGrantsForGrantee returns the PENDING grants naming grantee,
// from the query-only PendingGrantsByGrantee index. Pending grants are
// capped per granter, not per grantee, so this walk is bounded only by
// how many distinct granters named the address; it runs inside a
// migration tx, which is rare and signed by the member.
func (k Keeper) listPendingGrantsForGrantee(ctx context.Context, grantee string) ([]types.Grant, error) {
	var out []types.Grant
	rng := collections.NewPrefixedPairRange[string, uint64](grantee)
	err := k.PendingGrantsByGrantee.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		g, err := k.Grants.Get(ctx, key.K2())
		if err != nil {
			return true, err
		}
		if g.Status == types.GrantStatus_GRANT_STATUS_PENDING {
			out = append(out, g)
		}
		return false, nil
	})
	return out, err
}

// dropPendingForMigration deletes a PENDING grantee-side grant of a
// migrating member and refunds the consent deposit to its granter.
func (k Keeper) dropPendingForMigration(ctx context.Context, g types.Grant, fromStr, toStr string) error {
	refund, err := k.refundPendingDeposit(ctx, g, g.Granter)
	if err != nil {
		return err
	}
	if err := k.deleteGrant(ctx, g); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("grant_revoked",
		sdk.NewAttribute("id", fmt.Sprintf("%d", g.Id)),
		sdk.NewAttribute("type", types.GrantTypeEventValue(g.Type)),
		sdk.NewAttribute("granter", g.Granter),
		sdk.NewAttribute("grantee", g.Grantee),
		sdk.NewAttribute("refund_amount", refund.String()),
		sdk.NewAttribute("source", "member_migration"),
		sdk.NewAttribute("migrated_from", fromStr),
		sdk.NewAttribute("migrated_to", toStr),
	))
	return nil
}

// rekeyGrant rewrites a grant's parties, rebuilding every index.
func (k Keeper) rekeyGrant(ctx context.Context, g types.Grant, granter, grantee string) error {
	// The per-day spend buckets belong to the grant id, not its parties:
	// carry them across the delete+write so a re-key never resets the
	// max_per_epoch throttle.
	type bucket struct {
		key collections.Pair[uint64, int64]
		val string
	}
	var buckets []bucket
	if err := k.EpochSpendByGrant.Walk(ctx, collections.NewPrefixedPairRange[uint64, int64](g.Id),
		func(key collections.Pair[uint64, int64], v string) (bool, error) {
			buckets = append(buckets, bucket{key, v})
			return false, nil
		}); err != nil {
		return err
	}
	creator, creatorErr := k.GrantCreatorModule.Get(ctx, g.Id)
	if err := k.deleteGrant(ctx, g); err != nil {
		return err
	}
	g.Granter = granter
	g.Grantee = grantee
	if err := k.writeGrant(ctx, g); err != nil {
		return err
	}
	if creatorErr == nil {
		if err := k.GrantCreatorModule.Set(ctx, g.Id, creator); err != nil {
			return err
		}
	}
	for _, b := range buckets {
		if err := k.EpochSpendByGrant.Set(ctx, b.key, b.val); err != nil {
			return err
		}
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("grant_migrated",
		sdk.NewAttribute("id", fmt.Sprintf("%d", g.Id)),
		sdk.NewAttribute("type", types.GrantTypeEventValue(g.Type)),
		sdk.NewAttribute("granter", g.Granter),
		sdk.NewAttribute("grantee", g.Grantee),
	))
	return nil
}

// revokeForMigration deletes a grant and refunds any oneshot deposit to the
// migrating member's new address.
func (k Keeper) revokeForMigration(ctx context.Context, g types.Grant, fromStr, toStr string) error {
	refund := sdk.NewCoin(k.BondDenom(ctx), sdkmath.ZeroInt())
	if g.Type == types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
		if dep, err := k.OneshotGasDeposit.Get(ctx, g.Id); err == nil && !dep.IsZero() {
			recipient, err := k.addressCodec.StringToBytes(toStr)
			if err != nil {
				return err
			}
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(dep)); err != nil {
				return err
			}
			if err := k.OneshotGasDeposit.Remove(ctx, g.Id); err != nil {
				return err
			}
			refund = dep
		}
	}
	if g.Status == types.GrantStatus_GRANT_STATUS_PENDING {
		var err error
		if refund, err = k.refundPendingDeposit(ctx, g, toStr); err != nil {
			return err
		}
	}
	if err := k.deleteGrant(ctx, g); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("grant_revoked",
		sdk.NewAttribute("id", fmt.Sprintf("%d", g.Id)),
		sdk.NewAttribute("type", types.GrantTypeEventValue(g.Type)),
		sdk.NewAttribute("granter", g.Granter),
		sdk.NewAttribute("grantee", g.Grantee),
		sdk.NewAttribute("refund_amount", refund.String()),
		sdk.NewAttribute("source", "member_migration"),
		sdk.NewAttribute("migrated_from", fromStr),
		sdk.NewAttribute("migrated_to", toStr),
	))
	return nil
}
