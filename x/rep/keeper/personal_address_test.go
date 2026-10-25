package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestPersonalAddressGate pins that membership is a person: an address x/vault
// reports as a vault can neither be invited nor accept an invitation nor be a
// migration target (multisig-accounts plan §4.6).
func TestPersonalAddressGate(t *testing.T) {
	f := migrationFixture(t)
	inviter := sdk.AccAddress([]byte("gate-inviter_______"))
	vault := sdk.AccAddress([]byte("gate-vault_________"))
	person := sdk.AccAddress([]byte("gate-person________"))
	seedMigrant(t, f, inviter, 1_000_000_000)
	m := mustMember(t, f, inviter)
	m.InvitationCredits = 5
	require.NoError(t, f.keeper.Member.Set(f.ctx, inviter.String(), m))
	f.keeper.SetVaultChecker(func(_ context.Context, addr string) bool { return addr == vault.String() })

	require.NoError(t, f.keeper.IsPersonalAddress(f.ctx, person.String()))
	require.ErrorContains(t, f.keeper.IsPersonalAddress(f.ctx, vault.String()), "vault")

	// Invitation of a vault is refused; a person is fine.
	_, err := f.keeper.CreateInvitation(f.ctx, inviter, vault, math.NewInt(100_000_000), nil)
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	invID, err := f.keeper.CreateInvitation(f.ctx, inviter, person, math.NewInt(100_000_000), nil)
	require.NoError(t, err)

	// Acceptance re-runs the gate: flip the invitee into a vault after the
	// invitation was created (a vault created in between).
	f.keeper.SetVaultChecker(func(_ context.Context, addr string) bool { return addr == person.String() })
	err = f.keeper.AcceptInvitationWithRecovery(f.ctx, invID, person, "CC0-1.0", nil)
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	f.keeper.SetVaultChecker(func(context.Context, string) bool { return false })
	require.NoError(t, f.keeper.AcceptInvitationWithRecovery(f.ctx, invID, person, "CC0-1.0", nil))

	// Migration target freshness uses the same gate.
	f.keeper.SetVaultChecker(func(_ context.Context, addr string) bool { return addr == vault.String() })
	_, err = f.keeper.RequestKeyMigration(f.ctx, inviter.String(), vault.String())
	require.ErrorIs(t, err, types.ErrMigrationTargetNotFresh)
}
