package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestUpdateOperationalParams(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
		Authority: f.authority,
		OperationalParams: types.FederationOperationalParams{
			MaxInboundPerBlock:      100,
			MaxOutboundPerBlock:     75,
			MaxContentBodySize:      8192,
			MaxContentUriSize:       1024,
			MaxProtocolMetadataSize: 4096,
			ContentTtl:              60 * 24 * time.Hour,
			AttestationTtl:          15 * 24 * time.Hour,
			GlobalMaxTrustCredit:    2,
			TrustDiscountRate:       math.LegacyNewDecWithPrec(3, 1),
			BridgeInactivityThreshold: 50,
			MaxPrunePerBlock:        200,
		},
	})
	require.NoError(t, err)

	params, _ := f.keeper.Params.Get(f.ctx)
	require.Equal(t, uint64(100), params.MaxInboundPerBlock)
	require.Equal(t, uint64(75), params.MaxOutboundPerBlock)
	require.Equal(t, uint64(8192), params.MaxContentBodySize)
	require.Equal(t, uint32(2), params.GlobalMaxTrustCredit)
}
