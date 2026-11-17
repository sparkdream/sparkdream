package keeper

import (
	"context"
	"errors"

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
