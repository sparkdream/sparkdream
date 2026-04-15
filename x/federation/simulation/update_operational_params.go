package simulation

import (
	"math/rand"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgUpdateOperationalParams(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateOperationalParams{}), "failed to get params"), nil, nil
		}

		// Randomize operational params within reasonable bounds
		params.MaxInboundPerBlock = uint64(r.Intn(100) + 10)
		params.MaxOutboundPerBlock = uint64(r.Intn(100) + 10)
		params.MaxContentBodySize = uint64(r.Intn(8192) + 1024)
		params.MaxContentUriSize = uint64(r.Intn(4096) + 512)
		params.MaxProtocolMetadataSize = uint64(r.Intn(16384) + 2048)
		params.ContentTtl = time.Duration(r.Intn(180)+30) * 24 * time.Hour
		params.AttestationTtl = time.Duration(r.Intn(60)+7) * 24 * time.Hour
		params.GlobalMaxTrustCredit = uint32(r.Intn(3) + 1)
		params.TrustDiscountRate = math.LegacyNewDecWithPrec(int64(r.Intn(8)+1), 1) // 0.1-0.9
		params.BridgeInactivityThreshold = uint64(r.Intn(200) + 50)
		params.MaxPrunePerBlock = uint64(r.Intn(200) + 50)

		if err := k.Params.Set(ctx, params); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateOperationalParams{}), "failed to set params"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateOperationalParams{}), "ok (direct keeper call)"), nil, nil
	}
}
