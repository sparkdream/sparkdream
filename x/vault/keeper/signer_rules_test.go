package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

// VLT-03: a signer set may not list a tombstoned address, nor two addresses
// of the same member (one approval would carry both weights).
func TestSignerSetRefusesTombstonesAndAliasDuplicates(t *testing.T) {
	f := initFixture(t)
	rep := f.withRep()
	f.bank.balances[creator] = math.NewInt(100_000_000)
	rep.canonicalOf[alice] = dave
	rep.tombstoned[alice] = true

	_, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: creator, Signers: signers(map[string]uint64{alice: 1, bob: 1}, alice, bob),
		Threshold: 1, ProposalTtl: 24 * 3600,
	})
	require.ErrorIs(t, err, types.ErrNotPersonalAddress, "tombstoned signer")

	// Same member twice through the alias, even without a tombstone flag.
	rep.tombstoned[alice] = false
	_, err = f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: creator, Signers: signers(map[string]uint64{alice: 1, dave: 1, bob: 1}, alice, dave, bob),
		Threshold: 2, ProposalTtl: 24 * 3600,
	})
	require.ErrorIs(t, err, types.ErrInvalidSignerSet, "two addresses of one member")

	// The same rule applies to MsgUpdateVault.
	delete(rep.canonicalOf, alice)
	v := create2of3(t, f)
	rep.canonicalOf[carol] = dave
	err = f.keeper.UpdateVault(f.ctx, v.Address, signers(map[string]uint64{alice: 1, carol: 1, dave: 1}, alice, carol, dave), 2, 24*3600, "")
	require.ErrorIs(t, err, types.ErrInvalidSignerSet)
}

// VLT-02: one member may be an accepted signer of at most
// MaxVaultsPerSigner vaults. The creator's own entry is accepted at create;
// updating a vault the address already signs does not count that vault twice.
func TestMaxVaultsPerSigner(t *testing.T) {
	f := initFixture(t)
	f.bank.balances[alice] = math.NewInt(1_000_000_000_000)
	f.bank.balances[creator] = math.NewInt(1_000_000_000_000)
	var last types.Vault
	for i := 0; i < types.MaxVaultsPerSigner; i++ {
		res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
			Creator: alice, Signers: signers(map[string]uint64{alice: 1}, alice),
			Threshold: 1, ProposalTtl: 24 * 3600, Label: fmt.Sprintf("v%d", i),
		})
		require.NoError(t, err)
		last, _ = f.keeper.Vault.Get(f.ctx, res.VaultId)
		require.True(t, last.Signers[0].Accepted, "the creator's own entry is accepted")
	}
	_, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: alice, Signers: signers(map[string]uint64{alice: 1, bob: 1}, alice, bob),
		Threshold: 1, ProposalTtl: 24 * 3600,
	})
	require.ErrorIs(t, err, types.ErrTooManyVaultsForSigner)

	// Someone else may still list alice (no weight, no cap), but she cannot
	// accept a 21st vault.
	res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: creator, Signers: signers(map[string]uint64{alice: 1, bob: 1}, alice, bob),
		Threshold: 1, ProposalTtl: 24 * 3600,
	})
	require.NoError(t, err)
	_, err = f.srv.AcceptVaultSigner(f.ctx, &types.MsgAcceptVaultSigner{Signer: alice, VaultId: res.VaultId})
	require.ErrorIs(t, err, types.ErrTooManyVaultsForSigner)

	// Rotating one of alice's own vaults keeps working.
	require.NoError(t, f.keeper.UpdateVault(f.ctx, last.Address, signers(map[string]uint64{alice: 1, bob: 1}, alice, bob), 1, 24*3600, "rotated"))
	// Bob is unaffected by alice's cap.
	_, err = f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: creator, Signers: signers(map[string]uint64{bob: 1}, bob),
		Threshold: 1, ProposalTtl: 24 * 3600,
	})
	require.NoError(t, err)
}

// VLT-02: a listing made without the member's consent carries no weight,
// cannot act, and never consumes the member's MaxVaultsPerSigner cap, so a
// griefer listing a member in many vaults cannot keep them out of a real one.
func TestUnconsentedListingHasNoWeightAndNoCap(t *testing.T) {
	f := initFixture(t)
	f.bank.balances[creator] = math.NewInt(1_000_000_000_000)
	f.bank.balances[alice] = math.NewInt(1_000_000_000_000)
	var grief types.Vault
	for i := 0; i < types.MaxVaultsPerSigner+5; i++ {
		res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
			Creator: creator, Signers: signers(map[string]uint64{sink: 1, alice: 5}, sink, alice),
			Threshold: 1, ProposalTtl: 24 * 3600, Label: fmt.Sprintf("grief%d", i),
		})
		require.NoError(t, err)
		grief, _ = f.keeper.Vault.Get(f.ctx, res.VaultId)
	}
	require.False(t, grief.Signers[0].Accepted, "sink never consented")
	require.False(t, grief.Signers[1].Accepted, "alice never consented")
	has, _ := f.keeper.VaultsBySigner.Has(f.ctx, collections.Join(alice, grief.VaultId))
	require.False(t, has)
	has, _ = f.keeper.VaultInvitesBySigner.Has(f.ctx, collections.Join(alice, grief.VaultId))
	require.True(t, has)

	// No weight: alice's approval does not count, and she cannot act.
	require.Zero(t, f.keeper.AcceptedWeight(f.ctx, grief.Signers))
	require.Zero(t, f.keeper.Tally(f.ctx, grief, types.VaultProposal{Approvals: []string{alice}}))
	_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: grief.VaultId, Messages: []*codectypes.Any{anyMsg(t, &types.MsgCancelProposal{Signer: grief.Address, ProposalId: 1})}})
	require.ErrorIs(t, err, types.ErrNotSigner)

	// Queries keep the two apart.
	q := keeperQuery(f)
	acc, err := q.VaultsBySigner(f.ctx, &types.QueryVaultsBySignerRequest{Address: alice})
	require.NoError(t, err)
	require.Zero(t, acc.Pagination.Total)
	inv, err := q.VaultsBySigner(f.ctx, &types.QueryVaultsBySignerRequest{Address: alice, Invited: true})
	require.NoError(t, err)
	require.Equal(t, uint64(types.MaxVaultsPerSigner+5), inv.Pagination.Total)

	// No cap: alice still creates and accepts real vaults.
	_, err = f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: alice, Signers: signers(map[string]uint64{alice: 1}, alice),
		Threshold: 1, ProposalTtl: 24 * 3600, Label: "real",
	})
	require.NoError(t, err)
	f.bank.balances[bob] = math.NewInt(1_000_000_000)
	shared, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: bob, Signers: signers(map[string]uint64{bob: 1, alice: 1}, bob, alice),
		Threshold: 2, ProposalTtl: 24 * 3600, Label: "shared",
	})
	require.NoError(t, err, "a threshold only reachable with an invitee is allowed at create")
	_, err = f.srv.AcceptVaultSigner(f.ctx, &types.MsgAcceptVaultSigner{Signer: alice, VaultId: shared.VaultId})
	require.NoError(t, err)
	_, err = f.srv.AcceptVaultSigner(f.ctx, &types.MsgAcceptVaultSigner{Signer: alice, VaultId: shared.VaultId})
	require.ErrorIs(t, err, types.ErrAlreadyAccepted)
	_, err = f.srv.AcceptVaultSigner(f.ctx, &types.MsgAcceptVaultSigner{Signer: carol, VaultId: shared.VaultId})
	require.ErrorIs(t, err, types.ErrNotSigner)
	n, err := f.keeper.CountVaultsOfSigner(f.ctx, alice, 0)
	require.NoError(t, err)
	require.Equal(t, 2, n, "only the two accepted vaults count")
	msg, broken := keeperVaultInvariant(f)(f.ctx)
	require.False(t, broken, msg)
}

// VLT-03 / REP-R-02: a migration whose target already co-signs a vault with
// the source is refused on the KEY path. On RECOVERY / COUNCIL it is not (a
// thief could otherwise block recovery with a vault of {old, new}); the
// merged member then counts once in Tally.
func TestMigrationHookRefusesCoSigner(t *testing.T) {
	f := initFixture(t)
	rep := f.withRep()
	v := create2of3(t, f)
	h := keeper.NewMigrationHooks(&f.keeper)
	from := sdk.MustAccAddressFromBech32(alice)

	err := h.BeforeMemberMigration(f.ctx, from, sdk.MustAccAddressFromBech32(bob), reptypes.MigrationPath_MIGRATION_PATH_KEY)
	require.ErrorIs(t, err, types.ErrInvalidSignerSet)
	for _, path := range []reptypes.MigrationPath{reptypes.MigrationPath_MIGRATION_PATH_RECOVERY, reptypes.MigrationPath_MIGRATION_PATH_COUNCIL} {
		require.NoError(t, h.BeforeMemberMigration(f.ctx, from, sdk.MustAccAddressFromBech32(bob), path), path.String())
	}
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, sdk.MustAccAddressFromBech32(dave), reptypes.MigrationPath_MIGRATION_PATH_COUNCIL))
	require.NoError(t, h.AfterMemberMigration(f.ctx, from, sdk.MustAccAddressFromBech32(dave), reptypes.MigrationPath_MIGRATION_PATH_COUNCIL))

	// alice recovers into bob's address: one person, one weight.
	rep.canonicalOf[alice] = bob
	rep.tombstoned[alice] = true
	p := types.VaultProposal{Approvals: []string{bob}}
	require.Equal(t, uint64(1), f.keeper.Tally(f.ctx, v, p))
}

// VLT-07: the per-signer cap counts distinct vaults across every address of
// the member. Signer entries are never re-keyed, so a per-address count
// would let a member who migrates collect a fresh MaxVaultsPerSigner under
// each new address.
func TestMaxVaultsPerSignerCountsAliases(t *testing.T) {
	f := initFixture(t)
	rep := f.withRep()
	f.bank.balances[creator] = math.NewInt(1_000_000_000_000)
	f.bank.balances[alice] = math.NewInt(1_000_000_000_000)
	f.bank.balances[dave] = math.NewInt(1_000_000_000_000)
	var last types.Vault
	for i := 0; i < types.MaxVaultsPerSigner; i++ {
		res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
			Creator: alice, Signers: signers(map[string]uint64{alice: 1}, alice),
			Threshold: 1, ProposalTtl: 24 * 3600, Label: fmt.Sprintf("v%d", i),
		})
		require.NoError(t, err)
		last, _ = f.keeper.Vault.Get(f.ctx, res.VaultId)
	}
	// alice migrates to dave: the 20 entries stay under alice.
	rep.canonicalOf[alice] = dave
	rep.tombstoned[alice] = true

	_, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: dave, Signers: signers(map[string]uint64{dave: 1}, dave),
		Threshold: 1, ProposalTtl: 24 * 3600,
	})
	require.ErrorIs(t, err, types.ErrTooManyVaultsForSigner, "the new address inherits the old one's count")
	// Listed by someone else, dave cannot accept past the cap either.
	inv, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: creator, Signers: signers(map[string]uint64{dave: 1}, dave),
		Threshold: 1, ProposalTtl: 24 * 3600,
	})
	require.NoError(t, err)
	_, err = f.srv.AcceptVaultSigner(f.ctx, &types.MsgAcceptVaultSigner{Signer: dave, VaultId: inv.VaultId})
	require.ErrorIs(t, err, types.ErrTooManyVaultsForSigner)

	// Rotating one of the member's vaults to name the new address does not
	// count that vault twice (alice's entry in it is the vault being replaced).
	require.NoError(t, f.keeper.UpdateVault(f.ctx, last.Address, signers(map[string]uint64{dave: 1}, dave), 1, 24*3600, "rotated"))
	rotated, _ := f.keeper.Vault.Get(f.ctx, last.VaultId)
	require.True(t, rotated.Signers[0].Accepted, "re-keying a migrated accepted signer keeps its acceptance")
	bs, err := keeperQuery(f).VaultsBySigner(f.ctx, &types.QueryVaultsBySignerRequest{Address: dave})
	require.NoError(t, err)
	require.Equal(t, uint64(types.MaxVaultsPerSigner), bs.Pagination.Total, "the member still signs exactly the cap")
}

// VLT-07: the KEY-path hook walks the target's vaults, not the source's, so
// its cost does not grow with the source's alias history, and a target that
// signs nothing returns at once.
func TestMigrationHookWalksTarget(t *testing.T) {
	f := initFixture(t)
	f.withRep()
	f.bank.balances[creator] = math.NewInt(1_000_000_000_000)
	for i := 0; i < types.MaxVaultsPerSigner; i++ {
		res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
			Creator: creator, Signers: signers(map[string]uint64{alice: 1, bob: 1}, alice, bob),
			Threshold: 1, ProposalTtl: 24 * 3600, Label: fmt.Sprintf("v%d", i),
		})
		require.NoError(t, err)
		acceptAll(t, f, res.VaultId)
	}
	h := keeper.NewMigrationHooks(&f.keeper)
	from := sdk.MustAccAddressFromBech32(alice)

	// dave signs nothing: no vault is read.
	gas := f.ctx.GasMeter().GasConsumed()
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, sdk.MustAccAddressFromBech32(dave), reptypes.MigrationPath_MIGRATION_PATH_KEY))
	require.Less(t, f.ctx.GasMeter().GasConsumed()-gas, uint64(5_000), "empty target costs one prefix walk")

	// carol signs one vault without alice: allowed.
	cs, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: creator, Signers: signers(map[string]uint64{carol: 1, sink: 1}, carol, sink),
		Threshold: 1, ProposalTtl: 24 * 3600,
	})
	require.NoError(t, err)
	acceptAll(t, f, cs.VaultId)
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, sdk.MustAccAddressFromBech32(carol), reptypes.MigrationPath_MIGRATION_PATH_KEY))

	// bob co-signs with alice: refused.
	err = h.BeforeMemberMigration(f.ctx, from, sdk.MustAccAddressFromBech32(bob), reptypes.MigrationPath_MIGRATION_PATH_KEY)
	require.ErrorIs(t, err, types.ErrInvalidSignerSet)
}
