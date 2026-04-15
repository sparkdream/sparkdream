package simulation

import (
	"encoding/hex"
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgSubmitArbiterHash(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Need challenged content for arbiter resolution
		content, contentID, err := getOrCreateChallengedContent(r, ctx, k, addr)
		if err != nil || content.ContentHash == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitArbiterHash{}), "failed to get/create challenged content"), nil, nil
		}

		submission := types.ArbiterHashSubmission{
			ContentId:   contentID,
			ContentHash: content.ContentHash,
			SubmittedAt: ctx.BlockTime().Unix(),
			Operator:    addr,
			Nullifier:   []byte(simtypes.RandStringOfLength(r, 32)),
		}

		if err := k.ArbiterSubmissions.Set(ctx, collections.Join(contentID, addr), submission); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitArbiterHash{}), "failed to set submission"), nil, nil
		}

		// Increment hash count
		hashHex := hex.EncodeToString(content.ContentHash)
		count, _ := k.ArbiterHashCounts.Get(ctx, collections.Join(contentID, hashHex))
		_ = k.ArbiterHashCounts.Set(ctx, collections.Join(contentID, hashHex), count+1)

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitArbiterHash{}), "ok (direct keeper call)"), nil, nil
	}
}
