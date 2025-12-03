package keeper_test

import (
	"fmt"
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func TestPolicyPermissionsMsgServerCreate(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	for i := 0; i < 5; i++ {
		// Generate a unique 32-byte string for the address to satisfy strict codecs
		addrBytes := []byte(fmt.Sprintf("policyAddress_______________%d", i))
		policyAddr, err := f.addressCodec.BytesToString(addrBytes)
		require.NoError(t, err)

		// Self-Regulation: Authority must match PolicyAddress to be authorized
		expected := &types.MsgCreatePolicyPermissions{
			Authority:       policyAddr,
			PolicyAddress:   policyAddr,
			AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
		}

		_, err = srv.CreatePolicyPermissions(f.ctx, expected)
		require.NoError(t, err)

		rst, err := f.keeper.PolicyPermissions.Get(f.ctx, expected.PolicyAddress)
		require.NoError(t, err)
		require.Equal(t, expected.PolicyAddress, rst.PolicyAddress)
	}
}

func TestPolicyPermissionsMsgServerUpdate(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Create a policy first
	addrBytes := []byte("policyAddress_______________00")
	policyAddr, err := f.addressCodec.BytesToString(addrBytes)
	require.NoError(t, err)

	unauthBytes := []byte("unauthorizedAddr____________00")
	unauthorizedAddr, err := f.addressCodec.BytesToString(unauthBytes)
	require.NoError(t, err)

	// Create initial permissions
	initMsg := &types.MsgCreatePolicyPermissions{
		Authority:       policyAddr,
		PolicyAddress:   policyAddr,
		AllowedMessages: []string{"/msg.A", "/msg.B"},
	}
	_, err = srv.CreatePolicyPermissions(f.ctx, initMsg)
	require.NoError(t, err)

	tests := []struct {
		desc    string
		request *types.MsgUpdatePolicyPermissions
		err     error
	}{
		{
			desc: "invalid address",
			request: &types.MsgUpdatePolicyPermissions{
				Authority:     "invalid",
				PolicyAddress: policyAddr,
			},
			err: sdkerrors.ErrInvalidAddress,
		},
		{
			desc: "unauthorized",
			request: &types.MsgUpdatePolicyPermissions{
				Authority:     unauthorizedAddr,
				PolicyAddress: policyAddr,
			},
			err: sdkerrors.ErrUnauthorized,
		},
		{
			desc: "key not found",
			request: &types.MsgUpdatePolicyPermissions{
				Authority:     unauthorizedAddr,
				PolicyAddress: unauthorizedAddr,
			},
			err: sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "ratchet down violation (adding permissions)",
			request: &types.MsgUpdatePolicyPermissions{
				Authority:       policyAddr,
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/msg.A", "/msg.B", "/msg.C"}, // Adding C is forbidden for self
			},
			err: sdkerrors.ErrUnauthorized,
		},
		{
			desc: "completed (ratchet down success - removing permissions)",
			request: &types.MsgUpdatePolicyPermissions{
				Authority:       policyAddr,
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/msg.A"}, // Removing B is allowed
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err = srv.UpdatePolicyPermissions(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				rst, err := f.keeper.PolicyPermissions.Get(f.ctx, tc.request.PolicyAddress)
				require.NoError(t, err)
				require.Equal(t, tc.request.PolicyAddress, rst.PolicyAddress)
				require.Equal(t, tc.request.AllowedMessages, rst.AllowedMessages)
			}
		})
	}
}

func TestPolicyPermissionsMsgServerDelete(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Create a policy
	addrBytes := []byte("policyAddress_______________00")
	policyAddr, err := f.addressCodec.BytesToString(addrBytes)
	require.NoError(t, err)

	unauthBytes := []byte("unauthorizedAddr____________00")
	unauthorizedAddr, err := f.addressCodec.BytesToString(unauthBytes)
	require.NoError(t, err)

	_, err = srv.CreatePolicyPermissions(f.ctx, &types.MsgCreatePolicyPermissions{
		Authority:     policyAddr,
		PolicyAddress: policyAddr,
	})
	require.NoError(t, err)

	tests := []struct {
		desc    string
		request *types.MsgDeletePolicyPermissions
		err     error
	}{
		{
			desc: "invalid address",
			request: &types.MsgDeletePolicyPermissions{
				Authority:     "invalid",
				PolicyAddress: policyAddr,
			},
			err: sdkerrors.ErrInvalidAddress,
		},
		{
			desc: "unauthorized",
			request: &types.MsgDeletePolicyPermissions{
				Authority:     unauthorizedAddr,
				PolicyAddress: policyAddr,
			},
			err: sdkerrors.ErrUnauthorized,
		},
		{
			desc: "key not found",
			request: &types.MsgDeletePolicyPermissions{
				Authority:     unauthorizedAddr,
				PolicyAddress: unauthorizedAddr,
			},
			err: sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "completed",
			request: &types.MsgDeletePolicyPermissions{
				Authority:     policyAddr,
				PolicyAddress: policyAddr,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err = srv.DeletePolicyPermissions(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				found, err := f.keeper.PolicyPermissions.Has(f.ctx, tc.request.PolicyAddress)
				require.NoError(t, err)
				require.False(t, found)
			}
		})
	}
}
