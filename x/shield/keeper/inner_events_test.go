package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// eventRouter routes every message to a handler that, like the SDK's
// MsgServiceRouter, runs on a fresh event manager and returns its events in
// the result.
type eventRouter struct{}

func (eventRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler { return eventHandler }

func (eventRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return eventHandler }

func eventHandler(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	ctx.EventManager().EmitEvent(sdk.NewEvent("collection_created", sdk.NewAttribute("id", "7")))
	return &sdk.Result{Events: ctx.EventManager().ABCIEvents()}, nil
}

// The inner message's events reach the exec's event manager, so clients can
// read what an anonymous action created (and what it spent) from the tx.
func TestExecuteInnerMessageEmitsInnerEvents(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetRouter(eventRouter{})
	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &ownedModule{})

	inner := innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress(types.ModuleName).String()})
	ctx := sdk.UnwrapSDKContext(f.ctx).WithEventManager(sdk.NewEventManager())
	_, err := f.keeper.ExecuteInnerMessage(ctx, types.DefaultParams(), inner)
	require.NoError(t, err)

	var found bool
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == "collection_created" {
			found = true
		}
	}
	require.True(t, found, "inner message events were dropped")
}
