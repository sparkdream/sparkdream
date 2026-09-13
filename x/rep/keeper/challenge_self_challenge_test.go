package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
	shieldtypes "sparkdream/x/shield/types"
)

// An assignee cannot challenge their own submitted work. Admitting fault on a
// self-challenge upholds it instantly and pays the challenger reward to the
// very member who failed, making self-dealing profitable (observed live on
// the devnet 2026-09-11: an assignee pocketed the 20% reward on their own
// REJECTED initiative). The gate sits before the stake lock, so a rejected
// self-challenge must also leave the assignee's balance untouched.
func TestCreateChallengeRejectsSelfChallenge(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	mkMember := func(addr string) {
		require.NoError(t, k.Member.Set(ctx, addr, types.Member{
			Address:        addr,
			DreamBalance:   keeper.PtrInt(math.ZeroInt()),
			StakedDream:    keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned: keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
		}))
	}

	creator := sdk.AccAddress([]byte("sc-proj-creator--"))
	mkMember(creator.String())
	k.MintDREAM(ctx, creator, math.NewInt(1000000000))
	creatorMember, err := k.GetMember(ctx, creator)
	require.NoError(t, err)
	creatorMember.TrustLevel = types.TrustLevel_TRUST_LEVEL_ESTABLISHED
	require.NoError(t, k.Member.Set(ctx, creator.String(), creatorMember))
	projectID, err := k.CreateProject(ctx, creator, "ScProj", "D", []string{"coding"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "", math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	assignee := sdk.AccAddress([]byte("sc-assignee-aaaa"))
	mkMember(assignee.String())
	initID, err := k.CreateInitiative(ctx, creator, projectID, "ScInit", "D", []string{"coding"},
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "https://example.org/draft"))

	// Fund the assignee so the rejection is attributable to the self gate,
	// not to an empty balance.
	k.MintDREAM(ctx, assignee, math.NewInt(1000000000))

	_, err = k.CreateChallenge(ctx, assignee, initID, "my own work", nil, math.NewInt(50000000))
	require.ErrorIs(t, err, types.ErrSelfChallenge)

	// The gate fires before LockDREAM: nothing locked, nothing burned.
	member, err := k.GetMember(ctx, assignee)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1000000000).String(), member.DreamBalance.String())
	require.Equal(t, math.ZeroInt().String(), member.StakedDream.String())

	// Control: a different member may challenge the same submitted work.
	challenger := sdk.AccAddress([]byte("sc-challenger-aa"))
	mkMember(challenger.String())
	k.MintDREAM(ctx, challenger, math.NewInt(1000000000))
	chalID, err := k.CreateChallenge(ctx, challenger, initID, "not done", nil, math.NewInt(50000000))
	require.NoError(t, err)
	require.NotZero(t, chalID)

	// Control: the gate is assignee-only, not "anyone with a stake in the
	// outcome". A project creator may still challenge work they did not assign
	// to themselves — deliberate, because a creator challenging in bad faith
	// still has to survive the jury or committee to collect. Needs a second
	// initiative; the one above is already CHALLENGED.
	init2ID, err := k.CreateInitiative(ctx, creator, projectID, "ScInit2", "D", []string{"coding"},
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
	require.NoError(t, err)
	assignee2 := sdk.AccAddress([]byte("sc-assignee-bbbb"))
	mkMember(assignee2.String())
	require.NoError(t, k.AssignInitiativeToMember(ctx, init2ID, assignee2))
	require.NoError(t, k.SubmitInitiativeWork(ctx, init2ID, assignee2, "https://example.org/draft2"))

	creatorChalID, err := k.CreateChallenge(ctx, creator, init2ID, "not to spec", nil, math.NewInt(50000000))
	require.NoError(t, err)
	require.NotZero(t, creatorChalID)
}

// TRIPWIRE -- read this before making it pass again.
//
// TestCreateChallengeRejectsSelfChallenge above covers the directly-signed
// path. The gate it tests compares the MESSAGE SIGNER against the assignee,
// and on the shielded path that signer is always x/shield's module address:
// x/shield requires the inner MsgCreateChallenge to be signed by its own
// module account (ErrInvalidInnerMessageSigner), so the comparison can never
// match and an anonymous self-challenge is NOT rejected by that rule.
//
// What keeps the loop closed today is unrelated and accidental: the shield
// module address has no rep Member record, and both halves of the exploit
// need one -- LockDREAM to take the stake, MintDREAM to pay the reward. The
// anonymous path therefore fails closed at the stake lock.
//
// The obvious way to make anonymous challenges work is to seed the shield
// module address as a Member so the stake and payout resolve. That single
// change unblocks self-challenging AND makes it free, because the stake would
// come from a shared module pool instead of the attacker's own balance --
// strictly worse than today. This test fails the moment that happens.
//
// If you are here because you are completing anonymous challenges: the
// self-challenge rule has to be re-derived from the SHIELDED identity, not
// the signer. ShieldCircuit already computes publicKey = H(secretKey)
// internally and Member.zk_public_key already binds an address to that key
// on-chain, so the fix is an exclusion constraint (publicKey != the
// assignee's registered key, gated by a selector) plus a way for x/rep to
// supply that value -- see the challenge section of docs/x-rep-spec.md.
func TestShieldedChallengePathFailsClosed(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	shieldAddr := authtypes.NewModuleAddress(shieldtypes.ModuleName)

	// 1. The structural fact. The fixture runs InitGenesis(DefaultGenesis()),
	// so this also catches the shield module being seeded as a member there.
	_, err := k.Member.Get(ctx, shieldAddr.String())
	require.Error(t, err, "shield module address must not have a rep Member record")

	// 2. Both halves of the exploit need that record.
	require.ErrorIs(t, k.MintDREAM(ctx, shieldAddr, math.NewInt(1)), types.ErrMemberNotFound,
		"the challenger reward cannot be paid to the shield module address")

	// 3. The behavioural consequence, on a real submitted initiative.
	mkMember := func(addr string) {
		require.NoError(t, k.Member.Set(ctx, addr, types.Member{
			Address:        addr,
			DreamBalance:   keeper.PtrInt(math.ZeroInt()),
			StakedDream:    keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned: keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
		}))
	}

	creator := sdk.AccAddress([]byte("sh-proj-creator--"))
	mkMember(creator.String())
	k.MintDREAM(ctx, creator, math.NewInt(1000000000))
	creatorMember, err := k.GetMember(ctx, creator)
	require.NoError(t, err)
	creatorMember.TrustLevel = types.TrustLevel_TRUST_LEVEL_ESTABLISHED
	require.NoError(t, k.Member.Set(ctx, creator.String(), creatorMember))
	projectID, err := k.CreateProject(ctx, creator, "ShProj", "D", []string{"coding"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "", math.ZeroInt(), math.ZeroInt(), true)
	require.NoError(t, err)

	assignee := sdk.AccAddress([]byte("sh-assignee-aaaa"))
	mkMember(assignee.String())
	initID, err := k.CreateInitiative(ctx, creator, projectID, "ShInit", "D", []string{"coding"},
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
	require.NoError(t, err)
	require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))
	require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "https://example.org/draft"))

	// A challenge arriving as x/shield stands in for an anonymous challenger
	// who may well BE the assignee. Note which error comes back: the
	// self-challenge gate does not fire, because the signer is not the
	// assignee. Only the missing Member record stops it.
	_, err = k.CreateChallenge(ctx, shieldAddr, initID, "anonymous", nil, math.NewInt(50000000))
	require.Error(t, err)
	require.NotErrorIs(t, err, types.ErrSelfChallenge,
		"the signer-based gate cannot see through the shield -- this is the gap the comment describes")
	require.ErrorIs(t, err, types.ErrMemberNotFound,
		"the shielded path must keep failing closed at the stake lock")
}
