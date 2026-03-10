package keeper

import (
	"context"
	"sort"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) RenewGroup(goCtx context.Context, msg *types.MsgRenewGroup) (*types.MsgRenewGroupResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	extGroup, err := k.Groups.Get(ctx, msg.GroupName)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "group %s not found", msg.GroupName)
	}

	isGov := msg.Authority == k.GetAuthorityString()

	if len(msg.NewMembers) != len(msg.NewMemberWeights) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "new_members count (%d) does not match new_member_weights count (%d)", len(msg.NewMembers), len(msg.NewMemberWeights))
	}

	if !isGov {
		isParent := (msg.Authority == extGroup.ParentPolicyAddress)
		isElectoral := (extGroup.ElectoralPolicyAddress != "" && msg.Authority == extGroup.ElectoralPolicyAddress)

		if !isParent && !isElectoral {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
				"signer %s is not the parent or designated electoral authority of %s", msg.Authority, msg.GroupName)
		}

		currentTime := ctx.BlockTime().Unix()
		if currentTime < extGroup.CurrentTermExpiration {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "current term has not expired yet (expires: %d, current: %d)", extGroup.CurrentTermExpiration, currentTime)
		}

		newCount := uint64(len(msg.NewMembers))
		if newCount < extGroup.MinMembers {
			return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize, "count %d below min %d", newCount, extGroup.MinMembers)
		}
		if newCount > extGroup.MaxMembers {
			return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize, "count %d above max %d", newCount, extGroup.MaxMembers)
		}
	}

	// Build final member state
	finalState := make(map[string]string)

	// Mark all current members for removal
	currentMembers, err := k.GetCouncilMembers(ctx, msg.GroupName)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to fetch current group members")
	}
	for _, m := range currentMembers {
		finalState[m.Address] = "0"
	}

	// Apply new members
	newMembersParsed, err := k.parseMembers(msg.NewMembers, msg.NewMemberWeights)
	if err != nil {
		return nil, err
	}
	for _, m := range newMembersParsed {
		finalState[m.Address] = m.Weight
	}

	// Sort for determinism
	type memberUpdate struct {
		Address string
		Weight  string
	}
	var updates []memberUpdate
	for addr, weight := range finalState {
		updates = append(updates, memberUpdate{Address: addr, Weight: weight})
	}
	sort.Slice(updates, func(i, j int) bool {
		return updates[i].Address < updates[j].Address
	})

	// Apply updates to native Members collection
	for _, u := range updates {
		key := collections.Join(msg.GroupName, u.Address)
		if u.Weight == "0" {
			_ = k.Members.Remove(ctx, key)
		} else {
			if err := k.Members.Set(ctx, key, types.Member{
				Address:  u.Address,
				Weight:   u.Weight,
				Metadata: "Renewed via x/commons",
				AddedAt:  ctx.BlockTime().Unix(),
			}); err != nil {
				return nil, errorsmod.Wrap(err, "failed to update member")
			}
		}
	}

	// Reset term
	extGroup.CurrentTermExpiration = ctx.BlockTime().Unix() + extGroup.TermDuration
	if err := k.Groups.Set(ctx, msg.GroupName, extGroup); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update extended group state")
	}

	return &types.MsgRenewGroupResponse{}, nil
}
