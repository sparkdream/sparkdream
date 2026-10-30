package keeper_test

import (
	"context"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// recordingAppealTarget is a module-owned ModerationAppealTarget (the role
// x/collect and x/artifact play) that also implements the optional outcome
// hook.
type recordingAppealTarget struct {
	sentinel  string
	committed sdkmath.Int
	reversed  []string
	resolved  []string
	outcomes  []types.GovAppealStatus
}

func (r *recordingAppealTarget) GetActionSentinel(_ context.Context, _ types.GovActionType, _ string) (string, error) {
	return r.sentinel, nil
}

func (r *recordingAppealTarget) GetActionCommittedAmount(_ context.Context, _ types.GovActionType, _ string) (sdkmath.Int, error) {
	return r.committed, nil
}

func (r *recordingAppealTarget) OnSentinelActionResolved(_ context.Context, _ types.GovActionType, target string) error {
	r.resolved = append(r.resolved, target)
	return nil
}

func (r *recordingAppealTarget) ReverseSentinelAction(_ context.Context, _ types.GovActionType, target string) error {
	r.reversed = append(r.reversed, target)
	return nil
}

func (r *recordingAppealTarget) OnAppealOutcome(_ context.Context, _ types.GovActionType, _ string, outcome types.GovAppealStatus) error {
	r.outcomes = append(r.outcomes, outcome)
	return nil
}

func setupRegistry(t *testing.T) (*fixture, types.MsgServer, *mockForumKeeper, *recordingAppealTarget, sdk.AccAddress) {
	t.Helper()
	f := initFixture(t)
	f.commonsKeeper.IsCouncilAuthorizedFn = func(context.Context, string, string, string) bool { return true }
	fk := &mockForumKeeper{actionSentinels: map[string]string{}}
	f.keeper.SetForumKeeper(fk)
	target := &recordingAppealTarget{committed: sdkmath.ZeroInt()}
	f.keeper.RegisterModerationAppealTarget(types.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, target)
	appellant := sdk.AccAddress([]byte("appellant_regist_"))
	setActiveMember(t, f.keeper, f.ctx, appellant)
	return f, keeper.NewMsgServerImpl(f.keeper), fk, target, appellant
}

func TestModuleAppealVerdictsReachRegisteredTarget(t *testing.T) {
	for _, tc := range []struct {
		verdict  types.GovAppealStatus
		reversed bool
	}{
		{types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED, true},
		{types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD, false},
	} {
		t.Run(tc.verdict.String(), func(t *testing.T) {
			f, ms, fk, target, appellant := setupRegistry(t)
			appealID, _, err := f.keeper.CreateGovActionAppeal(f.ctx, types.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, "7", appellant, "restore my token")
			require.NoError(t, err)

			resolver := sdk.AccAddress([]byte("ops_resolver______")).String()
			_, err = ms.ResolveGovActionAppeal(f.ctx, &types.MsgResolveGovActionAppeal{
				Resolver: resolver, AppealId: appealID, Verdict: tc.verdict, Reason: "test",
			})
			require.NoError(t, err)

			require.Equal(t, []types.GovAppealStatus{tc.verdict}, target.outcomes)
			require.Equal(t, []string{"7"}, target.resolved)
			if tc.reversed {
				require.Equal(t, []string{"7"}, target.reversed)
			} else {
				require.Empty(t, target.reversed)
			}
			// The forum keeper is never consulted for a module-owned type.
			require.Empty(t, fk.resolvedCalls)
			require.Empty(t, fk.reverseCalls)
		})
	}
}

func TestModuleAppealTimeoutNotifiesTarget(t *testing.T) {
	f, _, _, target, appellant := setupRegistry(t)
	_, _, err := f.keeper.CreateGovActionAppeal(f.ctx, types.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, "9", appellant, "x")
	require.NoError(t, err)
	future := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(types.DefaultAppealDeadline+10) * time.Second))
	require.NoError(t, f.keeper.TimeoutExpiredAppeals(future))
	require.Equal(t, []types.GovAppealStatus{types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT}, target.outcomes)
	require.Empty(t, target.reversed, "a timeout blames no one and reverses nothing in rep")
}

func TestModuleOwnedAppealTypesRejectedFromMsgAppealGovAction(t *testing.T) {
	f, ms, _, _, appellant := setupRegistry(t)
	appellantStr, err := f.addressCodec.BytesToString(appellant)
	require.NoError(t, err)
	// FRM-02: forum's lock / move / pin are filed through x/forum too.
	for _, at := range []types.GovActionType{types.GovActionType_GOV_ACTION_TYPE_POST_HIDE, types.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE, types.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE,
		types.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK, types.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE, types.GovActionType_GOV_ACTION_TYPE_REPLY_PIN} {
		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator: appellantStr, ActionType: uint64(at), ActionTarget: "1", AppealReason: "bypass",
		})
		require.ErrorContains(t, err, "owning module", at.String())
	}
}

func TestUnregisteredModuleTypeDoesNotFallBackToForum(t *testing.T) {
	f, ms, fk, _, appellant := setupRegistry(t)
	// COLLECT_HIDE has no registration in this fixture.
	appealID, _, err := f.keeper.CreateGovActionAppeal(f.ctx, types.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE, "3", appellant, "x")
	require.NoError(t, err)
	_, err = ms.ResolveGovActionAppeal(f.ctx, &types.MsgResolveGovActionAppeal{
		Resolver: sdk.AccAddress([]byte("ops_resolver______")).String(), AppealId: appealID,
		Verdict: types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED, Reason: "test",
	})
	require.NoError(t, err)
	require.Empty(t, fk.reverseCalls, "a collect hide must never be reversed as a forum post")
}

func TestActionKindForModuleHides(t *testing.T) {
	require.Equal(t, types.ActionKindCollectHide, types.ActionKindForGovAction(types.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE))
	require.Equal(t, types.ActionKindArtifactHide, types.ActionKindForGovAction(types.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE))
}

// curatorAppealTarget is a target whose actions are taken by a collect
// curator rather than a content sentinel.
type curatorAppealTarget struct{ recordingAppealTarget }

func (c *curatorAppealTarget) AppealRoleType(types.GovActionType) types.RoleType {
	return types.RoleType_ROLE_TYPE_COLLECT_CURATOR
}

// Part 6 handoff: the verdict slashes the role the target names, not a
// hardcoded content sentinel.
func TestAppealVerdictUsesTargetRole(t *testing.T) {
	f, ms, _, _, appellant := setupRegistry(t)
	curator := sdk.AccAddress([]byte("appeal_curator____")).String()
	bond := sdkmath.NewInt(5_000_000)
	seedSlashableSentinel(t, f, curator, bond) // member record with locked DREAM
	require.NoError(t, f.keeper.BondedRoles.Set(f.ctx, bondedRoleKey(types.RoleType_ROLE_TYPE_COLLECT_CURATOR, curator), types.BondedRole{
		Address: curator, RoleType: types.RoleType_ROLE_TYPE_COLLECT_CURATOR, CurrentBond: bond.String(),
		TotalCommittedBond: "1000000", BondStatus: types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
	}))
	target := &curatorAppealTarget{recordingAppealTarget{sentinel: curator, committed: sdkmath.NewInt(1_000_000)}}
	f.keeper.RegisterModerationAppealTarget(types.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE, target)

	appealID, _, err := f.keeper.CreateGovActionAppeal(f.ctx, types.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE, "5", appellant, "x")
	require.NoError(t, err)
	_, err = ms.ResolveGovActionAppeal(f.ctx, &types.MsgResolveGovActionAppeal{
		Resolver: sdk.AccAddress([]byte("ops_resolver______")).String(), AppealId: appealID,
		Verdict: types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED, Reason: "test",
	})
	require.NoError(t, err)
	br, err := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_COLLECT_CURATOR, curator)
	require.NoError(t, err)
	require.Equal(t, "4000000", br.CurrentBond, "the curator's bond was slashed")
	sentinelRole, err := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_CONTENT_SENTINEL, curator)
	require.NoError(t, err)
	require.Equal(t, bond.String(), sentinelRole.CurrentBond, "the sentinel role is untouched")
}
