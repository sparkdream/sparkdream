package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"sparkdream/x/commons/ante"
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// feeTx is the slice of sdk.FeeTx the ProposalFeeDecorator reads.
type feeTx struct {
	msgs []sdk.Msg
	fee  sdk.Coins
}

func (m *feeTx) GetMsgs() []sdk.Msg { return m.msgs }
func (m *feeTx) GetMsgsV2() ([]protoreflect.ProtoMessage, error) {
	out := make([]protoreflect.ProtoMessage, len(m.msgs))
	for i, msg := range m.msgs {
		pm, ok := msg.(protoreflect.ProtoMessage)
		if !ok {
			return nil, fmt.Errorf("message %T is not a protoreflect.ProtoMessage", msg)
		}
		out[i] = pm
	}
	return out, nil
}
func (m *feeTx) ValidateBasic() error { return nil }
func (m *feeTx) GetFee() sdk.Coins    { return m.fee }
func (m *feeTx) GetGas() uint64       { return 200000 }
func (m *feeTx) FeePayer() []byte     { return nil }
func (m *feeTx) FeeGranter() []byte   { return nil }

// setupFeeGroup is a group with two members and a 100 SPARK proposal fee.
func setupFeeGroup(t *testing.T) (keeper.Keeper, sdk.Context, *mockBankKeeperCommons, string, []string, sdk.Coins) {
	t.Helper()
	k, ctx, bank := setupCommonsKeeper(t)
	policy, members := setupBallotGroup(t, k, ctx, "FeeCouncil", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 2, 0)
	params := types.DefaultParams()
	params.ProposalFee = "100000000uspark"
	require.NoError(t, k.Params.Set(ctx, params))
	fee := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100_000_000)))
	return k, ctx, bank, policy, members, fee
}

// accBytes decodes a cosmos-prefixed test address.
func accBytes(t *testing.T, bech string) sdk.AccAddress {
	t.Helper()
	bz, err := sdk.GetFromBech32(bech, "cosmos")
	require.NoError(t, err)
	return bz
}

func signaling(proposer, policy string) *types.MsgSubmitProposal {
	return &types.MsgSubmitProposal{Proposer: proposer, PolicyAddress: policy, Metadata: "signal"}
}

// CMN-12: a MsgSubmitProposal that reaches the handler without passing the
// ante (nested in an x/vault proposal, a commons proposal executing as a
// policy) pays the proposal fee from the proposer to the fee collector, and
// is refused when the proposer cannot pay.
func TestCMN12_NestedSubmitProposalPaysFee(t *testing.T) {
	k, ctx, bank, policy, members, fee := setupFeeGroup(t)
	ms := keeper.NewMsgServerImpl(k)

	resp, err := ms.SubmitProposal(ctx, signaling(members[0], policy))
	require.NoError(t, err)
	require.Equal(t, []moduleSend{{from: accBytes(t, members[0]), module: authtypes.FeeCollectorName, amount: fee}}, bank.moduleSends,
		"no ante credit: the handler charges the proposer once")
	stored, err := k.Proposals.Get(ctx, resp.ProposalId)
	require.NoError(t, err)
	require.Equal(t, members[0], stored.Proposer)
	var charged bool
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == "proposal_fee_charged" {
			charged = true
		}
	}
	require.True(t, charged, "the charge is observable")

	// Emergency-only proposals stay free on this path too.
	bank.moduleSends = nil
	veto, err := codectypes.NewAnyWithValue(&types.MsgVetoGroupProposals{Authority: policy, GroupName: "Other"})
	require.NoError(t, err)
	require.NoError(t, k.PolicyPermissions.Set(ctx, policy, types.PolicyPermissions{
		PolicyAddress: policy, AllowedMessages: []string{sdk.MsgTypeURL(&types.MsgVetoGroupProposals{})},
	}))
	_, err = ms.SubmitProposal(ctx, &types.MsgSubmitProposal{Proposer: members[0], PolicyAddress: policy, Messages: []*codectypes.Any{veto}})
	require.NoError(t, err)
	require.Empty(t, bank.moduleSends, "a veto-only proposal pays no fee")

	// A proposer who cannot pay is refused with the typed error, and no
	// proposal is stored.
	bank.moduleSendErr = sdkerrors.ErrInsufficientFunds
	next, err := k.ProposalSeq.Peek(ctx)
	require.NoError(t, err)
	_, err = ms.SubmitProposal(ctx, signaling(members[1], policy))
	require.ErrorIs(t, err, types.ErrInsufficientProposalFee)
	has, err := k.Proposals.Has(ctx, next)
	require.NoError(t, err)
	require.False(t, has)

	// A zero fee charges nothing.
	bank.moduleSendErr = nil
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	params.ProposalFee = ""
	require.NoError(t, k.Params.Set(ctx, params))
	_, err = ms.SubmitProposal(ctx, signaling(members[1], policy))
	require.NoError(t, err)
	require.Empty(t, bank.moduleSends)
}

// CMN-12: the ante floors the tx fee once per top-level fee-requiring
// MsgSubmitProposal and leaves a credit per proposer, so the handler never
// charges those again (N proposals in a tx pay exactly N fees, all through
// the tx fee). A submission by another proposer on the same context (a
// nested one) has no credit and pays.
func TestCMN12_AnteCreditPreventsDoubleCharge(t *testing.T) {
	k, ctx, bank, policy, members, fee := setupFeeGroup(t)
	ms := keeper.NewMsgServerImpl(k)
	decorator := ante.NewProposalFeeDecorator(k)
	next := func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) { return ctx, nil }

	first, second := signaling(members[0], policy), signaling(members[0], policy)
	tx := &feeTx{msgs: []sdk.Msg{first, second}, fee: fee.MulInt(math.NewInt(2))}
	_, err := decorator.AnteHandle(ctx, &feeTx{msgs: tx.msgs, fee: fee}, false, next)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee, "two proposals need two fees in the tx fee")
	anteCtx, err := decorator.AnteHandle(ctx, tx, false, next)
	require.NoError(t, err)

	// Both floored proposals run through the handler on the ante's context
	// (as baseapp does) without a second charge.
	_, err = ms.SubmitProposal(anteCtx, first)
	require.NoError(t, err)
	_, err = ms.SubmitProposal(anteCtx, second)
	require.NoError(t, err)
	require.Empty(t, bank.moduleSends, "the tx fee already covered both; the handler must not charge again")

	// A third submission by the same proposer has no credit left: it is
	// one the ante never floored, so the handler charges it.
	_, err = ms.SubmitProposal(anteCtx, signaling(members[0], policy))
	require.NoError(t, err)
	require.Len(t, bank.moduleSends, 1)

	// A nested submission by another proposer (an executor's child
	// context keeps the parent's credits) pays from that proposer.
	bank.moduleSends = nil
	nestedCtx, _ := anteCtx.CacheContext()
	_, err = ms.SubmitProposal(nestedCtx, signaling(members[1], policy))
	require.NoError(t, err)
	require.Equal(t, []moduleSend{{from: accBytes(t, members[1]), module: authtypes.FeeCollectorName, amount: fee}}, bank.moduleSends)

	// Without the ante (no credits at all) the handler charges every one.
	bank.moduleSends = nil
	_, err = ms.SubmitProposal(ctx, first)
	require.NoError(t, err)
	require.Len(t, bank.moduleSends, 1)
}
