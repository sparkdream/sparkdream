package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgResolveCurationChallenge is not simulated: only the gov
// authority, the Commons Council policy or the Commons Operations Committee
// policy (an executed proposal) may sign it. No sim account holds such an
// address and the simulation never registers a sim account as a policy
// (x/rep and x/vault refuse policy accounts as persons). Pending challenges
// are settled in the simulation by the EndBlocker's not-adjudicated timeout.
func SimulateMsgResolveCurationChallenge(
	_ types.AuthKeeper,
	_ types.BankKeeper,
	_ keeper.Keeper,
	_ client.TxConfig,
) simtypes.Operation {
	return func(_ *rand.Rand, _ *baseapp.BaseApp, _ sdk.Context, _ []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgResolveCurationChallenge{}, "signer must be a gov/council/committee policy address no sim account holds")
	}
}
