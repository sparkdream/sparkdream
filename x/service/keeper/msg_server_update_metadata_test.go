package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestMsgUpdateMetadata(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(f *fixture)
		msg    *types.MsgUpdateMetadata
		expErr error
		assert func(t *testing.T, f *fixture)
	}{
		{
			name: "happy path",
			setup: func(f *fixture) {
				f.seedServiceType(t)
				f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
			},
			msg: &types.MsgUpdateMetadata{
				Operator:    testOperator1,
				ServiceType: testServiceType,
				NewMetadata: []byte("updated"),
			},
			assert: func(t *testing.T, f *fixture) {
				op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
				require.True(t, ok)
				require.Equal(t, []byte("updated"), op.Metadata)
			},
		},
		{
			name: "operator not found",
			setup: func(f *fixture) {
				f.seedServiceType(t)
			},
			msg: &types.MsgUpdateMetadata{
				Operator:    testOperator1,
				ServiceType: testServiceType,
				NewMetadata: []byte("noop"),
			},
			expErr: types.ErrOperatorNotFound,
		},
		{
			name: "invalid signer address",
			setup: func(f *fixture) {
				f.seedServiceType(t)
			},
			msg: &types.MsgUpdateMetadata{
				Operator:    "not-bech32",
				ServiceType: testServiceType,
				NewMetadata: []byte("noop"),
			},
			expErr: types.ErrInvalidSigner,
		},
		{
			name: "metadata too large",
			setup: func(f *fixture) {
				f.seedServiceType(t)
				f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
			},
			msg: &types.MsgUpdateMetadata{
				Operator:    testOperator1,
				ServiceType: testServiceType,
				NewMetadata: make([]byte, types.DefaultMaxMetadataBytes+1),
			},
			expErr: types.ErrInvalidMetadataSize,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			tc.setup(f)
			_, err := f.msgServer.UpdateMetadata(f.ctx, tc.msg)
			if tc.expErr != nil {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expErr.Error())
				return
			}
			require.NoError(t, err)
			if tc.assert != nil {
				tc.assert(t, f)
			}
		})
	}
}
