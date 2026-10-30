package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgExtendSeason extends the current season through an executed
// x/commons proposal (council.go), one step per call. An extension only
// moves the end block later, so it never starts a transition mid-run. It
// needs an ACTIVE or NOMINATION season with extensions left; one proposal
// is open at a time.
func SimulateMsgExtendSeason(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgExtendSeason{}, decodeAs[types.MsgExtendSeason](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			if len(taken) > 0 {
				return nil, "an extension is already proposed", nil
			}
			season, err := e.k.Season.Get(e.ctx)
			if err != nil {
				return nil, "", err
			}
			if season.Status != types.SeasonStatus_SEASON_STATUS_ACTIVE &&
				season.Status != types.SeasonStatus_SEASON_STATUS_NOMINATION {
				return nil, "season is not active: " + season.Status.String(), nil
			}
			params, err := e.k.Params.Get(e.ctx)
			if err != nil {
				return nil, "", err
			}
			if season.ExtensionsCount >= uint64(params.MaxSeasonExtensions) {
				return nil, "no season extension left", nil
			}
			if params.MaxExtensionEpochs == 0 {
				return nil, "max_extension_epochs is zero", nil
			}
			maxEpochs := min(params.MaxExtensionEpochs, 3)
			return &types.MsgExtendSeason{
				Authority:       policy,
				ExtensionEpochs: 1 + commontypes.Saturate[uint64](e.r.Int63n(commontypes.Saturate[int64](maxEpochs))),
				Reason:          "simulation: more time for the " + pickName(e) + " initiatives",
			}, "", nil
		})
}
