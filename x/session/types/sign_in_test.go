package types_test

import (
	"strings"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/types"
)

func TestValidSignInService(t *testing.T) {
	for _, s := range []string{
		"mstdn.sparkdream.io",
		"a.b",
		"x-1.example-2.org",
		strings.Repeat("a", 63) + ".io",
	} {
		require.True(t, types.ValidSignInService(s), s)
	}
	for _, s := range []string{
		"",
		"localhost",
		"MSTDN.sparkdream.io",
		"mstdn.sparkdream.io.",
		".sparkdream.io",
		"mstdn..io",
		"-a.io",
		"a-.io",
		"a_b.io",
		"mstdn.sparkdream.io:443",
		"https://mstdn.sparkdream.io",
		"mstdn.sparkdream.io/path",
		strings.Repeat("a", 64) + ".io",
		strings.Repeat("a.", 127) + "io",
	} {
		require.False(t, types.ValidSignInService(s), s)
	}
}

func TestGenesisState_Validate_SignIn(t *testing.T) {
	granter := sdk.AccAddress([]byte("granter_____________")).String()
	grantee := sdk.AccAddress([]byte("grantee_____________")).String()
	grant := func(p *types.SignInPayload) *types.GenesisState {
		gs := types.DefaultGenesis()
		g := types.Grant{
			Id:        1,
			Granter:   granter,
			Grantee:   grantee,
			Type:      types.GrantType_GRANT_TYPE_SIGN_IN,
			Status:    types.GrantStatus_GRANT_STATUS_ACTIVE,
			ExpiresAt: time.Unix(2_000_000_000, 0).UTC(),
		}
		if p != nil {
			g.Payload = &types.Grant_SignIn{SignIn: p}
		}
		gs.Grants = []types.Grant{g}
		gs.GrantSeq = 2
		return gs
	}

	require.NoError(t, grant(&types.SignInPayload{Service: "mstdn.sparkdream.io"}).Validate())
	require.ErrorContains(t, grant(nil).Validate(), "requires sign_in payload")
	require.ErrorContains(t, grant(&types.SignInPayload{Service: "Mstdn.io"}).Validate(), "not a lowercase DNS hostname")
}
