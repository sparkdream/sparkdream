package keeper_test

import (
	"math/rand"
	"testing"

	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	namesim "sparkdream/x/name/simulation"
)

// NAM-07: the simulation covers every name message. The operations deliver
// real transactions (exercised by the full-app simulation); without an app
// to deliver to they are a NoOp, never a failure.
func TestNameSimulationOperationsRun(t *testing.T) {
	f := initFixture(t)
	r := rand.New(rand.NewSource(7))
	accs := simtypes.RandomAccounts(r, 3)
	for name, op := range map[string]simtypes.Operation{
		"set_display_name": namesim.SimulateMsgSetDisplayName(nil, nil, f.keeper, nil),
		"set_target":       namesim.SimulateMsgSetTarget(nil, nil, f.keeper, nil),
		"transfer_name":    namesim.SimulateMsgTransferName(nil, nil, f.keeper, nil),
		"contest_dispute":  namesim.SimulateMsgContestDispute(nil, nil, f.keeper, nil),
	} {
		for i := 0; i < 5; i++ {
			msg, _, err := op(r, nil, f.ctx.WithBlockHeight(int64(10+i)), accs, "sim")
			require.NoError(t, err, name)
			require.NotEmpty(t, msg.Route, name)
			require.False(t, msg.OK, name)
		}
	}
}
