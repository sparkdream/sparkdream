package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// FinalizeMigration moves the identity from the migration's old address to
// its new one (plan §5). Every step is bounded; the unbounded remainder
// (authorship, stakes, votes, escrows) stays under the old address and is
// read through the alias written in step 3. Callers run it inside a child
// cache context and mark the migration FAILED on error, so a bug here can
// never halt the chain.
func (k Keeper) FinalizeMigration(ctx context.Context, id uint64) error {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return types.ErrMigrationNotFound
	}
	if m.Status != types.MigrationStatus_MIGRATION_STATUS_ACCEPTED {
		return errorsmod.Wrapf(types.ErrMigrationWrongStatus, "status %s", m.Status)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	from, to := m.FromAddress, m.ToAddress
	fromAddr, err := k.addressCodec.StringToBytes(from)
	if err != nil {
		return err
	}
	toAddr, err := k.addressCodec.StringToBytes(to)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	epoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}

	// 1. Re-verify preconditions. A member zeroed during the delay has the
	//    migration cancelled rather than failed.
	member, err := k.checkMigrationSource(ctx, from)
	if err != nil {
		if errorsmod.IsOf(err, types.ErrMigrationZeroedMember) {
			return k.cancelMigration(ctx, &m, types.MigrationStatus_MIGRATION_STATUS_CANCELLED, "member zeroed during the delay")
		}
		return err
	}
	if err := k.checkTargetFreshAtAccept(ctx, m); err != nil {
		return err
	}
	if err := k.migrationHooks().BeforeMemberMigration(ctx, fromAddr, toAddr, m.Path); err != nil {
		return errorsmod.Wrap(types.ErrMigrationVetoed, err.Error())
	}

	// 2. Re-key the x/rep core.
	member.Address = to
	member.LastMigratedAt = now
	if m.Path == types.MigrationPath_MIGRATION_PATH_KEY {
		times := make([]int64, 0, int(params.MaxKeyMigrationsPerWindow))
		for _, t := range member.KeyMigrationTimes {
			if t > now-params.KeyMigrationWindow {
				times = append(times, t)
			}
		}
		times = append(times, now)
		if n := int(params.MaxKeyMigrationsPerWindow); len(times) > n {
			times = times[len(times)-n:]
		}
		member.KeyMigrationTimes = times
	} else {
		// A compromised key means a compromised signature-derived shield
		// secret: clear the ZK key so the thief cannot keep acting
		// anonymously as the recovered identity. The next registration
		// waits out the activation delay but skips the season cooldown.
		if len(member.ZkPublicKey) > 0 || len(member.PendingZkPublicKey) > 0 {
			if len(member.PendingZkPublicKey) > 0 {
				if err := k.ZkKeyActivationQueue.Remove(ctx, collections.Join(member.ZkKeyActivatesAtEpoch, from)); err != nil {
					return err
				}
			}
			member.ZkPublicKey = nil
			member.PendingZkPublicKey = nil
			member.ZkKeyActivatesAtEpoch = 0
			member.ZkKeyCooldownExempt = true
		}
	}
	if err := k.Member.Remove(ctx, from); err != nil {
		return err
	}
	if err := k.Member.Set(ctx, to, member); err != nil {
		return err
	}
	// Pending ZK key activation (KEY path keeps it) follows the record.
	if len(member.PendingZkPublicKey) > 0 {
		if err := k.ZkKeyActivationQueue.Remove(ctx, collections.Join(member.ZkKeyActivatesAtEpoch, from)); err != nil {
			return err
		}
		if err := k.ZkKeyActivationQueue.Set(ctx, collections.Join(member.ZkKeyActivatesAtEpoch, to)); err != nil {
			return err
		}
	}

	// Trust tree: the leaf index follows the member; the leaf value is
	// unchanged on the KEY path (same key, same trust level) and zeroed on
	// the others because the key was cleared. Marking the new address dirty
	// recomputes it either way.
	k.moveTrustTreeLeaf(ctx, from, to)

	if invID, err := k.InvitationsByInvitee.Get(ctx, from); err == nil {
		if err := k.InvitationsByInvitee.Remove(ctx, from); err != nil {
			return err
		}
		if err := k.InvitationsByInvitee.Set(ctx, to, invID); err != nil {
			return err
		}
	}
	for _, rt := range types.AllRoleTypes() {
		key := collections.Join(int32(rt), from)
		if role, err := k.BondedRoles.Get(ctx, key); err == nil {
			role.Address = to
			if role.UnbondCompletionTime != 0 {
				if err := k.BondedRoleUnbondQueue.Remove(ctx, collections.Join3(role.UnbondCompletionTime, int32(rt), from)); err != nil {
					return err
				}
			}
			if err := k.BondedRoles.Remove(ctx, key); err != nil {
				return err
			}
			if err := k.setBondedRoleAt(ctx, collections.Join(int32(rt), to), role); err != nil {
				return err
			}
		}
		if act, err := k.RoleActivities.Get(ctx, key); err == nil {
			act.Address = to
			if err := k.RoleActivities.Remove(ctx, key); err != nil {
				return err
			}
			if err := k.RoleActivities.Set(ctx, collections.Join(int32(rt), to), act); err != nil {
				return err
			}
		}
	}
	if jp, err := k.JuryParticipation.Get(ctx, from); err == nil {
		jp.Juror = to
		if err := k.JuryParticipation.Remove(ctx, from); err != nil {
			return err
		}
		if err := k.JuryParticipation.Set(ctx, to, jp); err != nil {
			return err
		}
	}
	if pool, err := k.MemberStakePool.Get(ctx, from); err == nil {
		pool.Member = to
		if err := k.MemberStakePool.Remove(ctx, from); err != nil {
			return err
		}
		if err := k.MemberStakePool.Set(ctx, to, pool); err != nil {
			return err
		}
	}
	if report, err := k.MemberReport.Get(ctx, from); err == nil {
		report.Member = to
		if err := k.MemberReport.Remove(ctx, from); err != nil {
			return err
		}
		if err := k.MemberReport.Set(ctx, to, report); err != nil {
			return err
		}
	}
	if rest, err := k.MemberActionRestorations.Get(ctx, from); err == nil {
		if err := k.removeMemberActionRestoration(ctx, rest); err != nil {
			return err
		}
		rest.Member = to
		if err := k.setMemberActionRestoration(ctx, rest); err != nil {
			return err
		}
	}
	// Gifts are only ever sent by the inviter, so the records where the
	// member is the recipient are (any address of the inviter, from): the
	// inviter may itself have migrated since (bounded by MaxLifetimeAliases).
	if member.InvitedBy != "" {
		for _, giver := range k.AllAddressesOf(ctx, member.InvitedBy) {
			giftKey := collections.Join(giver, from)
			gift, err := k.GiftRecord.Get(ctx, giftKey)
			if err != nil {
				continue
			}
			gift.Recipient = to
			if err := k.GiftRecord.Remove(ctx, giftKey); err != nil {
				return err
			}
			if err := k.GiftRecord.Set(ctx, collections.Join(giver, to), gift); err != nil {
				return err
			}
		}
	}
	if set, err := k.RecoveryCommitments.Get(ctx, from); err == nil {
		if err := k.RecoveryCommitments.Remove(ctx, from); err != nil {
			return err
		}
		if err := k.RecoveryCommitments.Set(ctx, to, set); err != nil {
			return err
		}
	}

	// 3. Write the alias and path-compress every predecessor.
	if err := k.Tombstones.Set(ctx, from, types.Tombstone{
		Address:        from,
		Canonical:      to,
		Path:           m.Path,
		MigrationId:    id,
		FinalizedEpoch: epoch,
	}); err != nil {
		return err
	}
	var predecessors []collections.Triple[string, int64, string]
	rng := collections.NewPrefixedTripleRange[string, int64, string](from)
	if err := k.MigratedFrom.Walk(ctx, rng, func(key collections.Triple[string, int64, string]) (bool, error) {
		predecessors = append(predecessors, key)
		return false, nil
	}); err != nil {
		return err
	}
	for _, key := range predecessors {
		old := key.K3()
		t, err := k.Tombstones.Get(ctx, old)
		if err != nil {
			return err
		}
		t.Canonical = to
		if err := k.Tombstones.Set(ctx, old, t); err != nil {
			return err
		}
		if err := k.MigratedFrom.Remove(ctx, key); err != nil {
			return err
		}
		if err := k.MigratedFrom.Set(ctx, collections.Join3(to, key.K2(), old)); err != nil {
			return err
		}
	}
	if err := k.MigratedFrom.Set(ctx, collections.Join3(to, epoch, from)); err != nil {
		return err
	}

	// 4. Module hooks re-key their bounded core state.
	if err := k.migrationHooks().AfterMemberMigration(ctx, fromAddr, toAddr, m.Path); err != nil {
		return errorsmod.Wrap(err, "migration hook failed")
	}

	// 5. SPARK clean-up: sweep the spendable balance and point staking
	//    rewards at the new address. On the KEY path delegations stay where
	//    they are (the owner still holds the old key). On RECOVERY and
	//    COUNCIL the old key is lost or stolen, so every delegation starts
	//    unbonding now; MsgSweepTombstone collects the matured SPARK.
	if spendable := k.bankKeeper.SpendableCoins(ctx, fromAddr); !spendable.IsZero() {
		if err := k.bankKeeper.SendCoins(ctx, fromAddr, toAddr, spendable); err != nil {
			return errorsmod.Wrap(err, "sweep spendable balance")
		}
	}
	if k.late.distrKeeper != nil {
		if err := k.late.distrKeeper.SetWithdrawAddr(ctx, fromAddr, toAddr); err != nil {
			sdkCtx.Logger().Info("could not redirect staking rewards of migrated member", "from", from, "to", to, "error", err)
		}
	}
	if m.Path != types.MigrationPath_MIGRATION_PATH_KEY {
		k.undelegateTombstone(ctx, fromAddr, "migration")
	}

	// Close the migration.
	if err := k.clearPendingIndexes(ctx, m, params); err != nil {
		return err
	}
	m.Status = types.MigrationStatus_MIGRATION_STATUS_FINALIZED
	m.FinalizedAt = now
	if err := k.MemberMigrations.Set(ctx, id, m); err != nil {
		return err
	}

	// 6. Emit.
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migrated",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("from", from),
		sdk.NewAttribute("to", to),
		sdk.NewAttribute("path", m.Path.String()),
	))
	return nil
}

// moveTrustTreeLeaf transfers the member's leaf index from one address to
// another and marks the new address dirty so the next EndBlock recomputes
// the leaf. The old address's dirty mark, if any, is dropped: the record no
// longer exists under it.
func (k Keeper) moveTrustTreeLeaf(ctx context.Context, from, to string) {
	store := k.storeService.OpenKVStore(ctx)
	if idx, ok := k.getMemberLeafIndex(ctx, from); ok {
		_ = store.Delete(memberIdxKey(from))
		k.setMemberLeafIndex(ctx, to, idx)
	}
	_ = store.Delete(dirtyMemberKey(from))
	k.MarkMemberDirty(ctx, to)
}
