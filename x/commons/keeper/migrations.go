package keeper

import (
	"context"
	"errors"
	"slices"

	"cosmossdk.io/collections"

	"sparkdream/x/commons/types"
)

// storageCouncilMsgs are the x/storage messages a controller of storage
// operators and scanners signs through a proposal (docs/x-storage-spec.md).
var storageCouncilMsgs = []string{
	"/sparkdream.storage.v1.MsgApproveOperator",
	"/sparkdream.storage.v1.MsgRemoveOperator",
	"/sparkdream.storage.v1.MsgApproveScanner",
	"/sparkdream.storage.v1.MsgRemoveScanner",
	"/sparkdream.storage.v1.MsgVoidCompletion",
	"/sparkdream.storage.v1.MsgUnflagRemoved",
}

// MigrateV2ToV3 gives the Commons Council and the Commons Operations
// Committee the x/storage council messages on a chain that started before
// x/storage existed, and the committee the storage price message. It also
// gives the Technical and Ecosystem Councils, on both their standard and
// veto policies, the messages a council needs to hold its seat on the
// Commons Supervisory Board (BoardSeatMsgs): without them the board's two
// members could neither propose nor vote, so it could never veto the Commons
// Governance Committee. The genesis bootstrap seeds the same entries on a
// new chain. A body that no longer exists is skipped; entries already there
// are kept.
func (k Keeper) MigrateV2ToV3(ctx context.Context) error {
	grants := []struct {
		group string
		msgs  []string
		// veto: also grant the group's veto policy, when it has one.
		veto bool
	}{
		{group: "Commons Council", msgs: storageCouncilMsgs},
		{group: "Commons Operations Committee", msgs: append(append([]string(nil), storageCouncilMsgs...),
			"/sparkdream.storage.v1.MsgUpdateOperationalParams")},
		{group: "Technical Council", msgs: BoardSeatMsgs, veto: true},
		{group: "Ecosystem Council", msgs: BoardSeatMsgs, veto: true},
	}
	for _, g := range grants {
		group, err := k.Groups.Get(ctx, g.group)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return err
		}
		policies := []string{group.PolicyAddress}
		if g.veto {
			policies = append(policies, group.VetoPolicyAddress)
		}
		for _, policy := range policies {
			if policy == "" {
				continue
			}
			if err := k.grantMessages(ctx, policy, g.msgs); err != nil {
				return err
			}
		}
	}
	return nil
}

// TreasuryStakingMsgs are the x/commons messages a group signs through a
// proposal to stake its treasury (treasury_staking.go). Withdrawing rewards
// needs no permission: anyone may send MsgWithdrawTreasuryRewards.
var TreasuryStakingMsgs = []string{
	"/sparkdream.commons.v1.MsgDelegateTreasury",
	"/sparkdream.commons.v1.MsgUndelegateTreasury",
	"/sparkdream.commons.v1.MsgRedelegateTreasury",
}

// TreasuryStakingGroups are the genesis bodies that hold a treasury (a
// share of x/split revenue): the three councils and their Operations
// Committees. The governance committees and the Supervisory Board have no
// funds.
var TreasuryStakingGroups = []string{
	"Commons Council",
	"Technical Council",
	"Ecosystem Council",
	"Commons Operations Committee",
	"Technical Operations Committee",
	"Ecosystem Operations Committee",
}

// MigrateV3ToV4 gives the standard policy of every genesis body with a
// treasury the treasury staking messages, on a chain that started before
// they existed. The genesis bootstrap seeds the same entries on a new
// chain. A body that no longer exists is skipped; entries already there are
// kept. Groups registered later get the messages from their parent or
// x/gov with MsgUpdatePolicyPermissions.
//
// It also takes every forbidden message off every stored allowlist (the
// raw staking messages became forbidden with this version, and a grant made
// before that would otherwise survive), and indexes the groups that already
// hold a delegation in TreasuryStakers.
func (k Keeper) MigrateV3ToV4(ctx context.Context) error {
	if err := k.stripForbiddenMessages(ctx); err != nil {
		return err
	}
	if err := k.indexTreasuryStakers(ctx); err != nil {
		return err
	}
	for _, name := range TreasuryStakingGroups {
		group, err := k.Groups.Get(ctx, name)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return err
		}
		if group.PolicyAddress == "" {
			continue
		}
		if err := k.grantMessages(ctx, group.PolicyAddress, TreasuryStakingMsgs); err != nil {
			return err
		}
	}
	return nil
}

// stripForbiddenMessages removes every types.ForbiddenMessages entry from
// every stored allowlist.
func (k Keeper) stripForbiddenMessages(ctx context.Context) error {
	var changed []types.PolicyPermissions
	if err := k.PolicyPermissions.Walk(ctx, nil, func(_ string, perms types.PolicyPermissions) (bool, error) {
		kept := slices.DeleteFunc(slices.Clone(perms.AllowedMessages), func(m string) bool {
			return types.ForbiddenMessages[m]
		})
		if len(kept) != len(perms.AllowedMessages) {
			perms.AllowedMessages = kept
			changed = append(changed, perms)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, perms := range changed {
		if err := k.PolicyPermissions.Set(ctx, perms.PolicyAddress, perms); err != nil {
			return err
		}
	}
	return nil
}

// indexTreasuryStakers writes a TreasuryStakers entry for every group whose
// standard policy address holds a delegation.
func (k Keeper) indexTreasuryStakers(ctx context.Context) error {
	sk := k.late.stakingKeeper
	if sk == nil {
		return nil
	}
	var policies []string
	if err := k.Groups.Walk(ctx, nil, func(_ string, g types.Group) (bool, error) {
		if g.PolicyAddress != "" {
			policies = append(policies, g.PolicyAddress)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, policy := range policies {
		bz, err := k.addressCodec.StringToBytes(policy)
		if err != nil {
			continue
		}
		delegations, err := sk.GetDelegatorDelegations(ctx, bz, 1)
		if err != nil {
			return err
		}
		if len(delegations) == 0 {
			continue
		}
		if err := k.TreasuryStakers.Set(ctx, policy); err != nil {
			return err
		}
	}
	return nil
}

// grantMessages adds msgs to a policy's allowlist, keeping what it holds and
// adding nothing twice. A policy with no allowlist record gets one.
func (k Keeper) grantMessages(ctx context.Context, policy string, msgs []string) error {
	perms, err := k.PolicyPermissions.Get(ctx, policy)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		perms = types.PolicyPermissions{PolicyAddress: policy}
	}
	have := make(map[string]bool, len(perms.AllowedMessages))
	for _, m := range perms.AllowedMessages {
		have[m] = true
	}
	for _, m := range msgs {
		if !have[m] {
			perms.AllowedMessages = append(perms.AllowedMessages, m)
			have[m] = true
		}
	}
	return k.PolicyPermissions.Set(ctx, policy, perms)
}
