package app

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	gnovmmoduletypes "github.com/sparkdream/gnovm/x/gnovm/types"
	"github.com/stretchr/testify/require"

	artifactmoduletypes "sparkdream/x/artifact/types"
	commonsmoduletypes "sparkdream/x/commons/types"
	ecosystemmoduletypes "sparkdream/x/ecosystem/types"
	namemoduletypes "sparkdream/x/name/types"
	shieldmoduletypes "sparkdream/x/shield/types"
)

// TestModuleAccountPermissions pins the module account permissions decided
// for x/commons and x/name: commons never mints (CMN-13), and x/name burns
// its registration fees (NAM-07).
func TestModuleAccountPermissions(t *testing.T) {
	perms := map[string][]string{}
	for _, p := range moduleAccPerms {
		perms[p.Account] = p.Permissions
	}

	commons, ok := perms[commonsmoduletypes.ModuleName]
	require.True(t, ok, "commons module account registered")
	require.Empty(t, commons, "commons module account holds no permission")

	name, ok := perms[namemoduletypes.ModuleName]
	require.True(t, ok, "name module account registered")
	require.Equal(t, []string{authtypes.Burner}, name, "x/name may only burn")
}

// TestBlockedModuleAccounts pins which module accounts may receive plain
// sends (APP-04). Every module account is blocked except the ones whose
// funding path is a plain send or a session pull; a new module account is
// blocked unless it is added to the allowed list here with a reason.
func TestBlockedModuleAccounts(t *testing.T) {
	allowed := map[string]string{
		govtypes.ModuleName:             "SDK convention",
		shieldmoduletypes.ModuleName:    "topped up by plain sends",
		ecosystemmoduletypes.ModuleName: "funded by direct sends (ECO-01)",
		gnovmmoduletypes.ModuleName:     "realm coin flows",
		artifactmoduletypes.ModuleName:  "grantee of Harberger tax RecurringPulls",
	}
	blocked := BlockedAddresses()
	for _, p := range moduleAccPerms {
		if _, ok := allowed[p.Account]; ok {
			require.False(t, blocked[p.Account], "%s must accept plain sends", p.Account)
			continue
		}
		require.True(t, blocked[p.Account], "%s must be blocked from plain sends", p.Account)
	}
}
