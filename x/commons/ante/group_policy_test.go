package ante_test

import (
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

	"sparkdream/x/commons/ante"
	"sparkdream/x/commons/keeper"
	module "sparkdream/x/commons/module"
	"sparkdream/x/commons/types"

	"github.com/cosmos/cosmos-sdk/types/bech32"
)

// --- TEST SETUP HELPERS ---
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
		nil, // Auth
		nil, // Bank
		nil, // Futarchy
		nil, // Gov
		nil, // Split
		nil, // Upgrade
	)

	// Set Fee Params
	params := types.DefaultParams()
	params.ProposalFee = "1000stake"
	err := k.Params.Set(ctx, params)
	require.NoError(t, err)

	return k, ctx
}

// --- MOCK TRANSACTION ---
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

// --- THE TESTS ---
func TestProposalFeeDecorator(t *testing.T) {
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

	requiredFee := sdk.NewCoins(sdk.NewCoin("stake", math.NewInt(1000)))
	insufficientFee := sdk.NewCoins(sdk.NewCoin("stake", math.NewInt(10)))

	pack := func(msg sdk.Msg) *codectypes.Any {
		anyMsg, err := codectypes.NewAnyWithValue(msg)
		require.NoError(t, err)
		return anyMsg
	}

	now := time.Now()

	tests := []struct {
		name      string
		msgs      []*codectypes.Any // Inner proposal messages
		fee       sdk.Coins
		expectErr bool
		errMsg    string
	}{
		{
			name:      "Block: Insufficient Fee (Standard Msg)",
			msgs:      []*codectypes.Any{pack(&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr})},
			fee:       insufficientFee,
			expectErr: true,
			errMsg:    "Commons Council requires min fee",
		},
		{
			name:      "Success: Sufficient Fee",
			msgs:      []*codectypes.Any{pack(&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr})},
			fee:       requiredFee,
			expectErr: false,
		},
		{
			name:      "Success: Veto Proposal is FREE",
			msgs:      []*codectypes.Any{pack(&types.MsgVetoGroupProposals{Authority: policyAddr, GroupName: "Target"})},
			fee:       insufficientFee,
			expectErr: false,
		},
		{
			name:      "Success: Emergency Cancel is FREE",
			msgs:      []*codectypes.Any{pack(&types.MsgEmergencyCancelGovProposal{Authority: policyAddr, ProposalId: 1})},
			fee:       insufficientFee,
			expectErr: false,
		},
		{
			name:      "Block: Delete Group is PAID (Not Exempt)",
			msgs:      []*codectypes.Any{pack(&types.MsgDeleteGroup{Authority: policyAddr, GroupName: "Target"})},
			fee:       insufficientFee,
			expectErr: true,
			errMsg:    "Commons Council requires min fee",
		},
		{
			name: "Block: Mixed Free + Paid Message",
			msgs: []*codectypes.Any{
				pack(&types.MsgVetoGroupProposals{Authority: policyAddr, GroupName: "Target"}),
				pack(&banktypes.MsgSend{FromAddress: policyAddr, ToAddress: policyAddr}),
			},
			fee:       insufficientFee,
			expectErr: true,
			errMsg:    "Commons Council requires min fee",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testCtx := ctx.WithBlockTime(now)

			decorator := ante.NewProposalFeeDecorator(k)

			// Wrap inner messages in a MsgSubmitProposal
			txMsgs := []sdk.Msg{&types.MsgSubmitProposal{
				PolicyAddress: policyAddr,
				Proposer:      policyAddr,
				Messages:      tc.msgs,
			}}

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
