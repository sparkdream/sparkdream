package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// --- Setup Helper ---

func setupKeeperForCancel(t *testing.T) (keeper.Keeper, sdk.Context, *govkeeper.Keeper, groupkeeper.Keeper, sdk.AccAddress) {
	// 1. Setup Store
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())

	keys := storetypes.NewKVStoreKeys(
		types.StoreKey, authtypes.StoreKey, banktypes.StoreKey, govtypes.StoreKey, group.StoreKey,
	)
	stateStore.MountStoreWithDB(keys[types.StoreKey], storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(keys[authtypes.StoreKey], storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(keys[banktypes.StoreKey], storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(keys[govtypes.StoreKey], storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(keys[group.StoreKey], storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, cmtproto.Header{Time: time.Now()}, false, log.NewNopLogger())

	// 2. Setup Codec & Registry
	cdcOptions := codectestutil.CodecOptions{}
	interfaceRegistry := cdcOptions.NewInterfaceRegistry()

	// Register interfaces
	cryptocodec.RegisterInterfaces(interfaceRegistry)
	banktypes.RegisterInterfaces(interfaceRegistry)
	govtypesv1.RegisterInterfaces(interfaceRegistry)
	group.RegisterInterfaces(interfaceRegistry)
	authtypes.RegisterInterfaces(interfaceRegistry)
	distrtypes.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)

	cdc := codec.NewProtoCodec(interfaceRegistry)
	addrCodec := addresscodec.NewBech32Codec("cosmos")
	authority := sdk.AccAddress([]byte("authority"))

	// 3. Setup Real Auth & Bank Keepers
	accountKeeper := authkeeper.NewAccountKeeper(
		cdc, runtime.NewKVStoreService(keys[authtypes.StoreKey]), authtypes.ProtoBaseAccount,
		map[string][]string{
			distrtypes.ModuleName: nil,
			govtypes.ModuleName:   {authtypes.Burner},
			types.ModuleName:      nil,
		},
		addrCodec, "cosmos",
		authority.String(),
	)
	bankKeeper := bankkeeper.NewBaseKeeper(
		cdc, runtime.NewKVStoreService(keys[banktypes.StoreKey]), accountKeeper,
		map[string]bool{}, authority.String(), log.NewNopLogger(),
	)

	// 4. Setup Router
	msgRouter := baseapp.NewMsgServiceRouter()
	msgRouter.SetInterfaceRegistry(interfaceRegistry)
	banktypes.RegisterMsgServer(msgRouter, bankkeeper.NewMsgServerImpl(bankKeeper))

	// 5. Setup Real Group Keeper
	groupK := groupkeeper.NewKeeper(
		keys[group.StoreKey],
		cdc,
		msgRouter,
		accountKeeper,
		group.DefaultConfig(),
	)

	// 6. Setup Real Gov Keeper
	govK := govkeeper.NewKeeper(
		cdc, runtime.NewKVStoreService(keys[govtypes.StoreKey]),
		accountKeeper, bankKeeper, &mockStakingKeeper{}, &mockDistrKeeper{},
		msgRouter,
		govtypes.DefaultConfig(), authority.String(),
	)

	err := govK.Params.Set(ctx, govtypesv1.DefaultParams())
	require.NoError(t, err)

	// 7. Setup Commons Keeper
	commonsK := keeper.NewKeeper(
		runtime.NewKVStoreService(keys[types.StoreKey]),
		cdc,
		addrCodec,
		authority,
		accountKeeper,
		bankKeeper,
		govK,
		groupK,
		mockSplitKeeper{},
		mockUpgradeKeeper{},
	)

	// Set Default Params
	err = commonsK.Params.Set(ctx, types.DefaultParams())
	require.NoError(t, err)

	// Register Commons MsgServer on the router
	types.RegisterMsgServer(msgRouter, keeper.NewMsgServerImpl(commonsK))

	return commonsK, ctx, govK, groupK, authority
}

// --- Tests ---

func TestEmergencyCancelGovProposal(t *testing.T) {
	k, ctx, govK, _, _ := setupKeeperForCancel(t)
	ms := keeper.NewMsgServerImpl(k)

	alice := sdk.AccAddress([]byte("alice_address_______"))
	bob := sdk.AccAddress([]byte("bob_address_________"))

	// Get the Governance Module Address (Must be the signer of proposal messages)
	govModAddr := authtypes.NewModuleAddress(govtypes.ModuleName)

	// Helper to create a proposal in the Voting Period
	createProp := func(expedited bool, msgs []sdk.Msg) uint64 {
		prop, err := govK.SubmitProposal(ctx, msgs, "", "title", "summary", alice, expedited)
		require.NoError(t, err)

		// Move to Voting Period manually
		prop.Status = govtypesv1.StatusVotingPeriod
		votingEnd := ctx.BlockTime().Add(24 * time.Hour)
		prop.VotingEndTime = &votingEnd
		err = govK.Proposals.Set(ctx, prop.Id, prop)
		require.NoError(t, err)

		err = govK.ActiveProposalsQueue.Set(ctx, collections.Join(*prop.VotingEndTime, prop.Id), prop.Id)
		require.NoError(t, err)

		return prop.Id
	}

	// Message Types
	bankMsg := &banktypes.MsgSend{
		FromAddress: govModAddr.String(),
		ToAddress:   bob.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(10))),
	}

	dangerousMsg := &types.MsgUpdateParams{
		Authority: govModAddr.String(),
		Params:    types.DefaultParams(),
	}

	// 1. Setup Permissions for Alice
	// Alice is granted the specific power to execute Emergency Cancel
	cancelMsgType := sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{})

	// The Authority MUST be the Gov Module Address because EmergencyCancel is a Restricted Message.
	_, err := ms.CreatePolicyPermissions(ctx, &types.MsgCreatePolicyPermissions{
		Authority:       govModAddr.String(),
		PolicyAddress:   alice.String(),
		AllowedMessages: []string{cancelMsgType},
	})
	require.NoError(t, err)

	t.Run("Scenario: Authorized User (Alice) Vetoes Proposal", func(t *testing.T) {
		propID := createProp(false, []sdk.Msg{bankMsg})

		// Alice HAS the permission, so this should succeed
		_, err := ms.EmergencyCancelGovProposal(ctx, &types.MsgEmergencyCancelGovProposal{
			Authority:  alice.String(),
			ProposalId: propID,
		})
		require.NoError(t, err)

		prop, _ := govK.Proposals.Get(ctx, propID)
		require.Equal(t, govtypesv1.StatusFailed, prop.Status)
		require.Contains(t, prop.FailedReason, "Emergency Cancel")
	})

	t.Run("Scenario: Unauthorized User (Bob)", func(t *testing.T) {
		propID := createProp(false, []sdk.Msg{bankMsg})

		// Bob does NOT have any permissions registered
		_, err := ms.EmergencyCancelGovProposal(ctx, &types.MsgEmergencyCancelGovProposal{
			Authority:  bob.String(),
			ProposalId: propID,
		})

		// Expect Unauthorized Error
		require.Error(t, err)
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	})

	t.Run("Scenario: Constitutional Protection (Expedited Commons Update)", func(t *testing.T) {
		// Create an EXPEDITED proposal containing a MsgUpdateParams
		propID := createProp(true, []sdk.Msg{dangerousMsg})

		// Alice HAS the permission to cancel, BUT the proposal is Expedited + Constitutional.
		// The Keeper should block this to protect the "Super Majority" will.
		_, err := ms.EmergencyCancelGovProposal(ctx, &types.MsgEmergencyCancelGovProposal{
			Authority:  alice.String(),
			ProposalId: propID,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "Constitutional Protection")
	})

	t.Run("Scenario: Inactive Proposal", func(t *testing.T) {
		propID := createProp(false, []sdk.Msg{bankMsg})

		// Manually close the proposal
		prop, _ := govK.Proposals.Get(ctx, propID)
		prop.Status = govtypesv1.StatusPassed
		_ = govK.Proposals.Set(ctx, propID, prop)

		// Even with permission, you can't cancel a finished proposal
		_, err := ms.EmergencyCancelGovProposal(ctx, &types.MsgEmergencyCancelGovProposal{
			Authority:  alice.String(),
			ProposalId: propID,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "proposal is already finalized")
	})
}
