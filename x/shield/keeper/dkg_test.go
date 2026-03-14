package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

func TestDKGState(t *testing.T) {
	f := initFixture(t)

	t.Run("not found initially", func(t *testing.T) {
		// After genesis init, DKG state is not set (genesis doesn't init DKG)
		// Actually, InitGenesis may set it. Let's use a fresh fixture.
		f2 := initFixtureEmpty(t)
		_, found := f2.keeper.GetDKGStateVal(f2.ctx)
		require.False(t, found)
	})

	t.Run("set and get", func(t *testing.T) {
		state := types.DKGState{
			Round:                1,
			Phase:                types.DKGPhase_DKG_PHASE_REGISTERING,
			ThresholdNumerator:   2,
			ThresholdDenominator: 3,
			ExpectedValidators:   []string{"val1", "val2", "val3"},
			OpenAtHeight:         100,
			RegistrationDeadline: 150,
			ContributionDeadline: 200,
		}
		require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, state))

		got, found := f.keeper.GetDKGStateVal(f.ctx)
		require.True(t, found)
		require.Equal(t, uint64(1), got.Round)
		require.Equal(t, types.DKGPhase_DKG_PHASE_REGISTERING, got.Phase)
		require.Equal(t, uint64(2), got.ThresholdNumerator)
		require.Equal(t, uint64(3), got.ThresholdDenominator)
		require.Len(t, got.ExpectedValidators, 3)
	})

	t.Run("overwrite state", func(t *testing.T) {
		state := types.DKGState{
			Round: 2,
			Phase: types.DKGPhase_DKG_PHASE_ACTIVE,
		}
		require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, state))

		got, found := f.keeper.GetDKGStateVal(f.ctx)
		require.True(t, found)
		require.Equal(t, uint64(2), got.Round)
		require.Equal(t, types.DKGPhase_DKG_PHASE_ACTIVE, got.Phase)
	})
}

func TestDKGContributions(t *testing.T) {
	f := initFixture(t)

	t.Run("not found initially", func(t *testing.T) {
		_, found := f.keeper.GetDKGContributionVal(f.ctx, "val1")
		require.False(t, found)
	})

	t.Run("count is 0 initially", func(t *testing.T) {
		require.Equal(t, uint64(0), f.keeper.CountDKGContributionsVal(f.ctx))
	})

	t.Run("set and get contribution", func(t *testing.T) {
		c := types.DKGContribution{
			ValidatorAddress:   "val1",
			FeldmanCommitments: [][]byte{[]byte("commit1"), []byte("commit2")},
			ProofOfPossession:  []byte("pop1"),
		}
		require.NoError(t, f.keeper.SetDKGContributionVal(f.ctx, c))

		got, found := f.keeper.GetDKGContributionVal(f.ctx, "val1")
		require.True(t, found)
		require.Equal(t, "val1", got.ValidatorAddress)
		require.Len(t, got.FeldmanCommitments, 2)
		require.Equal(t, []byte("pop1"), got.ProofOfPossession)
	})

	t.Run("count after adding", func(t *testing.T) {
		require.NoError(t, f.keeper.SetDKGContributionVal(f.ctx, types.DKGContribution{
			ValidatorAddress:   "val2",
			FeldmanCommitments: [][]byte{[]byte("c1")},
		}))
		require.Equal(t, uint64(2), f.keeper.CountDKGContributionsVal(f.ctx))
	})

	t.Run("get all contributions", func(t *testing.T) {
		all := f.keeper.GetAllDKGContributions(f.ctx)
		require.Len(t, all, 2)
	})

	t.Run("clear contributions", func(t *testing.T) {
		require.NoError(t, f.keeper.ClearDKGContributions(f.ctx))
		require.Equal(t, uint64(0), f.keeper.CountDKGContributionsVal(f.ctx))

		all := f.keeper.GetAllDKGContributions(f.ctx)
		require.Len(t, all, 0)
	})
}

func TestDKGRegistrations(t *testing.T) {
	f := initFixture(t)

	t.Run("not found initially", func(t *testing.T) {
		_, found := f.keeper.GetDKGRegistration(f.ctx, "val1")
		require.False(t, found)
	})

	t.Run("set and get registration", func(t *testing.T) {
		r := types.DKGContribution{
			ValidatorAddress:   "val1",
			FeldmanCommitments: [][]byte{[]byte("pubkey1")},
			ProofOfPossession:  []byte("pop1"),
		}
		require.NoError(t, f.keeper.SetDKGRegistration(f.ctx, r))

		got, found := f.keeper.GetDKGRegistration(f.ctx, "val1")
		require.True(t, found)
		require.Equal(t, "val1", got.ValidatorAddress)
		require.Equal(t, []byte("pubkey1"), got.FeldmanCommitments[0])
	})

	t.Run("get all registrations", func(t *testing.T) {
		require.NoError(t, f.keeper.SetDKGRegistration(f.ctx, types.DKGContribution{
			ValidatorAddress:   "val2",
			FeldmanCommitments: [][]byte{[]byte("pubkey2")},
		}))

		all := f.keeper.GetAllDKGRegistrations(f.ctx)
		require.Len(t, all, 2)
	})

	t.Run("clear registrations", func(t *testing.T) {
		require.NoError(t, f.keeper.ClearDKGRegistrations(f.ctx))

		all := f.keeper.GetAllDKGRegistrations(f.ctx)
		require.Len(t, all, 0)
	})
}

func TestGetDKGRegistrationPubKey(t *testing.T) {
	t.Run("with commitments", func(t *testing.T) {
		r := types.DKGContribution{
			FeldmanCommitments: [][]byte{[]byte("pubkey_bytes")},
		}
		require.Equal(t, []byte("pubkey_bytes"), keeper.GetDKGRegistrationPubKey(r))
	})

	t.Run("empty commitments returns nil", func(t *testing.T) {
		r := types.DKGContribution{}
		require.Nil(t, keeper.GetDKGRegistrationPubKey(r))
	})
}
