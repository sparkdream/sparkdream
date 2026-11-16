package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// A session key carries the member's personal standing, never a committee
// seat: under MsgExecSession the Operations Committee path of the delegable
// initiative handlers is refused, while the same message signed by the
// member's own key passes, and the project creator's standing is unaffected.
func TestSessionKeyHasNoOpsStanding(t *testing.T) {
	ops := sdk.AccAddress([]byte("ops-member"))
	creator := sdk.AccAddress([]byte("creator"))

	setup := func(t *testing.T) (*fixture, types.MsgServer, uint64, string, string, sdk.Context) {
		f := initFixture(t, WithAuthorizationPolicy(AuthorizeAddresses(cancelTestApprover, ops)))
		_, initID := setupCancellableInitiative(t, f.keeper, f.ctx, creator, math.NewInt(100))
		opsStr, err := f.addressCodec.BytesToString(ops)
		require.NoError(t, err)
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)
		// What x/session sets on the context it dispatches inner messages with.
		viaSession := f.ctx.WithValue(commontypes.ContextKeySessionExec, opsStr)
		return f, keeper.NewMsgServerImpl(f.keeper), initID, opsStr, creatorStr, viaSession
	}

	t.Run("close", func(t *testing.T) {
		f, ms, initID, opsStr, _, viaSession := setup(t)
		_, err := ms.CloseInitiative(viaSession, &types.MsgCloseInitiative{Creator: opsStr, InitiativeId: initID, Reason: "stale"})
		require.ErrorIs(t, err, types.ErrUnauthorized)
		_, err = ms.CloseInitiative(f.ctx, &types.MsgCloseInitiative{Creator: opsStr, InitiativeId: initID, Reason: "stale"})
		require.NoError(t, err)
	})

	t.Run("close by the project creator's session key", func(t *testing.T) {
		f, ms, initID, _, creatorStr, _ := setup(t)
		viaSession := f.ctx.WithValue(commontypes.ContextKeySessionExec, creatorStr)
		_, err := ms.CloseInitiative(viaSession, &types.MsgCloseInitiative{Creator: creatorStr, InitiativeId: initID, Reason: "done"})
		require.NoError(t, err)
	})

	t.Run("assign somebody else", func(t *testing.T) {
		_, ms, initID, opsStr, creatorStr, viaSession := setup(t)
		_, err := ms.AssignInitiative(viaSession, &types.MsgAssignInitiative{Creator: opsStr, InitiativeId: initID, Assignee: creatorStr})
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("force unassign", func(t *testing.T) {
		_, ms, initID, opsStr, _, viaSession := setup(t)
		_, err := ms.UnassignInitiative(viaSession, &types.MsgUnassignInitiative{Creator: opsStr, InitiativeId: initID, Reason: "stalled"})
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("complete", func(t *testing.T) {
		_, ms, initID, opsStr, _, viaSession := setup(t)
		_, err := ms.CompleteInitiative(viaSession, &types.MsgCompleteInitiative{Creator: opsStr, InitiativeId: initID})
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})
}
