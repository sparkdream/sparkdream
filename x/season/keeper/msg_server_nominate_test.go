package keeper_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerNominate(t *testing.T) {
	// setupNominationSeason sets the current season to NOMINATION status
	// and registers the given address as a member in the mock rep keeper.
	setupNominationSeason := func(t *testing.T, f *beginBlockFixture, creatorAddr sdk.AccAddress) (sdk.Context, string) {
		t.Helper()
		ctx := f.ctx

		creatorStr, err := f.addressCodec.BytesToString(creatorAddr)
		require.NoError(t, err)

		// Set season in NOMINATION phase
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   100000,
			Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
		}
		err = f.keeper.Season.Set(ctx, season)
		require.NoError(t, err)

		// Register creator as a member (default trust level 2)
		f.repKeeper.Members[creatorStr] = true

		return ctx, creatorStr
	}

	tests := []struct {
		name      string
		setup     func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture)
		wantErr   bool
		errIs     error
		errContains string
		validate  func(t *testing.T, f *beginBlockFixture, ctx sdk.Context, resp *types.MsgNominateResponse)
	}{
		{
			name: "invalid creator address",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				return f.keeper, f.ctx, &types.MsgNominate{
					Creator:    "invalid-address",
					ContentRef: "blog/post/1",
					Rationale:  "Great contribution",
				}, f
			},
			wantErr:     true,
			errContains: "invalid creator address",
		},
		{
			name: "maintenance mode active",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx, creatorStr := setupNominationSeason(t, f, TestAddrCreator)

				// Activate maintenance mode via SeasonTransitionState
				err := f.keeper.SeasonTransitionState.Set(ctx, types.SeasonTransitionState{
					MaintenanceMode: true,
				})
				require.NoError(t, err)

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "blog/post/1",
					Rationale:  "Great contribution",
				}, f
			},
			wantErr: true,
			errIs:   types.ErrMaintenanceMode,
		},
		{
			name: "no active season",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				creatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
				require.NoError(t, err)

				// Do NOT set a season -- leave it empty
				return f.keeper, f.ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "blog/post/1",
					Rationale:  "Great contribution",
				}, f
			},
			wantErr: true,
			errIs:   types.ErrSeasonNotActive,
		},
		{
			name: "season not in nomination phase",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx := f.ctx

				creatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
				require.NoError(t, err)

				// Set season to ACTIVE (not NOMINATION)
				season := types.Season{
					Number:     1,
					Name:       "Test Season",
					StartBlock: 0,
					EndBlock:   100000,
					Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
				}
				err = f.keeper.Season.Set(ctx, season)
				require.NoError(t, err)

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "blog/post/1",
					Rationale:  "Great contribution",
				}, f
			},
			wantErr: true,
			errIs:   types.ErrSeasonNotInNominationPhase,
		},
		{
			name: "not a member",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx := f.ctx

				nonMemberStr, err := f.addressCodec.BytesToString(TestAddrMember3)
				require.NoError(t, err)

				// Set season in NOMINATION phase
				season := types.Season{
					Number:     1,
					Name:       "Test Season",
					StartBlock: 0,
					EndBlock:   100000,
					Status:     types.SeasonStatus_SEASON_STATUS_NOMINATION,
				}
				err = f.keeper.Season.Set(ctx, season)
				require.NoError(t, err)

				// Do NOT add this address to the mock rep keeper's Members map

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    nonMemberStr,
					ContentRef: "blog/post/1",
					Rationale:  "Great contribution",
				}, f
			},
			wantErr: true,
			errIs:   types.ErrNotMember,
		},
		{
			name: "insufficient trust level",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx, creatorStr := setupNominationSeason(t, f, TestAddrCreator)

				// The mock returns trust level 2 for members by default.
				// Set the required trust level to 3 so the member fails the check.
				params, err := f.keeper.Params.Get(ctx)
				require.NoError(t, err)
				params.NominationMinTrustLevel = 3
				err = f.keeper.Params.Set(ctx, params)
				require.NoError(t, err)

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "blog/post/1",
					Rationale:  "Great contribution",
				}, f
			},
			wantErr: true,
			errIs:   types.ErrInsufficientTrustLevel,
		},
		{
			name: "rationale too long",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx, creatorStr := setupNominationSeason(t, f, TestAddrCreator)

				params, err := f.keeper.Params.Get(ctx)
				require.NoError(t, err)

				// Build a rationale that exceeds the max length (default 500)
				longRationale := strings.Repeat("x", int(params.NominationRationaleMaxLength)+1)

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "blog/post/1",
					Rationale:  longRationale,
				}, f
			},
			wantErr: true,
			errIs:   types.ErrRationaleTooLong,
		},
		{
			name: "invalid content ref - too few parts",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx, creatorStr := setupNominationSeason(t, f, TestAddrCreator)

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "bad",
					Rationale:  "Great contribution",
				}, f
			},
			wantErr: true,
			errIs:   types.ErrInvalidContentRef,
		},
		{
			name: "invalid content ref - unsupported module",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx, creatorStr := setupNominationSeason(t, f, TestAddrCreator)

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "unknown/type/123",
					Rationale:  "Great contribution",
				}, f
			},
			wantErr: true,
			errIs:   types.ErrInvalidContentRef,
		},
		{
			name: "max nominations reached",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx, creatorStr := setupNominationSeason(t, f, TestAddrCreator)

				params, err := f.keeper.Params.Get(ctx)
				require.NoError(t, err)

				// Pre-create MaxNominationsPerMember nominations by this creator
				for i := uint64(0); i < params.MaxNominationsPerMember; i++ {
					seqVal, err := f.keeper.NominationSeq.Next(ctx)
					require.NoError(t, err)
					nomID := seqVal + 1

					nom := types.Nomination{
						Id:           nomID,
						Nominator:    creatorStr,
						ContentRef:   "blog/post/" + strings.Repeat("1", int(i)+1), // unique refs
						Rationale:    "Existing nomination",
						Season:       1,
						TotalStaked:  math.LegacyZeroDec(),
						Conviction:   math.LegacyZeroDec(),
						RewardAmount: math.LegacyZeroDec(),
						Rewarded:     false,
					}
					err = f.keeper.Nomination.Set(ctx, nomID, nom)
					require.NoError(t, err)
				}

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "blog/post/999",
					Rationale:  "One more nomination",
				}, f
			},
			wantErr: true,
			errIs:   types.ErrMaxNominationsReached,
		},
		{
			name: "duplicate content ref",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx, _ := setupNominationSeason(t, f, TestAddrCreator)

				// Register a second member as the original nominator
				member1Str, err := f.addressCodec.BytesToString(TestAddrMember1)
				require.NoError(t, err)
				f.repKeeper.Members[member1Str] = true

				creatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
				require.NoError(t, err)

				// Pre-create a nomination with the same content_ref by a different member
				seqVal, err := f.keeper.NominationSeq.Next(ctx)
				require.NoError(t, err)
				nomID := seqVal + 1

				nom := types.Nomination{
					Id:           nomID,
					Nominator:    member1Str,
					ContentRef:   "blog/post/42",
					Rationale:    "Already nominated",
					Season:       1,
					TotalStaked:  math.LegacyZeroDec(),
					Conviction:   math.LegacyZeroDec(),
					RewardAmount: math.LegacyZeroDec(),
					Rewarded:     false,
				}
				err = f.keeper.Nomination.Set(ctx, nomID, nom)
				require.NoError(t, err)

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "blog/post/42", // same content ref
					Rationale:  "Duplicate attempt",
				}, f
			},
			wantErr: true,
			errIs:   types.ErrAlreadyNominated,
		},
		{
			name: "successful nomination",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx, creatorStr := setupNominationSeason(t, f, TestAddrCreator)

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "blog/post/7",
					Rationale:  "Outstanding forum contribution",
				}, f
			},
			wantErr: false,
			validate: func(t *testing.T, f *beginBlockFixture, ctx sdk.Context, resp *types.MsgNominateResponse) {
				t.Helper()

				creatorStr, err := f.addressCodec.BytesToString(TestAddrCreator)
				require.NoError(t, err)

				// Verify response contains a valid nomination ID
				require.Greater(t, resp.NominationId, uint64(0))

				// Verify nomination was stored correctly
				nom, err := f.keeper.Nomination.Get(ctx, resp.NominationId)
				require.NoError(t, err)
				require.Equal(t, resp.NominationId, nom.Id)
				require.Equal(t, creatorStr, nom.Nominator)
				require.Equal(t, "blog/post/7", nom.ContentRef)
				require.Equal(t, "Outstanding forum contribution", nom.Rationale)
				require.Equal(t, uint64(1), nom.Season)
				require.True(t, nom.TotalStaked.IsZero())
				require.True(t, nom.Conviction.IsZero())
				require.True(t, nom.RewardAmount.IsZero())
				require.False(t, nom.Rewarded)
				require.Greater(t, nom.CreatedAtBlock, int64(-1)) // block height is set

				// Verify event was emitted
				sdkCtx := sdk.UnwrapSDKContext(ctx)
				events := sdkCtx.EventManager().Events()
				found := false
				for _, event := range events {
					if event.Type == "nomination_created" {
						found = true
						for _, attr := range event.Attributes {
							switch attr.Key {
							case "nominator":
								require.Equal(t, creatorStr, attr.Value)
							case "content_ref":
								require.Equal(t, "blog/post/7", attr.Value)
							case "season":
								require.Equal(t, "1", attr.Value)
							}
						}
						break
					}
				}
				require.True(t, found, "nomination_created event should be emitted")
			},
		},
		{
			name: "successful nomination with various content ref formats",
			setup: func(t *testing.T) (keeper.Keeper, sdk.Context, *types.MsgNominate, *beginBlockFixture) {
				f := initBeginBlockFixture(t)
				ctx, creatorStr := setupNominationSeason(t, f, TestAddrCreator)

				return f.keeper, ctx, &types.MsgNominate{
					Creator:    creatorStr,
					ContentRef: "rep/initiative/42",
					Rationale:  "Initiative contribution",
				}, f
			},
			wantErr: false,
			validate: func(t *testing.T, f *beginBlockFixture, ctx sdk.Context, resp *types.MsgNominateResponse) {
				t.Helper()

				require.Greater(t, resp.NominationId, uint64(0))

				nom, err := f.keeper.Nomination.Get(ctx, resp.NominationId)
				require.NoError(t, err)
				require.Equal(t, "rep/initiative/42", nom.ContentRef)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k, ctx, msg, f := tc.setup(t)
			ms := keeper.NewMsgServerImpl(k)

			resp, err := ms.Nominate(ctx, msg)

			if tc.wantErr {
				require.Error(t, err)
				if tc.errIs != nil {
					require.ErrorIs(t, err, tc.errIs)
				}
				if tc.errContains != "" {
					require.Contains(t, err.Error(), tc.errContains)
				}
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tc.validate != nil {
					tc.validate(t, f, ctx, resp)
				}
			}
		})
	}
}
