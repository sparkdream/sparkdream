package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	nametypes "sparkdream/x/name/types"
	reptypes "sparkdream/x/rep/types"
	seasontypes "sparkdream/x/season/types"
	servicetypes "sparkdream/x/service/types"
	vaulttypes "sparkdream/x/vault/types"
)

// TestMigrationHooksNeverBlockRecoveryOrCouncil walks every
// MemberMigrationHooks implementation wired in app.go and holds the x/rep
// spec rule "Nobody but the owner can make a recovery or council migration
// fail": a module may veto from-side state on the KEY path only. Anyone can
// create from-side state (a display-name report, a name dispute), so a
// from-side veto on RECOVERY or COUNCIL lets a thief or griefer fail every
// recovery reveal, and each failed reveal burns one of the victim's recovery
// commitments (REP-R-101).
//
// Every wired hook must have an entry in fromSideVetoes. A hook that vetoes
// from-side state on the KEY path seeds that state here; the test checks the
// seed makes KEY fail (so the seed is real) and that RECOVERY and COUNCIL
// pass with every seed applied at once. A hook with no from-side check has a
// nil seed. Adding a hook without an entry fails the test.
func TestMigrationHooksNeverBlockRecoveryOrCouncil(t *testing.T) {
	files := snapshotGenesisFiles(t)
	if len(files) == 0 {
		t.Skip("no e2e snapshot; run /save-setup")
	}
	app, ctx, err := newAppFromGenesisFile(t, files[0])
	if err != nil {
		t.Skipf("snapshot %s does not import (%v); re-run /save-setup", files[0], err)
	}

	from := sdk.AccAddress([]byte("hook-guard-from-addr"))
	to := sdk.AccAddress([]byte("hook-guard-to-addr__"))
	other := sdk.AccAddress([]byte("hook-guard-other____"))

	// Keyed by the concrete hook type as fmt's %T prints it.
	fromSideVetoes := map[string]func(t *testing.T, ctx sdk.Context){
		"keeper.MigrationHooks@x/season": func(t *testing.T, ctx sdk.Context) {
			require.NoError(t, app.SeasonKeeper.SetDisplayNameModeration(ctx, from.String(), seasontypes.DisplayNameModeration{
				Member: from.String(), RejectedName: "Guard", ModeratedAt: ctx.BlockHeight(), Active: true,
			}))
		},
		"keeper.MigrationHooks@x/name": func(t *testing.T, ctx sdk.Context) {
			require.NoError(t, app.NameKeeper.OwnerNames.Set(ctx, collections.Join(from.String(), "hookguard")))
			require.NoError(t, app.NameKeeper.SetDispute(ctx, nametypes.Dispute{
				Name: "hookguard", Claimant: other.String(), FiledAt: ctx.BlockHeight(), Active: true,
			}))
		},
		"keeper.MigrationHooks@x/service": func(t *testing.T, ctx sdk.Context) {
			require.NoError(t, app.ServiceKeeper.Operators.Set(ctx, collections.Join([]byte(from), "hook-guard"), servicetypes.Operator{
				Address: from.String(), ServiceType: "hook-guard", Status: servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE,
			}))
		},
		"keeper.MigrationHooks@x/vault": func(t *testing.T, ctx sdk.Context) {
			const id = 987654
			require.NoError(t, app.VaultKeeper.Vault.Set(ctx, id, vaulttypes.Vault{
				VaultId: id,
				Signers: []vaulttypes.VaultSigner{
					{Address: from.String(), Weight: 1, Accepted: true},
					{Address: to.String(), Weight: 1, Accepted: true},
				},
				Threshold: 1,
			}))
			require.NoError(t, app.VaultKeeper.VaultsBySigner.Set(ctx, collections.Join(from.String(), uint64(id))))
			require.NoError(t, app.VaultKeeper.VaultsBySigner.Set(ctx, collections.Join(to.String(), uint64(id))))
		},
		// Target-side checks only.
		"keeper.MigrationHooks@x/artifact":   nil,
		"keeper.MigrationHooks@x/commons":    nil,
		"keeper.MigrationHooks@x/session":    nil,
		"keeper.MigrationHooks@x/federation": nil,
	}

	multi, ok := app.RepKeeper.MigrationHooks().(reptypes.MultiMemberMigrationHooks)
	require.True(t, ok, "app.go wires a MultiMemberMigrationHooks")
	require.NotEmpty(t, multi)

	key := reptypes.MigrationPath_MIGRATION_PATH_KEY
	all, _ := ctx.CacheContext()
	for _, h := range multi {
		name := hookName(h)
		seed, reviewed := fromSideVetoes[name]
		require.True(t, reviewed, "%s is wired as a MemberMigrationHooks but has no entry in this guard: decide its from-side vetoes (KEY path only) and add it", name)
		if seed == nil {
			continue
		}
		// The seed alone must make this hook veto KEY.
		one, _ := ctx.CacheContext()
		seed(t, one)
		require.Error(t, h.BeforeMemberMigration(one, from, to, key), "%s: the seed must trip the KEY-path veto", name)
		seed(t, all)
	}

	for _, path := range []reptypes.MigrationPath{
		reptypes.MigrationPath_MIGRATION_PATH_RECOVERY,
		reptypes.MigrationPath_MIGRATION_PATH_COUNCIL,
	} {
		for _, h := range multi {
			require.NoError(t, h.BeforeMemberMigration(all, from, to, path),
				"%s vetoes %s on from-side state; RECOVERY and COUNCIL are never blocked by it", hookName(h), path)
		}
		require.NoError(t, multi.BeforeMemberMigration(all, from, to, path))
	}
}

// hookName is the hook's type plus the module directory of its package, e.g.
// "keeper.MigrationHooks@x/season".
func hookName(h reptypes.MemberMigrationHooks) string {
	t := reflect.TypeOf(h)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	pkg := strings.TrimSuffix(strings.TrimPrefix(t.PkgPath(), "sparkdream/"), "/keeper")
	return fmt.Sprintf("%T", h) + "@" + pkg
}
