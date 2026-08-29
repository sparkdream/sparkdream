package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func reviewerConfig(t *testing.T, f *fixture) types.BondedRoleConfig {
	t.Helper()
	cfg, err := f.keeper.BondedRoleConfigs.Get(f.ctx, int32(types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER))
	require.NoError(t, err)
	return cfg
}

// The reviewer floor is enforced from the BondedRoleConfig but owned by params,
// so the write-through is the only thing keeping the two in agreement.
func TestSyncReviewerBondedRoleConfigWritesParamsThrough(t *testing.T) {
	f := initFixture(t)

	p := types.DefaultParams()
	p.MinReviewerBond = math.NewInt(750_000_000)
	p.ReviewerDemotionThreshold = math.NewInt(300_000_000)
	p.MinReviewerTrustLevel = "TRUST_LEVEL_TRUSTED"
	p.MinReviewerRepTier = 2
	p.MinReviewerAgeBlocks = 42
	p.ReviewerDemotionCooldown = 1234
	p.ReviewerUnbondCooldown = 5678

	require.NoError(t, f.keeper.SyncReviewerBondedRoleConfig(f.ctx, p))

	cfg := reviewerConfig(t, f)
	require.Equal(t, "750000000", cfg.MinBond)
	require.Equal(t, "300000000", cfg.DemotionThreshold)
	require.Equal(t, "TRUST_LEVEL_TRUSTED", cfg.MinTrustLevel)
	require.Equal(t, uint64(2), cfg.MinRepTier)
	require.Equal(t, int64(42), cfg.MinAgeBlocks)
	require.Equal(t, int64(1234), cfg.DemotionCooldown)
	require.Equal(t, int64(5678), cfg.UnbondCooldown)
}

// A deliberate zero is a value, not an omission. If the sync substituted a
// default here, a council that voted for no unbond cooldown would get 0 in
// params and 14 days in the config — the exact params/enforcement drift the
// write-through exists to close.
func TestSyncReviewerBondedRoleConfigKeepsExplicitZeros(t *testing.T) {
	f := initFixture(t)

	p := types.DefaultParams()
	p.MinReviewerRepTier = 0
	p.MinReviewerAgeBlocks = 0
	p.ReviewerDemotionCooldown = 0
	p.ReviewerUnbondCooldown = 0
	require.NoError(t, p.Validate(), "zeros are valid values for these fields")

	require.NoError(t, f.keeper.SyncReviewerBondedRoleConfig(f.ctx, p))

	cfg := reviewerConfig(t, f)
	require.Equal(t, uint64(0), cfg.MinRepTier)
	require.Equal(t, int64(0), cfg.MinAgeBlocks)
	require.Equal(t, int64(0), cfg.DemotionCooldown)
	require.Equal(t, int64(0), cfg.UnbondCooldown)
}

// Validation keeps nil bonds from ever being stored, so this guards the one
// case that would panic in Int.String() instead. Falling back to zero would be
// far worse than falling back to the shipped number: a 0 floor makes the role
// free to enter.
func TestSyncReviewerBondedRoleConfigNilBondsFallBackToDefaults(t *testing.T) {
	f := initFixture(t)

	var stale types.Params // zero value: nil Ints
	require.NoError(t, f.keeper.SyncReviewerBondedRoleConfig(f.ctx, stale))

	cfg := reviewerConfig(t, f)
	defaults := types.DefaultParams()
	require.Equal(t, defaults.MinReviewerBond.String(), cfg.MinBond)
	require.NotEqual(t, "0", cfg.MinBond)
	require.Equal(t, defaults.ReviewerDemotionThreshold.String(), cfg.DemotionThreshold)
}

// InitGenesis must let params win over whatever the genesis file seeded in
// bonded_role_config_list, so a chain cannot boot advertising one reviewer
// policy in params while enforcing another from the seed.
func TestInitGenesisReviewerConfigComesFromParams(t *testing.T) {
	f := initFixture(t)

	genState := types.DefaultGenesis()
	genState.Params.MinReviewerBond = math.NewInt(1_100_000_000)
	genState.Params.ReviewerDemotionThreshold = math.NewInt(550_000_000)
	genState.Params.ReviewerUnbondCooldown = 99

	// Seed the list with a conflicting reviewer entry; the sync must overwrite it.
	for i := range genState.BondedRoleConfigList {
		if genState.BondedRoleConfigList[i].RoleType == types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER {
			genState.BondedRoleConfigList[i].MinBond = "1"
			genState.BondedRoleConfigList[i].DemotionThreshold = "1"
			genState.BondedRoleConfigList[i].UnbondCooldown = 1
		}
	}
	require.NoError(t, genState.Validate())
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *genState))

	cfg := reviewerConfig(t, f)
	require.Equal(t, "1100000000", cfg.MinBond)
	require.Equal(t, "550000000", cfg.DemotionThreshold)
	require.Equal(t, int64(99), cfg.UnbondCooldown)
}

// The council path is how the policy is meant to be retuned day to day, so the
// write-through matters most here.
func TestMsgUpdateOperationalParamsSyncsReviewerBondedRoleConfig(t *testing.T) {
	f := initFixtureNilCommons(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	op := types.DefaultRepOperationalParams()
	op.MinReviewerBond = math.NewInt(800_000_000)
	op.ReviewerDemotionThreshold = math.NewInt(200_000_000)
	op.MinReviewerTrustLevel = "TRUST_LEVEL_CORE"
	op.ReviewerUnbondCooldown = 4242

	_, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
		Authority:         f.keeper.GetAuthorityString(),
		OperationalParams: op,
	})
	require.NoError(t, err)

	cfg := reviewerConfig(t, f)
	require.Equal(t, "800000000", cfg.MinBond)
	require.Equal(t, "200000000", cfg.DemotionThreshold)
	require.Equal(t, "TRUST_LEVEL_CORE", cfg.MinTrustLevel)
	require.Equal(t, int64(4242), cfg.UnbondCooldown)

	// Params and enforcement must agree afterwards, not just individually.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, params.MinReviewerBond.String(), cfg.MinBond)
	require.Equal(t, params.ReviewerUnbondCooldown, cfg.UnbondCooldown)
}

// A rejected update must leave both params and the config untouched, or a
// failed proposal would half-apply.
func TestMsgUpdateOperationalParamsRejectsInvalidReviewerPolicy(t *testing.T) {
	f := initFixtureNilCommons(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	before := reviewerConfig(t, f)

	op := types.DefaultRepOperationalParams()
	op.ReviewerDemotionThreshold = op.MinReviewerBond.AddRaw(1)

	_, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
		Authority:         f.keeper.GetAuthorityString(),
		OperationalParams: op,
	})
	require.ErrorContains(t, err, "must not exceed min reviewer bond")
	require.Equal(t, before, reviewerConfig(t, f))
}

// The gov path writes params wholesale and must carry the same write-through as
// the council path; without it gov could move the advertised floor while
// BondRole went on enforcing the old one.
func TestMsgUpdateParamsSyncsReviewerBondedRoleConfig(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	p := types.DefaultParams()
	p.MinReviewerBond = math.NewInt(900_000_000)
	p.ReviewerDemotionThreshold = math.NewInt(450_000_000)

	_, err = ms.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: authority, Params: p})
	require.NoError(t, err)

	cfg := reviewerConfig(t, f)
	require.Equal(t, "900000000", cfg.MinBond)
	require.Equal(t, "450000000", cfg.DemotionThreshold)
}
