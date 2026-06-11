package keeper

import (
	"context"
	"time"

	"cosmossdk.io/math"

	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	guardiantypes "sparkdream/x/guardian/types"
)

type msgServer struct {
	k *Keeper
}

// NewMsgServerImpl returns a guardian Msg server. Takes a pointer so any
// post-construction Set*Keeper updates are visible.
func NewMsgServerImpl(k *Keeper) guardiantypes.MsgServer {
	return &msgServer{k: k}
}

var _ guardiantypes.MsgServer = msgServer{}

// Exec is the universal guardian dispatch. Validates authority, unpacks the
// inner msg, applies the per-type filter, overwrites the inner Authority
// with guardian's module address, and routes via the msg service router.
func (ms msgServer) Exec(ctx context.Context, msg *guardiantypes.MsgExec) (*guardiantypes.MsgExecResponse, error) {
	if err := ms.k.authorityCheck(msg.Authority); err != nil {
		return nil, err
	}
	if msg.Inner == nil {
		return nil, errorsmod.Wrap(guardiantypes.ErrInnerMsgInvalid, "inner is nil")
	}

	// Unpack the inner Any into an sdk.Msg via the codec's interface registry.
	var inner sdk.Msg
	if err := ms.k.cdc.UnpackAny(msg.Inner, &inner); err != nil {
		return nil, errorsmod.Wrap(guardiantypes.ErrInnerMsgInvalid, err.Error())
	}

	guardianAddr := ModuleAddress()

	// Allowlist + per-type filter + authority substitution. New entries
	// here are spec changes; add only after a security review of what
	// fields must be locked.
	//
	// Note: bank.MsgSetDenomMetadata is intentionally NOT in this list
	// because cosmos-sdk v0.53.6's bank module does not expose it as a
	// gov-routable msg (no rpc declaration in bank/v1beta1/tx.proto).
	// Bank's SetDenomMetaData is keeper-only; identity protection for
	// native denom Symbol/Display is enforced by the bank-keeper wrapper
	// (app/bank_guard.go) and invariant 4 of x/identity. If a future
	// cosmos-sdk version adds MsgSetDenomMetadata, this switch must be
	// extended with the appropriate filter and AllowedMsgTypes updated.
	switch m := inner.(type) {

	case *banktypes.MsgSetSendEnabled:
		if err := ms.filterBankSetSendEnabled(ctx, m); err != nil {
			return nil, err
		}
		m.Authority = guardianAddr

	case *banktypes.MsgUpdateParams:
		// No field filter: bank params are non-critical for the
		// federation model. Gov retains control via guardian.
		m.Authority = guardianAddr

	case *minttypes.MsgUpdateParams:
		if err := ms.filterMintUpdateParams(ctx, m); err != nil {
			return nil, err
		}
		m.Authority = guardianAddr

	case *stakingtypes.MsgUpdateParams:
		if err := ms.filterStakingUpdateParams(ctx, m); err != nil {
			return nil, err
		}
		m.Authority = guardianAddr

	case *distrtypes.MsgCommunityPoolSpend:
		// Hard reject: x/split is the canonical revenue distribution path
		// (Community Pool → split → councils). Allowing gov to drain the
		// pool directly bypasses split's per-council allocation and
		// invalidates the tokenomics flow documented in docs/tokenomics.md.
		// Listed in the allowlist (vs. silently falling through to the
		// default arm) so the rejection surfaces as an explicit policy
		// statement in audit logs and error messages.
		return nil, errorsmod.Wrap(guardiantypes.ErrInnerMsgNotAllowed,
			"distribution.MsgCommunityPoolSpend is rejected by guardian: community pool spending must flow through x/split")

	case *distrtypes.MsgUpdateParams:
		if err := ms.filterDistrUpdateParams(ctx, m); err != nil {
			return nil, err
		}
		m.Authority = guardianAddr

	case *govtypesv1.MsgUpdateParams:
		if err := ms.filterGovUpdateParams(ctx, m); err != nil {
			return nil, err
		}
		m.Authority = guardianAddr

	case *slashingtypes.MsgUpdateParams:
		if err := ms.filterSlashingUpdateParams(ctx, m); err != nil {
			return nil, err
		}
		m.Authority = guardianAddr

	case *authtypes.MsgUpdateParams:
		if err := ms.filterAuthUpdateParams(ctx, m); err != nil {
			return nil, err
		}
		m.Authority = guardianAddr

	case *consensustypes.MsgUpdateParams:
		if err := ms.filterConsensusUpdateParams(m); err != nil {
			return nil, err
		}
		m.Authority = guardianAddr

	default:
		return nil, errorsmod.Wrapf(guardiantypes.ErrInnerMsgNotAllowed,
			"msg type %T (%s) is not in the guardian allowlist", inner, sdk.MsgTypeURL(inner))
	}

	// Route to the target module's handler. The router returns nil if no
	// handler is registered for this msg type.
	handler := ms.k.msgRouter.Handler(inner)
	if handler == nil {
		return nil, errorsmod.Wrapf(guardiantypes.ErrRoutingFailed,
			"no handler registered for %s", sdk.MsgTypeURL(inner))
	}
	resp, err := handler(sdk.UnwrapSDKContext(ctx), inner)
	if err != nil {
		return nil, errorsmod.Wrap(guardiantypes.ErrRoutingFailed, err.Error())
	}

	// Pack the response back into an Any so callers (gov) can inspect it
	// if they care.
	var innerResp *types.Any
	if resp != nil && len(resp.MsgResponses) > 0 {
		innerResp = resp.MsgResponses[0]
	}
	return &guardiantypes.MsgExecResponse{InnerResponse: innerResp}, nil
}

// --- per-msg-type filters ---------------------------------------------------

// filterBankSetSendEnabled rejects toggles that would disable send on the
// chain's native bond or dream denom. Other denoms (IBC vouchers, third-
// party-issued tokens) pass through: `SetSendEnabled` remains a legitimate
// emergency lever for them. Without identity wired, the filter is fail-
// closed (cannot recognize native denoms, so it cannot safely permit any
// SetSendEnabled).
func (ms msgServer) filterBankSetSendEnabled(ctx context.Context, m *banktypes.MsgSetSendEnabled) error {
	if ms.k.identityKeeper == nil {
		return errorsmod.Wrap(guardiantypes.ErrIdentityNotSealed,
			"identity keeper not wired into guardian; refusing to route bank.MsgSetSendEnabled")
	}
	sealed, err := ms.k.identityKeeper.GetSealedIdentity(ctx)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrIdentityNotSealed,
			"sealed-identity lookup failed: %v", err)
	}
	for _, se := range m.SendEnabled {
		if se == nil {
			continue
		}
		if (se.Denom == sealed.BondDenom || se.Denom == sealed.DreamDenom) && !se.Enabled {
			return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
				"bank.MsgSetSendEnabled: cannot disable send on native denom %q", se.Denom)
		}
	}
	for _, denom := range m.UseDefaultFor {
		// Falling back to default for a native denom only matters if
		// the bank default itself is `false`. Rather than reasoning
		// about which default is currently in effect, reject the toggle
		// on native denoms outright; gov can still adjust the default
		// for foreign denoms via MsgUpdateParams.
		if denom == sealed.BondDenom || denom == sealed.DreamDenom {
			return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
				"bank.MsgSetSendEnabled: native denom %q cannot be reverted to default (could disable send)", denom)
		}
	}
	return nil
}

// filterMintUpdateParams rejects changes to inflation_min, inflation_max,
// inflation_rate_change, goal_bonded, and mint_denom. blocks_per_year is
// operationally tunable.
//
// mint_denom is locked here for parity with staking.bond_denom (filtered
// in filterStakingUpdateParams). Both denoms are sealed at genesis by
// x/identity; identity invariant 5 detects drift but is warning-grade
// (stop=false), so the hard reject at this layer is the primary guard.
func (ms msgServer) filterMintUpdateParams(ctx context.Context, m *minttypes.MsgUpdateParams) error {
	if ms.k.mintKeeper == nil {
		return errorsmod.Wrap(guardiantypes.ErrImmutableField,
			"mint keeper not wired into guardian; refusing to route mint.MsgUpdateParams without filter context")
	}
	current, err := ms.k.mintKeeper.GetParams(ctx)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField, "current mint params lookup failed: %v", err)
	}
	if !m.Params.InflationMin.Equal(current.InflationMin) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"mint.MsgUpdateParams: inflation_min is immutable (current=%s, proposed=%s)",
			current.InflationMin, m.Params.InflationMin)
	}
	if !m.Params.InflationMax.Equal(current.InflationMax) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"mint.MsgUpdateParams: inflation_max is immutable (current=%s, proposed=%s)",
			current.InflationMax, m.Params.InflationMax)
	}
	if !m.Params.GoalBonded.Equal(current.GoalBonded) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"mint.MsgUpdateParams: goal_bonded is immutable (current=%s, proposed=%s)",
			current.GoalBonded, m.Params.GoalBonded)
	}
	if !m.Params.InflationRateChange.Equal(current.InflationRateChange) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"mint.MsgUpdateParams: inflation_rate_change is immutable (current=%s, proposed=%s)",
			current.InflationRateChange, m.Params.InflationRateChange)
	}
	if m.Params.MintDenom != current.MintDenom {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"mint.MsgUpdateParams: mint_denom is immutable (current=%q, proposed=%q)",
			current.MintDenom, m.Params.MintDenom)
	}
	// blocks_per_year: pass through. Operationally tunable.
	return nil
}

// filterStakingUpdateParams rejects changes to bond_denom. Other fields
// (unbonding_time, max_validators, max_entries, historical_entries,
// min_commission_rate, key_rotation_fee) are tunable.
func (ms msgServer) filterStakingUpdateParams(ctx context.Context, m *stakingtypes.MsgUpdateParams) error {
	if ms.k.stakingKeeper == nil {
		return errorsmod.Wrap(guardiantypes.ErrImmutableField,
			"staking keeper not wired into guardian; refusing to route staking.MsgUpdateParams without filter context")
	}
	current, err := ms.k.stakingKeeper.GetParams(ctx)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField, "current staking params lookup failed: %v", err)
	}
	if m.Params.BondDenom != current.BondDenom {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"staking.MsgUpdateParams: bond_denom is immutable (current=%q, proposed=%q)",
			current.BondDenom, m.Params.BondDenom)
	}
	return nil
}

// --- bounds for non-immutable params ---------------------------------------
//
// These constants pin floors and ceilings around the genesis-shipped values.
// They protect against governance setting params to degenerate values that
// would brick the chain (e.g., max_gas=1, voting_period=1s, community_tax=0).
// The floors are intentionally permissive: they allow normal operational
// retunes while blocking economic-attack and DOS-attack ranges. Changes
// require a chain upgrade (these are compiled constants, not gov-tunable).

var (
	// distribution.community_tax: genesis is 0.15. Floor at 0.05 keeps
	// councils funded; ceiling at 0.25 prevents starving validators.
	communityTaxFloor   = math.LegacyNewDecWithPrec(5, 2)
	communityTaxCeiling = math.LegacyNewDecWithPrec(25, 2)

	// gov.voting_period floor: 6h is short enough for emergencies, long
	// enough to prevent same-day surprise proposals.
	govVotingPeriodFloor          = 6 * time.Hour
	govExpeditedVotingPeriodFloor = 1 * time.Hour
	// gov.quorum / threshold / veto_threshold floors. Defaults are
	// 0.334 / 0.5 / 0.334; we floor at 0.2 / 0.5 / 0.2.
	govQuorumFloor        = math.LegacyNewDecWithPrec(20, 2)
	govThresholdFloor     = math.LegacyNewDecWithPrec(50, 2)
	govVetoThresholdFloor = math.LegacyNewDecWithPrec(20, 2)

	// slashing fractions: defaults 0.05 (double-sign) and 0.0001 (downtime).
	// Floors prevent gov from setting them to zero.
	slashFractionDoubleSignFloor = math.LegacyNewDecWithPrec(1, 2) // 1%
	slashFractionDowntimeFloor   = math.LegacyNewDecWithPrec(1, 5) // 0.00001
	signedBlocksWindowCeiling    = int64(1_000_000)

	// auth gas costs: floors at the genesis defaults (10 per byte, 590 ed,
	// 1000 secp). Setting these to zero makes txs effectively free.
	authTxSizeCostPerByteFloor      = uint64(1)
	authSigVerifyCostEd25519Floor   = uint64(100)
	authSigVerifyCostSecp256k1Floor = uint64(100)

	// consensus floors / ceilings. block.max_gas == -1 (unlimited) is
	// allowed; any other value must be at least minMaxGas. max_bytes must
	// be at least minMaxBytes to avoid bricking block production.
	consensusMinMaxBytes            = int64(200_000)
	consensusMinMaxGas              = int64(1_000_000) // -1 (unlimited) also allowed
	consensusMinEvidenceAgeBlocks   = int64(1_000)
	consensusMinEvidenceAgeDuration = 1 * time.Hour
)

// filterDistrUpdateParams keeps community_tax inside [floor, ceiling].
// withdraw_addr_enabled and the deprecated proposer-reward fields are
// passthrough.
func (ms msgServer) filterDistrUpdateParams(ctx context.Context, m *distrtypes.MsgUpdateParams) error {
	if m.Params.CommunityTax.LT(communityTaxFloor) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"distribution.MsgUpdateParams: community_tax below floor (proposed=%s, floor=%s)",
			m.Params.CommunityTax, communityTaxFloor)
	}
	if m.Params.CommunityTax.GT(communityTaxCeiling) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"distribution.MsgUpdateParams: community_tax above ceiling (proposed=%s, ceiling=%s)",
			m.Params.CommunityTax, communityTaxCeiling)
	}
	return nil
}

// filterGovUpdateParams enforces floors on voting_period, expedited_voting_
// period, quorum, threshold, and veto_threshold. Caller may still raise
// them; the floors only block degenerate values that would let a hostile
// majority ram through proposals before the broader community can react.
func (ms msgServer) filterGovUpdateParams(ctx context.Context, m *govtypesv1.MsgUpdateParams) error {
	if m.Params.VotingPeriod == nil || *m.Params.VotingPeriod < govVotingPeriodFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: voting_period below floor (proposed=%v, floor=%v)",
			m.Params.VotingPeriod, govVotingPeriodFloor)
	}
	if m.Params.ExpeditedVotingPeriod == nil || *m.Params.ExpeditedVotingPeriod < govExpeditedVotingPeriodFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: expedited_voting_period below floor (proposed=%v, floor=%v)",
			m.Params.ExpeditedVotingPeriod, govExpeditedVotingPeriodFloor)
	}
	quorum, err := math.LegacyNewDecFromStr(m.Params.Quorum)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: quorum is not a valid decimal: %v", err)
	}
	if quorum.LT(govQuorumFloor) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: quorum below floor (proposed=%s, floor=%s)", quorum, govQuorumFloor)
	}
	threshold, err := math.LegacyNewDecFromStr(m.Params.Threshold)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: threshold is not a valid decimal: %v", err)
	}
	if threshold.LT(govThresholdFloor) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: threshold below floor (proposed=%s, floor=%s)", threshold, govThresholdFloor)
	}
	veto, err := math.LegacyNewDecFromStr(m.Params.VetoThreshold)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: veto_threshold is not a valid decimal: %v", err)
	}
	if veto.LT(govVetoThresholdFloor) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: veto_threshold below floor (proposed=%s, floor=%s)", veto, govVetoThresholdFloor)
	}
	return nil
}

// filterSlashingUpdateParams enforces nonzero slash fractions and a sane
// upper bound on signed_blocks_window. Setting either fraction to zero
// would disable the matching slash path entirely, defeating the
// accountability story.
func (ms msgServer) filterSlashingUpdateParams(ctx context.Context, m *slashingtypes.MsgUpdateParams) error {
	if m.Params.SlashFractionDoubleSign.LT(slashFractionDoubleSignFloor) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: slash_fraction_double_sign below floor (proposed=%s, floor=%s)",
			m.Params.SlashFractionDoubleSign, slashFractionDoubleSignFloor)
	}
	if m.Params.SlashFractionDowntime.LT(slashFractionDowntimeFloor) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: slash_fraction_downtime below floor (proposed=%s, floor=%s)",
			m.Params.SlashFractionDowntime, slashFractionDowntimeFloor)
	}
	if m.Params.SignedBlocksWindow > signedBlocksWindowCeiling {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: signed_blocks_window above ceiling (proposed=%d, ceiling=%d)",
			m.Params.SignedBlocksWindow, signedBlocksWindowCeiling)
	}
	return nil
}

// filterAuthUpdateParams enforces minimum gas costs so gov cannot make
// txs effectively free (a DOS surface).
func (ms msgServer) filterAuthUpdateParams(ctx context.Context, m *authtypes.MsgUpdateParams) error {
	if m.Params.TxSizeCostPerByte < authTxSizeCostPerByteFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"auth.MsgUpdateParams: tx_size_cost_per_byte below floor (proposed=%d, floor=%d)",
			m.Params.TxSizeCostPerByte, authTxSizeCostPerByteFloor)
	}
	if m.Params.SigVerifyCostED25519 < authSigVerifyCostEd25519Floor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"auth.MsgUpdateParams: sig_verify_cost_ed25519 below floor (proposed=%d, floor=%d)",
			m.Params.SigVerifyCostED25519, authSigVerifyCostEd25519Floor)
	}
	if m.Params.SigVerifyCostSecp256k1 < authSigVerifyCostSecp256k1Floor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"auth.MsgUpdateParams: sig_verify_cost_secp256k1 below floor (proposed=%d, floor=%d)",
			m.Params.SigVerifyCostSecp256k1, authSigVerifyCostSecp256k1Floor)
	}
	return nil
}

// filterConsensusUpdateParams enforces ABCI / cometbft floors. Consensus
// params do not live in the SDK keeper (cometbft owns them), so this
// filter validates the proposed values directly against absolute floors
// rather than comparing to current.
//
// All four fields are required by the proto contract ("NOTE: All parameters
// must be supplied"), so nil is treated as an error.
func (ms msgServer) filterConsensusUpdateParams(m *consensustypes.MsgUpdateParams) error {
	if m.Block == nil {
		return errorsmod.Wrap(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: block params required")
	}
	if m.Block.MaxBytes < consensusMinMaxBytes {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: block.max_bytes below floor (proposed=%d, floor=%d)",
			m.Block.MaxBytes, consensusMinMaxBytes)
	}
	// max_gas == -1 means unlimited; anything else must clear the floor.
	if m.Block.MaxGas != -1 && m.Block.MaxGas < consensusMinMaxGas {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: block.max_gas must be -1 (unlimited) or >= %d (proposed=%d)",
			consensusMinMaxGas, m.Block.MaxGas)
	}
	if m.Evidence == nil {
		return errorsmod.Wrap(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: evidence params required")
	}
	if m.Evidence.MaxAgeNumBlocks < consensusMinEvidenceAgeBlocks {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: evidence.max_age_num_blocks below floor (proposed=%d, floor=%d)",
			m.Evidence.MaxAgeNumBlocks, consensusMinEvidenceAgeBlocks)
	}
	if m.Evidence.MaxAgeDuration < consensusMinEvidenceAgeDuration {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: evidence.max_age_duration below floor (proposed=%v, floor=%v)",
			m.Evidence.MaxAgeDuration, consensusMinEvidenceAgeDuration)
	}
	if m.Validator == nil {
		return errorsmod.Wrap(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: validator params required")
	}
	// validator.pub_key_types: passthrough (operationally tunable).
	// abci params: passthrough (legacy field, mostly empty in v0.50+).
	return nil
}
