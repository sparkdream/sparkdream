package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgConfirmIdentityLink(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Find an unverified link to confirm
		link, err := findIdentityLinkByStatus(r, ctx, k, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED)
		if err != nil || link == nil {
			// Create one
			newLink, err := getOrCreateIdentityLink(r, ctx, k, addr)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgConfirmIdentityLink{}), "failed to get/create identity link"), nil, nil
			}
			link = &newLink
		}

		// Confirm it
		link.Status = types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED
		link.VerifiedAt = ctx.BlockTime().Unix()
		if err := k.IdentityLinks.Set(ctx, collections.Join(link.LocalAddress, link.PeerId), *link); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgConfirmIdentityLink{}), "failed to update link"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgConfirmIdentityLink{}), "ok (direct keeper call)"), nil, nil
	}
}
