package app

import (
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	commonsmoduletypes "sparkdream/x/commons/types"
)

// policyGatedMessages lists every message type some keeper accepts from a
// council or committee POLICY address (an executed x/commons proposal:
// IsCouncilOrCommitteePolicy, IsCouncilBodyPolicy, federation's
// isCouncilPolicy, service's assertCommitteePolicy), keyed by the body the
// gate names. The keeper check is only half of the grant: x/commons refuses
// a proposal whose message is not in the policy's AllowedMessages, so a
// gated message missing from the body's bootstrap allowlist is unexecutable
// by that body (SVC-201, FED-203, the MsgResumePeer deadlock). Each entry
// cites the gate; add one whenever a keeper starts accepting a policy.
var policyGatedMessages = map[string][]string{
	"Commons Council": {
		"/sparkdream.service.v1.MsgResolveReportByJury",   // service assertCommitteePolicy (REDUCE / dissolve)
		"/sparkdream.federation.v1.MsgResumePeer",         // federation ResumePeer
		"/sparkdream.federation.v1.MsgRemovePeer",         // federation isCouncilPolicy
		"/sparkdream.federation.v1.MsgRevokeIdentityLink", // federation isCouncilPolicy
	},
	"Commons Operations Committee": {
		"/sparkdream.service.v1.MsgResolveReportByJury",          // service assertCommitteePolicy (REDUCE / dissolve)
		"/sparkdream.federation.v1.MsgResumePeer",                // federation ResumePeer
		"/sparkdream.federation.v1.MsgRemovePeer",                // federation isCouncilPolicy
		"/sparkdream.federation.v1.MsgSuspendPeer",               // spec §6.2 bundle with MsgRemovePeer
		"/sparkdream.federation.v1.MsgModerateContent",           // federation isCouncilPolicy (reject / restore)
		"/sparkdream.federation.v1.MsgRevokeIdentityLink",        // federation isCouncilPolicy
		"/sparkdream.federation.v1.MsgResolveEscalatedChallenge", // federation isCouncilPolicy
		"/sparkdream.federation.v1.MsgUpdateOperationalParams",   // federation isCouncilPolicy
		"/sparkdream.session.v1.MsgUpdateOperationalParams",      // session isCouncilPolicy
		"/sparkdream.blog.v1.MsgUpdateOperationalParams",         // blog IsCouncilOrCommitteePolicy
		"/sparkdream.collect.v1.MsgUpdateOperationalParams",      // collect IsCouncilOrCommitteePolicy
		"/sparkdream.collect.v1.MsgResolveCurationChallenge",     // collect IsCouncilOrCommitteePolicy
		"/sparkdream.forum.v1.MsgUpdateOperationalParams",        // forum IsCouncilOrCommitteePolicy
		"/sparkdream.futarchy.v1.MsgUpdateOperationalParams",     // futarchy IsCouncilOrCommitteePolicy
		"/sparkdream.name.v1.MsgUpdateOperationalParams",         // name IsCouncilOrCommitteePolicy
		"/sparkdream.rep.v1.MsgUpdateOperationalParams",          // rep IsCouncilOrCommitteePolicy
		"/sparkdream.rep.v1.MsgResolveMemberReport",              // rep IsCouncilOrCommitteePolicy
		"/sparkdream.rep.v1.MsgResolveGovActionAppeal",           // rep IsCouncilOrCommitteePolicy
		"/sparkdream.rep.v1.MsgApproveInterim",                   // rep IsCouncilOrCommitteePolicy
		"/sparkdream.rep.v1.MsgCompleteInterim",                  // rep IsCouncilOrCommitteePolicy
		"/sparkdream.rep.v1.MsgApproveProjectBudget",             // rep IsCouncilOrCommitteePolicy
		"/sparkdream.rep.v1.MsgCancelProject",                    // rep IsCouncilOrCommitteePolicy
		"/sparkdream.rep.v1.MsgCompleteProject",                  // rep IsCouncilOrCommitteePolicy
		"/sparkdream.season.v1.MsgUpdateOperationalParams",       // season IsCouncilOrCommitteePolicy
		"/sparkdream.season.v1.MsgAbortSeasonTransition",         // season IsCouncilOrCommitteePolicy
		"/sparkdream.season.v1.MsgSkipTransitionPhase",           // season IsCouncilOrCommitteePolicy
		"/sparkdream.season.v1.MsgRetrySeasonTransition",         // season IsCouncilOrCommitteePolicy
		"/sparkdream.season.v1.MsgExtendSeason",                  // season IsCouncilOrCommitteePolicy
		"/sparkdream.season.v1.MsgSetNextSeasonInfo",             // season IsCouncilOrCommitteePolicy
		"/sparkdream.season.v1.MsgResolveDisplayNameAppeal",      // season IsCouncilOrCommitteePolicy
		"/sparkdream.season.v1.MsgResolveUnappealedModeration",   // season IsCouncilOrCommitteePolicy
		"/sparkdream.artifact.v1.MsgUpdateOperationalParams",     // artifact IsCouncilAuthorized / IsCouncilBodyPolicy
		"/sparkdream.artifact.v1.MsgCreateClass",                 // artifact IsCouncilBodyPolicy
	},
	"Commons Governance Committee": {
		"/sparkdream.rep.v1.MsgForceMigration",  // rep IsCouncilOrCommitteePolicy(commons, hr)
		"/sparkdream.rep.v1.MsgCancelMigration", // rep IsCouncilOrCommitteePolicy(commons, hr)
	},
}

// TestPolicyGatedMessagesAreAllowlisted boots the app from its default
// genesis (which runs x/commons BootstrapGovernance) and checks every
// policy-gated message against the live PolicyPermissions of the body its
// keeper gate names, rather than the bootstrap source text.
func TestPolicyGatedMessagesAreAllowlisted(t *testing.T) {
	app, ctx := newAppFromDefaultGenesis(t)
	var bodies []string
	require.NoError(t, app.CommonsKeeper.Groups.Walk(ctx, nil, func(name string, _ commonsmoduletypes.Group) (bool, error) {
		bodies = append(bodies, name)
		return false, nil
	}))
	for body, msgs := range policyGatedMessages {
		group, err := app.CommonsKeeper.Groups.Get(ctx, body)
		require.NoError(t, err, "%s is a genesis body (bootstrapped: %v)", body, bodies)
		perms, err := app.CommonsKeeper.GetPolicyPermissions(ctx, group.PolicyAddress)
		require.NoError(t, err, "%s has policy permissions", body)
		allowed := map[string]bool{}
		for _, m := range perms.AllowedMessages {
			allowed[m] = true
		}
		for _, m := range msgs {
			require.True(t, allowed[m],
				"%s is gated on the %s policy in its keeper but is not in that body's AllowedMessages, "+
					"so a passed proposal cannot execute it (x/commons/keeper/genesis_bootstrap.go)", m, body)
		}
	}
}

// newAppFromDefaultGenesis runs InitChain with the app's default genesis
// plus the one validator InitGenesis requires, and returns a context over
// the resulting state. No e2e snapshot is involved, so the test is never
// stale. x/commons bootstraps the governance bodies when its genesis has no
// groups, which the default genesis does not.
func newAppFromDefaultGenesis(t *testing.T) (*App, sdk.Context) {
	t.Helper()
	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = t.TempDir()
	const chainID = "sparkdream-guard-1"
	app := New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(chainID))

	valSet, err := simtestutil.CreateRandomValidatorSet()
	require.NoError(t, err)
	acc := authtypes.NewBaseAccount(sdk.AccAddress(valSet.Validators[0].Address), nil, 0, 0)
	balance := banktypes.Balance{
		Address: acc.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.DefaultPowerReduction.MulRaw(10))),
	}
	genesisState, err := simtestutil.GenesisStateWithValSet(app.AppCodec(), app.DefaultGenesis(), valSet, []authtypes.GenesisAccount{acc}, balance)
	require.NoError(t, err)

	// The bootstrap needs a founder among the genesis accounts; the
	// validator account stands in (founding_members overrides the per-build
	// GenesisNames, which name accounts this genesis does not hold).
	var commonsGen commonsmoduletypes.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesisState[commonsmoduletypes.ModuleName], &commonsGen)
	commonsGen.FoundingMembers = []commonsmoduletypes.FoundingMember{{Address: acc.GetAddress().String(), DisplayName: "phoenix", Founder: true}}
	genesisState[commonsmoduletypes.ModuleName] = app.AppCodec().MustMarshalJSON(&commonsGen)

	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	genesisTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = app.InitChain(&abci.RequestInitChain{
		ChainId: chainID, Time: genesisTime, AppStateBytes: stateBytes, InitialHeight: 1,
	})
	require.NoError(t, err)
	ctx := app.NewContextLegacy(false, cmtproto.Header{Height: 1, ChainID: chainID, Time: genesisTime})
	return app, ctx
}
