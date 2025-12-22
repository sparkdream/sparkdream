package keeper

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/group"
)

func (k msgServer) RenewGroup(goCtx context.Context, msg *types.MsgRenewGroup) (*types.MsgRenewGroupResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Retrieve Group
	extGroup, err := k.ExtendedGroup.Get(ctx, msg.GroupName)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "group %s not found", msg.GroupName)
	}

	// 2. Determine Authority Level
	// Check if the signer is x/gov (The Supreme Authority)
	isGov := msg.Authority == k.GetAuthorityString()

	// 3. Validation: Input Integrity (Always Enforced)
	if len(msg.NewMembers) != len(msg.NewMemberWeights) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "new_members count (%d) does not match new_member_weights count (%d)", len(msg.NewMembers), len(msg.NewMemberWeights))
	}

	// 4. Strict Validation Rules (Enforced ONLY if signer is NOT Governance)
	if !isGov {
		// A. Authority Check:
		// Allow Parent OR Designated Electoral Authority
		isParent := (msg.Authority == extGroup.ParentPolicyAddress)
		isElectoral := (extGroup.ElectoralPolicyAddress != "" && msg.Authority == extGroup.ElectoralPolicyAddress)

		if !isParent && !isElectoral {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
				"signer %s is not the parent or designated electoral authority of %s", msg.Authority, msg.GroupName)
		}

		// B. Term Expiration Check (Time Lock)
		currentTime := ctx.BlockTime().Unix()
		if currentTime < extGroup.CurrentTermExpiration {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "current term has not expired yet (expires: %d, current: %d)", extGroup.CurrentTermExpiration, currentTime)
		}

		// C. Group Size Check
		// Governance can override Min/Max constraints (e.g. install 1 dictator),
		// but the group itself/electoral authority cannot break its own bounds.
		newCount := uint64(len(msg.NewMembers))
		if newCount < extGroup.MinMembers {
			return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize, "count %d below min %d", newCount, extGroup.MinMembers)
		}
		if newCount > extGroup.MaxMembers {
			return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize, "count %d above max %d", newCount, extGroup.MaxMembers)
		}
	}

	// =========================================================================
	// 5. CALCULATE FINAL STATE (Deduplication Logic)
	// Map[Address] -> TargetWeight ("0" = Remove, ">0" = Add/Keep)
	// =========================================================================

	finalState := make(map[string]string)

	// Step A: Mark ALL current members for removal (Weight "0")
	currentMembers, err := k.groupKeeper.GroupMembers(ctx, &group.QueryGroupMembersRequest{GroupId: extGroup.GroupId})
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to fetch current group members")
	}

	for _, m := range currentMembers.Members {
		finalState[m.Member.Address] = "0"
	}

	// Step B: Apply New Members (Overwrite "0" with new weight)
	newMembersParsed, err := k.parseMembers(msg.NewMembers, msg.NewMemberWeights)
	if err != nil {
		return nil, err
	}

	var totalHumanWeight uint64 = 0
	for _, m := range newMembersParsed {
		finalState[m.Address] = m.Weight

		// Sum weight for futarchy calculation
		w, _ := strconv.ParseUint(m.Weight, 10, 64)
		totalHumanWeight += w
	}

	// Step C: Futarchy Calculation
	if extGroup.FutarchyEnabled && extGroup.FutarchyMemberAddress != "" {
		futarchyWeight := totalHumanWeight / 4
		if futarchyWeight == 0 {
			futarchyWeight = 1
		}
		// Set Futarchy Bot weight (overwriting any previous removal/add)
		finalState[extGroup.FutarchyMemberAddress] = fmt.Sprintf("%d", futarchyWeight)
	}

	// Step D: Convert Map to List
	var memberUpdates []group.MemberRequest
	for addr, weight := range finalState {
		memberUpdates = append(memberUpdates, group.MemberRequest{
			Address:  addr,
			Weight:   weight,
			Metadata: "Renewed via x/commons",
		})
	}

	// Deterministic Ordering (Sort by Address)
	sort.Slice(memberUpdates, func(i, j int) bool {
		return memberUpdates[i].Address < memberUpdates[j].Address
	})

	// 6. Execute Update via x/group
	_, err = k.groupKeeper.UpdateGroupMembers(ctx, &group.MsgUpdateGroupMembers{
		Admin:         k.GetModuleAddress().String(),
		GroupId:       extGroup.GroupId,
		MemberUpdates: memberUpdates,
	})
	if err != nil {
		return nil, errorsmod.Wrap(err, "x/group update failed")
	}

	// 7. Reset Term
	// We reset the clock even if Gov forced it, to establish the new regime's term.
	extGroup.CurrentTermExpiration = ctx.BlockTime().Unix() + extGroup.TermDuration
	if err := k.ExtendedGroup.Set(ctx, msg.GroupName, extGroup); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update extended group state")
	}

	return &types.MsgRenewGroupResponse{}, nil
}
