package simulation

import (
	"crypto/sha256"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func SimulateMsgStoreSRS(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgStoreSRS{}

		// Generate random SRS data (64-256 bytes)
		srsLen := 64 + r.Intn(193)
		srs := make([]byte, srsLen)
		for i := range srs {
			srs[i] = byte(r.Intn(256))
		}
		hash := sha256.Sum256(srs)

		// Store SRS state directly (bypasses governance authority check)
		srsState := types.SrsState{
			Srs:      srs,
			Hash:     hash[:],
			StoredAt: ctx.BlockHeight(),
		}
		if err := k.SrsState.Set(ctx, srsState); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to store SRS: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
