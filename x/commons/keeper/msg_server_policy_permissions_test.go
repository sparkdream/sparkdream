package keeper_test

import (
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func TestPolicyPermissionsMsgServerCreate(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	tests := []struct {
		desc    string
		setup   func() string // Returns the policy address to be used
		request func(policyAddr string) *types.MsgCreatePolicyPermissions
		err     error
	}{
		{
			desc: "success - standard permissions (self-regulated)",
			setup: func() string {
				addrBytes := []byte("policyAddr_Standard_____")
				addr, _ := f.addressCodec.BytesToString(addrBytes)
				return addr
			},
			request: func(policyAddr string) *types.MsgCreatePolicyPermissions {
				return &types.MsgCreatePolicyPermissions{
					Authority:       policyAddr, // Self
					PolicyAddress:   policyAddr,
					AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
				}
			},
			err: nil,
		},
		{
			desc: "failure - restricted permission (self attempted veto)",
			setup: func() string {
				addrBytes := []byte("policyAddr_VetoFail_____")
				addr, _ := f.addressCodec.BytesToString(addrBytes)
				return addr
			},
			request: func(policyAddr string) *types.MsgCreatePolicyPermissions {
				return &types.MsgCreatePolicyPermissions{
					Authority:       policyAddr, // Self (Unauthorized for Veto)
					PolicyAddress:   policyAddr,
					AllowedMessages: []string{"/sparkdream.commons.v1.MsgEmergencyCancelGovProposal"},
				}
			},
			err: sdkerrors.ErrUnauthorized, // validatePermissions should catch this
		},
		{
			desc: "success - restricted permission (granted by Gov)",
			setup: func() string {
				addrBytes := []byte("policyAddr_VetoSuccess__")
				addr, _ := f.addressCodec.BytesToString(addrBytes)
				return addr
			},
			request: func(policyAddr string) *types.MsgCreatePolicyPermissions {
				return &types.MsgCreatePolicyPermissions{
					Authority:       govAddr, // x/gov is signer
					PolicyAddress:   policyAddr,
					AllowedMessages: []string{"/sparkdream.commons.v1.MsgEmergencyCancelGovProposal"},
				}
			},
			err: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			policyAddr := tc.setup()
			msg := tc.request(policyAddr)

			_, err := srv.CreatePolicyPermissions(f.ctx, msg)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				// Verify state
				rst, err := f.keeper.PolicyPermissions.Get(f.ctx, msg.PolicyAddress)
				require.NoError(t, err)
				require.Equal(t, msg.AllowedMessages, rst.AllowedMessages)
			}
		})
	}
}

func TestPolicyPermissionsMsgServerUpdate(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// Setup: Create a policy first
	addrBytes := []byte("policyAddress_______________00")
	policyAddr, err := f.addressCodec.BytesToString(addrBytes)
	require.NoError(t, err)

	unauthBytes := []byte("unauthorizedAddr____________00")
	unauthorizedAddr, err := f.addressCodec.BytesToString(unauthBytes)
	require.NoError(t, err)

	// Create initial permissions (Standard)
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
			desc: "unauthorized signer",
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
			desc: "ratchet down violation (self adding permissions)",
			request: &types.MsgUpdatePolicyPermissions{
				Authority:       policyAddr,
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/msg.A", "/msg.B", "/msg.C"}, // Adding C is forbidden for self
			},
			err: sdkerrors.ErrUnauthorized,
		},
		{
			desc: "restricted violation (self adding veto)",
			request: &types.MsgUpdatePolicyPermissions{
				Authority:       policyAddr,
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/msg.A", "/msg.B", "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal"},
			},
			err: sdkerrors.ErrUnauthorized, // Should fail either at Validation OR Ratchet Down
		},
		{
			desc: "completed (ratchet down success - self removing permissions)",
			request: &types.MsgUpdatePolicyPermissions{
				Authority:       policyAddr,
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/msg.A"}, // Removing B is allowed
			},
			err: nil,
		},
		{
			desc: "completed (gov override - adding restricted permission)",
			request: &types.MsgUpdatePolicyPermissions{
				Authority:       govAddr, // Gov can add anything
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/msg.A", "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal"},
			},
			err: nil,
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
