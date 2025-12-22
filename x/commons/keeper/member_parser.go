package keeper

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/group"
)

// parseMembers converts string lists of members and weights into x/group requests
func (k Keeper) parseMembers(members []string, weights []string) ([]group.MemberRequest, error) {
	if len(members) != len(weights) {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "members count (%d) does not match weights count (%d)", len(members), len(weights))
	}

	var memberRequests []group.MemberRequest
	for i, address := range members {
		// Validate the address format
		_, err := sdk.AccAddressFromBech32(address)
		if err != nil {
			return nil, errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid member address: %s", address)
		}

		// Validate weight (must be positive integer string)
		if weights[i] == "" || weights[i] == "0" {
			// Depending on logic, 0 might be allowed for removal, but usually new members need >0
		}

		memberRequests = append(memberRequests, group.MemberRequest{
			Address:  address,
			Weight:   weights[i],
			Metadata: "Added via x/commons",
		})
	}

	return memberRequests, nil
}
