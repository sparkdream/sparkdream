package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgRegisterZkPublicKey(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		member, simAccount, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterZkPublicKey{}), "failed to get/create member"), nil, nil
		}

		// ZK key change discipline (member key migration, plan section 9): a
		// change is refused while another is waiting for activation, and at
		// most one change lands per season unless a migration cleared the key.
		if len(member.PendingZkPublicKey) > 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterZkPublicKey{}), "zk key change pending activation"), nil, nil
		}
		if len(member.ZkPublicKey) > 0 && member.ZkKeyChanged && !member.ZkKeyCooldownExempt {
			if season, err := k.GetCurrentSeason(ctx); err != nil || member.LastZkKeyChangeSeason == season {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterZkPublicKey{}), "zk key already changed this season"), nil, nil
			}
		}

		// Generate a random 32-byte ZK public key. The handler rejects values
		// at or above the BN254 scalar modulus (just under 2^254), so clear the
		// top three bits: anything below 2^253 is canonical.
		zkPubKey := make([]byte, 32)
		r.Read(zkPubKey)
		zkPubKey[0] &= 0x1f

		msg := &types.MsgRegisterZkPublicKey{
			Member:      member.Address,
			ZkPublicKey: zkPubKey,
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
