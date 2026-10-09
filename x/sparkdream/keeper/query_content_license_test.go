package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/sparkdream/keeper"
	"sparkdream/x/sparkdream/types"
)

func TestContentLicenseQuery(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.ContentLicense(f.ctx, &types.QueryContentLicenseRequest{})
	require.NoError(t, err)
	require.Equal(t, "CC0-1.0", resp.License)
	require.Equal(t, commontypes.ChainContentLicenseURL, resp.Url)
	require.Contains(t, resp.Dedication, "public domain")
	require.ElementsMatch(t, []string{"CC0-1.0", "PDM-1.0"}, resp.AcceptedFederatedLicenses)

	_, err = qs.ContentLicense(f.ctx, nil)
	require.Error(t, err)
}
