package keeper_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// gasRouter routes every message to a handler that burns a known amount of
// gas on the context it is given.
type gasRouter struct{ burn uint64 }

func (r gasRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler { return r.handle }

func (r gasRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return r.handle }

func (r gasRouter) handle(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
	ctx.GasMeter().ConsumeGas(r.burn, "inner work")
	return &sdk.Result{}, nil
}

// TestInnerMessageGasChargedToOuterTx: the inner handler runs under a
// per-exec cap, and what it uses is charged to the outer tx's meter, so the
// fee and the block gas accounting cover the inner work (review finding M4).
func TestInnerMessageGasChargedToOuterTx(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetRouter(gasRouter{burn: 123_456})
	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &ownedModule{})
	inner := innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress(types.ModuleName).String()})

	params := types.DefaultParams()
	params.MaxGasPerExec = 500_000
	ctx := sdk.UnwrapSDKContext(f.ctx).WithGasMeter(storetypes.NewGasMeter(10_000_000))
	before := ctx.GasMeter().GasConsumed()
	_, err := f.keeper.ExecuteInnerMessage(ctx, params, inner)
	require.NoError(t, err)
	require.GreaterOrEqual(t, ctx.GasMeter().GasConsumed()-before, uint64(123_456), "inner gas not charged to the outer tx")

	// Over the per-exec cap the inner handler runs out of gas: the exec fails
	// instead of running an arbitrarily expensive inner message.
	f.keeper.SetRouter(gasRouter{burn: 600_000})
	require.Panics(t, func() {
		_, _ = f.keeper.ExecuteInnerMessage(ctx, params, inner)
	})
}
