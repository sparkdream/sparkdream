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
// x/storage existed, and the committee the storage price message. The
// genesis bootstrap seeds the same entries on a new chain. A body that no
// longer exists is skipped; entries already there are kept.
func (k Keeper) MigrateV2ToV3(ctx context.Context) error {
	grants := []struct {
		group string
		msgs  []string
	}{
		{"Commons Council", storageCouncilMsgs},
		{"Commons Operations Committee", append(append([]string(nil), storageCouncilMsgs...),
			"/sparkdream.storage.v1.MsgUpdateOperationalParams")},
	}
	for _, g := range grants {
		group, err := k.Groups.Get(ctx, g.group)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return err
		}
		if group.PolicyAddress == "" {
			continue
		}
		perms, err := k.PolicyPermissions.Get(ctx, group.PolicyAddress)
		if err != nil {
			if !errors.Is(err, collections.ErrNotFound) {
				return err
			}
			perms = types.PolicyPermissions{PolicyAddress: group.PolicyAddress}
		}
		have := make(map[string]bool, len(perms.AllowedMessages))
		for _, m := range perms.AllowedMessages {
			have[m] = true
		}
		for _, m := range g.msgs {
			if !have[m] {
				perms.AllowedMessages = append(perms.AllowedMessages, m)
				have[m] = true
			}
		}
		if err := k.PolicyPermissions.Set(ctx, group.PolicyAddress, perms); err != nil {
			return err
		}
	}
	return nil
}
