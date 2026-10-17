package keeper_test

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/util/random"

	"sparkdream/tools/tle"
	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// realContribution builds a dealer's Feldman contribution from a fresh random
// polynomial of the given degree bound (G1 and G2 commitments to the same
// coefficients) and returns it with its constant term.
func realContribution(t *testing.T, dealer string, threshold int) (types.DKGContribution, kyber.Scalar) {
	t.Helper()
	coeffs := make([]kyber.Scalar, threshold)
	c := types.DKGContribution{ValidatorAddress: dealer, Round: 1}
	for i := range coeffs {
		coeffs[i] = tle.Suite.G1().Scalar().Pick(random.New())
		g1, err := tle.Suite.G1().Point().Mul(coeffs[i], nil).MarshalBinary()
		require.NoError(t, err)
		g2, err := tle.Suite.G2().Point().Mul(coeffs[i], nil).MarshalBinary()
		require.NoError(t, err)
		c.FeldmanCommitments = append(c.FeldmanCommitments, g1)
		c.FeldmanCommitmentsG2 = append(c.FeldmanCommitmentsG2, g2)
	}
	return c, coeffs[0]
}

// evalAt is p(j) for the polynomial behind a contribution built by
// realContribution, recomputed from its coefficients.
func evalAt(coeffs []kyber.Scalar, j uint32) kyber.Scalar {
	x := tle.Suite.G1().Scalar().SetInt64(int64(j))
	r := tle.Suite.G1().Scalar().Zero()
	for i := len(coeffs) - 1; i >= 0; i-- {
		r = r.Mul(r, x)
		r = r.Add(r, coeffs[i])
	}
	return r
}

// TestDKGComplaintPhases drives a round from CONTRIBUTING through
// COMPLAINING and JUSTIFYING to ACTIVE: a dealer that leaves a complaint
// unanswered is disqualified and its secret is left out of the master key;
// a dealer that answers with a valid evaluation stays in.
func TestDKGComplaintPhases(t *testing.T) {
	f := initFixture(t)
	// enable_batch_after_dkg off (the production default; the testparams
	// genesis turns it on): completion must leave batch mode off.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EnableBatchAfterDkg = false
	params.EncryptedBatchEnabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	vals := []string{"val1", "val2", "val3"}
	state := types.DKGState{
		Round:                 1,
		Phase:                 types.DKGPhase_DKG_PHASE_CONTRIBUTING,
		ContributionDeadline:  20,
		ComplaintDeadline:     30,
		JustificationDeadline: 40,
		ThresholdNumerator:    2,
		ThresholdDenominator:  3,
		ExpectedValidators:    vals,
	}
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, state))
	a0 := map[string]kyber.Scalar{}
	for _, v := range vals {
		c, s := realContribution(t, v, 2)
		a0[v] = s
		require.NoError(t, f.keeper.SetDKGContributionVal(f.ctx, c))
	}
	state.ContributionsReceived = 3
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, state))

	// All contributions in: the ceremony moves to COMPLAINING at once, but
	// the complaint window keeps its original schedule.
	require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(5)))
	got, _ := f.keeper.GetDKGStateVal(f.ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_COMPLAINING, got.Phase)

	// val1 complains about val3 (bad evaluation) and about val2 (which will
	// answer). Before the complaint deadline nothing moves.
	require.NoError(t, f.keeper.SetDKGComplaint(f.ctx, types.DKGComplaint{DealerAddress: "val3", ComplainerAddress: "val1"}))
	require.NoError(t, f.keeper.SetDKGComplaint(f.ctx, types.DKGComplaint{DealerAddress: "val2", ComplainerAddress: "val1"}))
	require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(29)))
	got, _ = f.keeper.GetDKGStateVal(f.ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_COMPLAINING, got.Phase)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(30)))
	got, _ = f.keeper.GetDKGStateVal(f.ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_JUSTIFYING, got.Phase)
	require.Len(t, f.keeper.ComplaintsAgainst(f.ctx, "val3"), 1)

	// val2 justifies (as the PreBlocker records a verified answer); val3 stays silent.
	c2, _ := f.keeper.GetDKGComplaint(f.ctx, "val2", "val1")
	c2.Justified = true
	require.NoError(t, f.keeper.SetDKGComplaint(f.ctx, c2))

	require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(39)))
	got, _ = f.keeper.GetDKGStateVal(f.ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_JUSTIFYING, got.Phase)

	// Liveness record left from a previous key set: completion starts the
	// new key set with a clean one.
	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val1", 4))
	require.NoError(t, f.keeper.TLEJudgedEpochs.Set(f.ctx, 9))

	ctx := f.ctx.WithBlockHeight(40)
	require.NoError(t, f.keeper.BeginBlocker(ctx))
	got, _ = f.keeper.GetDKGStateVal(ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_ACTIVE, got.Phase)
	require.Equal(t, []string{"val3"}, got.DisqualifiedDealers)

	// The key set is the sum of the qualified dealers' constant terms only,
	// with a public share per participant (indices follow expected_validators).
	ks, found := f.keeper.GetTLEKeySetVal(ctx)
	require.True(t, found)
	master := tle.Suite.G1().Scalar().Add(a0["val1"], a0["val2"])
	require.Equal(t, tle.MasterPublicKeyG1(master), ks.MasterPublicKey)
	require.Equal(t, tle.MasterPublicKeyG2(master), ks.MasterPublicKeyG2)
	require.NoError(t, ks.Validate())
	require.Len(t, ks.ValidatorShares, 3)
	require.Len(t, f.keeper.QualifiedDKGContributions(ctx, got), 2)

	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(ctx, "val1"), "liveness reset with the new key set")
	judged, _ := f.keeper.TLEJudgedEpochs.Get(ctx)
	require.Zero(t, judged)

	// Batch mode stayed off: enable_batch_after_dkg is a governance opt-in.
	params, err = f.keeper.Params.Get(ctx)
	require.NoError(t, err)
	require.False(t, params.EncryptedBatchEnabled)
	var disq, complete bool
	for _, ev := range ctx.EventManager().Events() {
		switch ev.Type {
		case types.EventTypeShieldDKGDisqualified:
			disq = true
			for _, a := range ev.Attributes {
				switch a.Key {
				case types.AttributeKeyDealer:
					require.Equal(t, "val3", a.Value)
				case types.AttributeKeyDKGRound:
					require.Equal(t, "1", a.Value)
				}
			}
		case types.EventTypeShieldDKGComplete:
			complete = true
		}
	}
	require.True(t, disq)
	require.True(t, complete)
}

// TestDKGDisqualificationBelowThresholdFails: when disqualifications leave
// fewer qualified dealers than the threshold, the round fails and resets.
func TestDKGDisqualificationBelowThresholdFails(t *testing.T) {
	f := initFixture(t)
	vals := []string{"val1", "val2", "val3"}
	state := types.DKGState{
		Round: 1, Phase: types.DKGPhase_DKG_PHASE_JUSTIFYING, JustificationDeadline: 40,
		ThresholdNumerator: 2, ThresholdDenominator: 3, ExpectedValidators: vals, ContributionsReceived: 3,
	}
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, state))
	for _, v := range vals {
		c, _ := realContribution(t, v, 2)
		require.NoError(t, f.keeper.SetDKGContributionVal(f.ctx, c))
	}
	require.NoError(t, f.keeper.SetDKGComplaint(f.ctx, types.DKGComplaint{DealerAddress: "val2", ComplainerAddress: "val1"}))
	require.NoError(t, f.keeper.SetDKGComplaint(f.ctx, types.DKGComplaint{DealerAddress: "val3", ComplainerAddress: "val1"}))

	require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(40)))
	got, _ := f.keeper.GetDKGStateVal(f.ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_INACTIVE, got.Phase)
	_, found := f.keeper.GetTLEKeySetVal(f.ctx)
	require.False(t, found)
	require.Empty(t, f.keeper.GetAllDKGComplaints(f.ctx), "a failed round clears its complaints")
	require.Empty(t, f.keeper.GetAllDKGContributions(f.ctx))
}

// TestDKGEnableBatchAfterDKGParam: with the opt-in set, a completed DKG
// turns batch mode on.
func TestDKGEnableBatchAfterDKGParam(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EnableBatchAfterDkg = true
	params.EncryptedBatchEnabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	vals := []string{"val1", "val2"}
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
		Round: 1, Phase: types.DKGPhase_DKG_PHASE_JUSTIFYING, JustificationDeadline: 10,
		ThresholdNumerator: 2, ThresholdDenominator: 3, ExpectedValidators: vals, ContributionsReceived: 2,
	}))
	for _, v := range vals {
		c, _ := realContribution(t, v, 2)
		require.NoError(t, f.keeper.SetDKGContributionVal(f.ctx, c))
	}
	require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(10)))
	params, err = f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, params.EncryptedBatchEnabled)
}

// TestVerifyEvaluation pins the local check every validator runs on a dealt
// evaluation and the chain runs on a justification.
func TestVerifyEvaluation(t *testing.T) {
	c, a0 := realContribution(t, "val1", 3)
	coeffs := []kyber.Scalar{a0}
	// Recover the other coefficients is not possible from the contribution;
	// rebuild a polynomial we know instead.
	coeffs = nil
	c2 := types.DKGContribution{ValidatorAddress: "val2"}
	for i := 0; i < 3; i++ {
		s := tle.Suite.G1().Scalar().Pick(random.New())
		coeffs = append(coeffs, s)
		g1, _ := tle.Suite.G1().Point().Mul(s, nil).MarshalBinary()
		c2.FeldmanCommitments = append(c2.FeldmanCommitments, g1)
	}
	for j := uint32(1); j <= 4; j++ {
		bz, err := evalAt(coeffs, j).MarshalBinary()
		require.NoError(t, err)
		require.NoError(t, keeper.VerifyEvaluation(c2.FeldmanCommitments, j, bz))
		require.Error(t, keeper.VerifyEvaluation(c2.FeldmanCommitments, j+1, bz), "wrong index")
		require.Error(t, keeper.VerifyEvaluation(c.FeldmanCommitments, j, bz), "another dealer's commitments")
	}
	require.Error(t, keeper.VerifyEvaluation(c2.FeldmanCommitments, 0, nil))
	require.Error(t, keeper.VerifyEvaluation(c2.FeldmanCommitments, 1, []byte("not a scalar")))
}

// TestTriggerDKGClearsRound: a governance re-key drops registrations,
// contributions and complaints of the previous round.
func TestTriggerDKGClearsRound(t *testing.T) {
	f, ms := initMsgServer(t)
	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{Round: 1, Phase: types.DKGPhase_DKG_PHASE_ACTIVE, ExpectedValidators: []string{"val1"}}))
	require.NoError(t, f.keeper.SetDKGRegistration(f.ctx, types.DKGContribution{ValidatorAddress: "val1", Round: 1, FeldmanCommitments: [][]byte{[]byte("pk")}}))
	require.NoError(t, f.keeper.SetDKGContributionVal(f.ctx, types.DKGContribution{ValidatorAddress: "val1", Round: 1}))
	require.NoError(t, f.keeper.SetDKGComplaint(f.ctx, types.DKGComplaint{DealerAddress: "val1", ComplainerAddress: "val2"}))
	require.NoError(t, f.keeper.TLEJudgedEpochs.Set(f.ctx, 7))

	_, err = ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{Authority: authority})
	require.NoError(t, err)
	_, found := f.keeper.GetDKGRegistration(f.ctx, "val1")
	require.False(t, found, "stale registration would have dealers encrypt to a retired key")
	require.Empty(t, f.keeper.GetAllDKGContributions(f.ctx))
	require.Empty(t, f.keeper.GetAllDKGComplaints(f.ctx))
	judged, _ := f.keeper.TLEJudgedEpochs.Get(f.ctx)
	require.Zero(t, judged, "liveness ring reset with the key set")
	st, _ := f.keeper.GetDKGStateVal(f.ctx)
	require.Equal(t, uint64(2), st.Round)
	require.Greater(t, st.ComplaintDeadline, st.ContributionDeadline)
	require.Greater(t, st.JustificationDeadline, st.ComplaintDeadline)
}

// TestGenesisComplaintsRoundTrip exports and re-imports complaints.
func TestGenesisComplaintsRoundTrip(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.SetDKGComplaint(f.ctx, types.DKGComplaint{DealerAddress: "val1", ComplainerAddress: "val2"}))
	require.NoError(t, f.keeper.SetDKGComplaint(f.ctx, types.DKGComplaint{DealerAddress: "val1", ComplainerAddress: "val3", Justified: true, Evaluation: []byte{1, 2}}))
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.DkgComplaints, 2)

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))
	got, found := f2.keeper.GetDKGComplaint(f2.ctx, "val1", "val3")
	require.True(t, found)
	require.True(t, got.Justified)
	require.Equal(t, []byte{1, 2}, got.Evaluation)
	require.Len(t, f2.keeper.ComplaintsAgainst(f2.ctx, "val1"), 2)
}
