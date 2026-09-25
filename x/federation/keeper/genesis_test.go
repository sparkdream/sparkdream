package keeper_test

import (
	"testing"

	"sparkdream/x/federation/types"
	identitytypes "sparkdream/x/identity/types"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, genesisState.PortId, got.PortId)
	require.EqualExportedValues(t, genesisState.Params, got.Params)
}

func TestGenesisRoundTripsOperatorRewardDayFunding(t *testing.T) {
	// The per-UTC-day draw ledger bounds how much the operator reward pool can
	// pull from the community pool in a day. It lives only in module state, so
	// leaving it out of genesis let a mid-day export/import hand the chain a
	// fresh allowance and take the same day's draw twice. x/rep ledgers its
	// role-reward equivalent the same way.
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
		OperatorRewardDayFundingList: []types.OperatorRewardDayFunding{
			{Day: 20120, AmountFunded: math.NewInt(1_500_000)},
			{Day: 20121, AmountFunded: math.NewInt(2_750_000)},
		},
	}
	require.NoError(t, genesisState.Validate())

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, genesisState))

	// The imported ledger is what the daily cap reads, not a zeroed one.
	require.Equal(t, math.NewInt(1_500_000), f.keeper.GetOperatorRewardDayFunding(f.ctx, 20120))
	require.Equal(t, math.NewInt(2_750_000), f.keeper.GetOperatorRewardDayFunding(f.ctx, 20121))

	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, genesisState.OperatorRewardDayFundingList, got.OperatorRewardDayFundingList)
}

func TestGenesisRejectsDuplicateOperatorRewardDay(t *testing.T) {
	// A duplicate day collapses silently on import, under-reporting the day's
	// draw and handing back part of an allowance already spent.
	gs := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
		OperatorRewardDayFundingList: []types.OperatorRewardDayFunding{
			{Day: 20120, AmountFunded: math.NewInt(1_000_000)},
			{Day: 20120, AmountFunded: math.NewInt(2_000_000)},
		},
	}
	require.ErrorContains(t, gs.Validate(), "duplicated operator reward day funding")

	gs.OperatorRewardDayFundingList = []types.OperatorRewardDayFunding{
		{Day: 20120, AmountFunded: math.NewInt(-1)},
	}
	require.ErrorContains(t, gs.Validate(), "must be non-negative")
}

// The verification-lifecycle queues carry deadlines that live nowhere
// else — ArbiterResolutionQueue and ArbiterEscalationQueue especially.
// Dropping them on an export/import upgrade strands every in-flight
// dispute: the content sits DISPUTED or CHALLENGED until content_ttl and
// the verifier's committed bond is never released, because the EndBlocker
// walk that would release it has no entry to walk.
func TestGenesisRoundTripsVerificationLifecycleQueues(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
		VerificationWindowQueue: []types.ContentDeadline{
			{Deadline: 1_700_000_100, ContentId: 1},
		},
		ChallengeWindowQueue: []types.ContentDeadline{
			{Deadline: 1_700_000_200, ContentId: 2},
		},
		ArbiterResolutionQueue: []types.ContentDeadline{
			{Deadline: 1_700_000_300, ContentId: 3},
			{Deadline: 1_700_000_400, ContentId: 4},
		},
		ArbiterEscalationQueue: []types.ContentDeadline{
			{Deadline: 1_700_000_500, ContentId: 5},
		},
		EscalatedChallengeDeadlineQueue: []types.ContentDeadline{
			{Deadline: 1_700_000_600, ContentId: 6},
		},
		EscalatedChallenges: []types.EscalatedChallenge{{
			ContentId:                   6,
			Escalator:                   "sprkdrm1escalator",
			EscrowedEscalationFee:       math.NewInt(5_000_000),
			AutoVerdictBeforeEscalation: types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT,
			JuryDeadline:                1_700_000_600,
		}},
		ArbiterSubmissions: []types.ArbiterSubmissionEntry{{
			SubmitterKey: "sprkdrm1arbiter",
			Submission: types.ArbiterHashSubmission{
				ContentId: 3, ContentHash: []byte{0xaa, 0xbb}, SubmittedAt: 1_700_000_050,
				Operator: "sprkdrm1arbiter",
			},
		}, {
			SubmitterKey: "anon:7",
			Submission: types.ArbiterHashSubmission{
				ContentId: 3, ContentHash: []byte{0xaa, 0xbb}, SubmittedAt: 1_700_000_060,
				Operator: "sprkdrm1shieldmodule",
			},
		}},
		ArbiterHashCounts: []types.ArbiterHashCount{
			{ContentId: 3, ContentHash: "aabb", Count: 2},
		},
		NextArbiterAnonSubmissionId: 8,
	}

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, genesisState))

	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)

	require.ElementsMatch(t, genesisState.VerificationWindowQueue, got.VerificationWindowQueue)
	require.ElementsMatch(t, genesisState.ChallengeWindowQueue, got.ChallengeWindowQueue)
	require.ElementsMatch(t, genesisState.ArbiterResolutionQueue, got.ArbiterResolutionQueue)
	require.ElementsMatch(t, genesisState.ArbiterEscalationQueue, got.ArbiterEscalationQueue)
	require.ElementsMatch(t, genesisState.EscalatedChallengeDeadlineQueue, got.EscalatedChallengeDeadlineQueue)
	require.ElementsMatch(t, genesisState.EscalatedChallenges, got.EscalatedChallenges)
	require.ElementsMatch(t, genesisState.ArbiterHashCounts, got.ArbiterHashCounts)
	require.Equal(t, genesisState.NextArbiterAnonSubmissionId, got.NextArbiterAnonSubmissionId)

	// The submitter key is part of the STATE key, not of the value, so it
	// must survive the round trip or the "no double vote per operator"
	// rule silently stops holding after an upgrade.
	require.ElementsMatch(t, genesisState.ArbiterSubmissions, got.ArbiterSubmissions)
}

// This session's state shapes survive export and import: a policy's
// content_hosts, and a superseded pair (the retired record's SUPERSEDED
// status and superseded_by, the successor's supersedes ref -- including a
// ref to content id 0, which is why supersedes is a message, not a uint64).
func TestGenesisRoundTripsSupersedeAndContentHosts(t *testing.T) {
	uri := "https://phoenix.example/users/a/statuses/1"
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
		PeerPolicies: []types.PeerPolicy{{
			PeerId:              "phoenix.example",
			InboundContentTypes: []string{"blog_post"},
			ContentHosts:        []string{"social.phoenix.example"},
		}},
		FederatedContent: []types.FederatedContent{
			{
				Id: 0, PeerId: "phoenix.example", ContentType: "blog_post", ContentUri: uri,
				SubmittedBy: "sprkdrm1operator", ContentHash: []byte{0x01},
				Status:       types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_SUPERSEDED,
				SupersededBy: 1, ExpiresAt: 1_700_000_900,
			},
			{
				Id: 1, PeerId: "phoenix.example", ContentType: "blog_post", ContentUri: uri,
				SubmittedBy: "sprkdrm1operator", ContentHash: []byte{0x02},
				Status:     types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION,
				Supersedes: &types.ContentRef{ContentId: 0}, ExpiresAt: 1_700_000_900,
			},
		},
	}

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, genesisState))
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)

	require.ElementsMatch(t, genesisState.FederatedContent, got.FederatedContent)
	require.Len(t, got.PeerPolicies, 1)
	require.Equal(t, []string{"social.phoenix.example"}, got.PeerPolicies[0].ContentHosts)
	for _, c := range got.FederatedContent {
		if c.Id == 1 {
			require.NotNil(t, c.Supersedes, "a supersedes ref to content id 0 must survive export")
			require.Equal(t, uint64(0), c.Supersedes.ContentId)
		}
	}
}

// A peer's transfer channel and identity are what voucher metadata is keyed
// on (MsgRegisterPeer, x-identity-spec §9.2); they must survive an
// export/import, or a relaunched chain renders the peer's SPARK as ibc/<hash>
// on the next re-registration check.
func TestGenesisRoundTripsPeerTransferChannel(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
		Peers: []types.Peer{{
			Id: "phoenix-1", DisplayName: "Phoenix", Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
			Status: types.PeerStatus_PEER_STATUS_ACTIVE, IbcChannelId: "channel-1",
			IbcTransferChannelId: "channel-0",
			PeerIdentity: &identitytypes.ChainIdentity{
				BondDenom: "uspk.phoenix", BondDisplaySymbol: "PSPK", BondDisplayName: "Phoenix Spark", BondDisplayDecimals: 6,
			},
		}},
	}

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, genesisState))
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, got.Peers, 1)
	require.Equal(t, "channel-0", got.Peers[0].IbcTransferChannelId)
	require.Equal(t, "channel-1", got.Peers[0].IbcChannelId)
	require.Equal(t, "PSPK", got.Peers[0].PeerIdentity.GetBondDisplaySymbol())
}
