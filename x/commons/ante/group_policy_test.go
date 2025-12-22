package ante_test

import (
	"context"
	"fmt"
	"testing"
	"time"

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

	"github.com/cosmos/cosmos-sdk/types/bech32"
)

// --- 1. MOCK GROUP KEEPER ---
type mockGroupKeeper struct {
	proposals map[uint64]*group.Proposal
}

func (m mockGroupKeeper) GroupPolicyInfo(ctx context.Context, request *group.QueryGroupPolicyInfoRequest) (*group.QueryGroupPolicyInfoResponse, error) {
	return &group.QueryGroupPolicyInfoResponse{
		Info: &group.GroupPolicyInfo{
			Address:  request.Address,
			Metadata: "some-metadata",
		},
	}, nil
}

func (m mockGroupKeeper) Proposal(ctx context.Context, request *group.QueryProposalRequest) (*group.QueryProposalResponse, error) {
	p, ok := m.proposals[request.ProposalId]
	if !ok {
		return nil, fmt.Errorf("proposal not found")
	}
	return &group.QueryProposalResponse{Proposal: p}, nil
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
		nil,                  // Auth
		nil,                  // Bank
		nil,                  // Gov
		groupkeeper.Keeper{}, // Group
		nil,                  // Split
		nil,                  // Upgrade
	)

	// Set Fee Params
	params := types.DefaultParams()
	params.ProposalFee = "1000stake"
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

	// Generate VALID Bech32 addresses
	createAddr := func(val string) string {
		bz := make([]byte, 20)
		copy(bz, []byte(val))
		addr, err := bech32.ConvertAndEncode("cosmos", bz)
		require.NoError(t, err)
		return addr
	}

	policyAddr := createAddr("policy______________")
	otherAddr := createAddr("other_______________")

	requiredFee := sdk.NewCoins(sdk.NewCoin("stake", math.NewInt(1000)))
	insufficientFee := sdk.NewCoins(sdk.NewCoin("stake", math.NewInt(10)))

	pack := func(msg sdk.Msg) *codectypes.Any {
		anyMsg, err := codectypes.NewAnyWithValue(msg)
		require.NoError(t, err)
		return anyMsg
	}

	// Time Helpers
	now := time.Now()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	tests := []struct {
		name        string
		permissions *types.PolicyPermissions

		// New Fields for Expiration/Regulated Groups
		extendedGroup  *types.ExtendedGroup // If set, this group is registered in x/commons
		blockTime      time.Time            // Simulates chain time
		storedProposal *group.Proposal      // For testing MsgExec

		msgs      []sdk.Msg
		fee       sdk.Coins
		expectErr bool
		errMsg    string
	}{
		// ----------------------------------------------------------------
		// EXISTING TESTS
		// ----------------------------------------------------------------
		{
			name:        "Block: No Permissions Found",
			permissions: nil,
			msgs:        []sdk.Msg{&banktypes.MsgSend{}},
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
			msgs:      []sdk.Msg{&banktypes.MsgSend{}},
			fee:       requiredFee,
			expectErr: true,
			errMsg:    "not allowed for policy",
		},
		{
			name: "Block: Loopback Violation",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
			},
			msgs:      []sdk.Msg{&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: otherAddr}},
			fee:       requiredFee,
			expectErr: true,
			errMsg:    "Council policies can only send funds to themselves",
		},
		{
			name: "Success: Loopback Allowed",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
			},
			msgs:      []sdk.Msg{&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr}},
			fee:       requiredFee,
			expectErr: false,
		},
		{
			name: "Block: Insufficient Fee (Standard Msg)",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
			},
			msgs:      []sdk.Msg{&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr}},
			fee:       insufficientFee,
			expectErr: true,
			errMsg:    "Commons Council requires min fee",
		},

		// ----------------------------------------------------------------
		// TESTS: EXPIRATION & RENEWAL
		// ----------------------------------------------------------------
		{
			name: "Block: Term Expired (Submit Proposal)",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
			},
			extendedGroup: &types.ExtendedGroup{
				Index:                 "MyCouncil",
				PolicyAddress:         policyAddr,
				CurrentTermExpiration: past.Unix(), // Expired
			},
			blockTime: now,
			msgs:      []sdk.Msg{&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr}},
			fee:       requiredFee,
			expectErr: true,
			errMsg:    "TERM EXPIRED",
		},
		{
			name: "Success: Term Expired but MsgRenewGroup",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/sparkdream.commons.v1.MsgRenewGroup"},
			},
			extendedGroup: &types.ExtendedGroup{
				Index:                 "MyCouncil",
				PolicyAddress:         policyAddr,
				CurrentTermExpiration: past.Unix(), // Expired
			},
			blockTime: now,
			msgs:      []sdk.Msg{&types.MsgRenewGroup{Authority: policyAddr}},
			fee:       requiredFee,
			expectErr: false,
		},
		{
			name: "Success: Not Expired Yet",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
			},
			extendedGroup: &types.ExtendedGroup{
				Index:                 "MyCouncil",
				PolicyAddress:         policyAddr,
				CurrentTermExpiration: future.Unix(), // Valid
			},
			blockTime: now,
			msgs:      []sdk.Msg{&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr}},
			fee:       requiredFee,
			expectErr: false,
		},
		{
			name: "Block: Expired Group (MsgExec Normal Proposal)",
			extendedGroup: &types.ExtendedGroup{
				Index:                 "MyCouncil",
				PolicyAddress:         policyAddr,
				CurrentTermExpiration: past.Unix(), // Expired
			},
			blockTime: now,
			storedProposal: &group.Proposal{
				Id:                 1,
				GroupPolicyAddress: policyAddr,
				Messages:           []*codectypes.Any{pack(&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr})},
			},
			msgs:      []sdk.Msg{&group.MsgExec{ProposalId: 1, Executor: otherAddr}},
			fee:       requiredFee,
			expectErr: true,
			errMsg:    "TERM EXPIRED",
		},
		{
			name: "Success: Expired Group (MsgExec Renew Proposal)",
			extendedGroup: &types.ExtendedGroup{
				Index:                 "MyCouncil",
				PolicyAddress:         policyAddr,
				CurrentTermExpiration: past.Unix(), // Expired
			},
			blockTime: now,
			storedProposal: &group.Proposal{
				Id:                 2,
				GroupPolicyAddress: policyAddr,
				Messages:           []*codectypes.Any{pack(&types.MsgRenewGroup{Authority: policyAddr})},
			},
			msgs:      []sdk.Msg{&group.MsgExec{ProposalId: 2, Executor: otherAddr}},
			fee:       requiredFee,
			expectErr: false,
		},

		// ----------------------------------------------------------------
		// FEE EXEMPTIONS
		// ----------------------------------------------------------------
		{
			name: "Success: Veto Proposal is FREE",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/sparkdream.commons.v1.MsgVetoGroupProposals"},
			},
			msgs:      []sdk.Msg{&types.MsgVetoGroupProposals{Authority: policyAddr, GroupName: "Target"}},
			fee:       insufficientFee, // Should Pass even with low fee
			expectErr: false,
		},
		{
			name: "Success: Emergency Cancel is FREE",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/sparkdream.commons.v1.MsgEmergencyCancelGovProposal"},
			},
			msgs:      []sdk.Msg{&types.MsgEmergencyCancelGovProposal{Authority: policyAddr, ProposalId: 1}},
			fee:       insufficientFee, // Should Pass
			expectErr: false,
		},
		{
			name: "Block: Delete Group is PAID (Not Exempt)",
			permissions: &types.PolicyPermissions{
				PolicyAddress:   policyAddr,
				AllowedMessages: []string{"/sparkdream.commons.v1.MsgDeleteGroup"},
			},
			msgs:      []sdk.Msg{&types.MsgDeleteGroup{Authority: policyAddr, GroupName: "Target"}},
			fee:       insufficientFee, // Should Fail
			expectErr: true,
			errMsg:    "Commons Council requires min fee",
		},
		{
			name: "Block: Mixed Free + Paid Message (Partial Exemption Fails)",
			permissions: &types.PolicyPermissions{
				PolicyAddress: policyAddr,
				AllowedMessages: []string{
					"/sparkdream.commons.v1.MsgVetoGroupProposals",
					"/cosmos.bank.v1beta1.MsgSend",
				},
			},
			msgs: []sdk.Msg{
				&types.MsgVetoGroupProposals{Authority: policyAddr, GroupName: "Target"},
				&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr}, // Paid action triggers fee
			},
			fee:       insufficientFee, // Should Fail because of MsgSend
			expectErr: true,
			errMsg:    "Commons Council requires min fee",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Setup Context Time
			testCtx := ctx
			if !tc.blockTime.IsZero() {
				testCtx = ctx.WithBlockTime(tc.blockTime)
			} else {
				testCtx = ctx.WithBlockTime(now)
			}

			// 2. Setup Permissions
			if tc.permissions != nil {
				err := k.PolicyPermissions.Set(testCtx, policyAddr, *tc.permissions)
				require.NoError(t, err)
			} else {
				_ = k.PolicyPermissions.Remove(testCtx, policyAddr)
			}

			// 3. Setup Regulated Group (and Index)
			if tc.extendedGroup != nil {
				// Set the Group
				err := k.ExtendedGroup.Set(testCtx, tc.extendedGroup.Index, *tc.extendedGroup)
				require.NoError(t, err)
				// Set the Index (Policy -> Name)
				err = k.PolicyToName.Set(testCtx, tc.extendedGroup.PolicyAddress, tc.extendedGroup.Index)
				require.NoError(t, err)
			} else {
				// Clean up for standard tests
				_ = k.ExtendedGroup.Remove(testCtx, "MyCouncil")
				_ = k.PolicyToName.Remove(testCtx, policyAddr)
			}

			// 4. Setup Mock Keeper with Proposals
			mock := mockGroupKeeper{
				proposals: make(map[uint64]*group.Proposal),
			}
			if tc.storedProposal != nil {
				mock.proposals[tc.storedProposal.Id] = tc.storedProposal
			}

			// 5. Run Decorator
			decorator := ante.NewGroupPolicyDecorator(mock, k)

			var anyMsgs []*codectypes.Any
			for _, m := range tc.msgs {
				anyMsgs = append(anyMsgs, pack(m))
			}

			// Wrap in MsgSubmitProposal if not already an Exec or other msg
			var txMsgs []sdk.Msg
			if len(tc.msgs) > 0 {
				switch tc.msgs[0].(type) {
				case *group.MsgExec:
					txMsgs = tc.msgs // Use as is
				default:
					txMsgs = []sdk.Msg{&group.MsgSubmitProposal{
						GroupPolicyAddress: policyAddr,
						Messages:           anyMsgs,
					}}
				}
			}

			tx := &mockFeeTx{
				msgs: txMsgs,
				fee:  tc.fee,
			}

			_, err := decorator.AnteHandle(testCtx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				return ctx, nil
			})

			if tc.expectErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					require.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
