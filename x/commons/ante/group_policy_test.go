package ante_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"

	"sparkdream/x/commons/ante"
	"sparkdream/x/commons/keeper"
	module "sparkdream/x/commons/module"
	"sparkdream/x/commons/types"
)

// --- 1. MOCK GROUP KEEPER ---
type mockGroupKeeper struct{}

func (m mockGroupKeeper) GroupPolicyInfo(ctx context.Context, request *group.QueryGroupPolicyInfoRequest) (*group.QueryGroupPolicyInfoResponse, error) {
	return &group.QueryGroupPolicyInfoResponse{
		Info: &group.GroupPolicyInfo{
			Address:  request.Address,
			Metadata: "some-metadata",
		},
	}, nil
}

// --- 2. TEST SETUP HELPERS ---
func setupKeeper(t *testing.T) (keeper.Keeper, sdk.Context) {
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	authority := authtypes.NewModuleAddress(types.GovModuleName)

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		nil, nil, nil,
		groupkeeper.Keeper{},
	)

	params := types.DefaultParams()
	params.CommonsCouncilFee = "1000stake"
	err := k.Params.Set(ctx, params)
	require.NoError(t, err)

	return k, ctx
}

// --- 3. MOCK TRANSACTION ---
type mockFeeTx struct {
	msgs []sdk.Msg
	fee  sdk.Coins
}

func (m *mockFeeTx) GetMsgs() []sdk.Msg { return m.msgs }

func (m *mockFeeTx) GetMsgsV2() ([]protoreflect.ProtoMessage, error) {
	v2Msgs := make([]protoreflect.ProtoMessage, len(m.msgs))
	for i, msg := range m.msgs {
		pMsg, ok := msg.(protoreflect.ProtoMessage)
		if !ok {
			return nil, fmt.Errorf("message %T does not implement protoreflect.ProtoMessage", msg)
		}
		v2Msgs[i] = pMsg
	}
	return v2Msgs, nil
}

func (m *mockFeeTx) ValidateBasic() error { return nil }
func (m *mockFeeTx) GetFee() sdk.Coins    { return m.fee }
func (m *mockFeeTx) GetGas() uint64       { return 200000 }
func (m *mockFeeTx) FeePayer() []byte     { return nil }
func (m *mockFeeTx) FeeGranter() []byte   { return nil }

// --- 4. THE TESTS ---
func TestGroupPolicyDecorator(t *testing.T) {
	k, ctx := setupKeeper(t)
	mockGroup := mockGroupKeeper{}
	decorator := ante.NewGroupPolicyDecorator(mockGroup, k)

	policyAddr := "cosmos1policy"
	otherAddr := "cosmos1other"

	requiredFee := sdk.NewCoins(sdk.NewCoin("stake", math.NewInt(1000)))
	insufficientFee := sdk.NewCoins(sdk.NewCoin("stake", math.NewInt(10)))

	tests := []struct {
		name        string
		permissions *types.PolicyPermissions
		msg         sdk.Msg
		fee         sdk.Coins
		expectErr   bool
		errMsg      string
	}{
		{
			name:        "Block: No Permissions Found",
			permissions: nil,
			msg:         &banktypes.MsgSend{},
			fee:         requiredFee,
			expectErr:   true,
			errMsg:      "no policy permissions found",
		},
		{
			name: "Block: Message Not in Allowlist",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/some.other.Msg"},
			},
			msg:       &banktypes.MsgSend{},
			fee:       requiredFee,
			expectErr: true,
			errMsg:    "not in the allowlist",
		},
		{
			name: "Block: Loopback Violation (MsgSend to Other)",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
			},
			msg:       &banktypes.MsgSend{FromAddress: policyAddr, ToAddress: otherAddr},
			fee:       requiredFee,
			expectErr: true,
			errMsg:    "Loopback Signal",
		},
		{
			name: "Success: Loopback Allowed (MsgSend to Self)",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
			},
			msg:       &banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr},
			fee:       requiredFee,
			expectErr: false,
		},
		{
			name: "Block: Insufficient Fee (Standard Policy)",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
			},
			msg:       &banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr},
			fee:       insufficientFee,
			expectErr: true,
			errMsg:    "Commons Council proposals require min fee",
		},
		{
			name: "Success: Veto Exemption (No Fee Required)",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/sparkdream.commons.v1.MsgEmergencyCancelProposal"},
			},
			msg:       &types.MsgEmergencyCancelProposal{},
			fee:       sdk.NewCoins(),
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.permissions != nil {
				err := k.PolicyPermissions.Set(ctx, policyAddr, *tc.permissions)
				require.NoError(t, err)
			} else {
				_ = k.PolicyPermissions.Remove(ctx, policyAddr)
			}

			anyMsg, err := codectypes.NewAnyWithValue(tc.msg)
			require.NoError(t, err)

			proposalMsg := &group.MsgSubmitProposal{
				GroupPolicyAddress: policyAddr,
				Messages:           []*codectypes.Any{anyMsg},
			}

			// CHANGE: Pass address of mockFeeTx (&) to satisfy pointer receiver interface
			tx := &mockFeeTx{
				msgs: []sdk.Msg{proposalMsg},
				fee:  tc.fee,
			}

			_, err = decorator.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				return ctx, nil
			})

			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
