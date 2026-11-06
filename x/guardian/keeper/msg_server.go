package keeper

import (
	"context"
	"errors"
	"time"

	"cosmossdk.io/collections"
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
		if err := ms.filterBankUpdateParams(m); err != nil {
			return nil, err
		}
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
		if err := ms.filterConsensusUpdateParams(ctx, m); err != nil {
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
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	resp, err := handler(sdkCtx, inner)
	if err != nil {
		return nil, errorsmod.Wrap(guardiantypes.ErrRoutingFailed, err.Error())
	}
	// The router hands the inner handler a fresh EventManager; re-emit its
	// events so indexers see the params update (SES-06).
	if resp != nil {
		sdkCtx.EventManager().EmitEvents(resp.GetEvents())
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

// filterBankUpdateParams refuses default_send_enabled=false. The network
// geneses ship `send_enabled: []`, so the native bond and dream denoms read
// the bank default: flipping it would disable every native MsgSend, the exact
// outcome filterBankSetSendEnabled exists to prevent. The refusal is
// unconditional (no "unless explicit native entries exist" carve-out), so the
// filter needs no bank keeper. Gov can still disable a foreign denom with
// MsgSetSendEnabled. The legacy params.send_enabled list is refused when it
// disables anything; bank itself rejects a non-empty list since v0.47.
func (ms msgServer) filterBankUpdateParams(m *banktypes.MsgUpdateParams) error {
	if !m.Params.DefaultSendEnabled {
		return errorsmod.Wrap(guardiantypes.ErrImmutableField,
			"bank.MsgUpdateParams: default_send_enabled=false would disable send on the native denoms")
	}
	for _, se := range m.Params.SendEnabled { //nolint:staticcheck // legacy field, still decoded
		if se != nil && !se.Enabled {
			return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
				"bank.MsgUpdateParams: legacy send_enabled entry disables %q; use MsgSetSendEnabled", se.Denom)
		}
	}
	return nil
}

// filterMintUpdateParams rejects changes to inflation_min, inflation_max,
// inflation_rate_change, goal_bonded, and mint_denom, and bounds
// blocks_per_year (see below).
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
	if err := requireDecs("mint.MsgUpdateParams", []namedDec{
		{"inflation_min", m.Params.InflationMin}, {"inflation_max", m.Params.InflationMax},
		{"goal_bonded", m.Params.GoalBonded}, {"inflation_rate_change", m.Params.InflationRateChange},
	}); err != nil {
		return err
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
	// blocks_per_year is the divisor of per-block issuance
	// (BlockProvision = AnnualProvisions / BlocksPerYear). Left free, it
	// defeats every locked field above: 1 mints a year of inflation per
	// block, u64-max mints nothing. It stays tunable so the issuance rate
	// can follow a real change in block time, but only inside a compiled
	// absolute range, by at most ±50% of the current value, and at most once
	// per mintBlocksPerYearChangeCooldown (GRD-11: the ±50% step compares
	// with the stored value, so without the cooldown several messages in
	// one proposal, or several proposals ending together, would compound
	// the step and cross the whole range in one vote).
	bpy := m.Params.BlocksPerYear
	if bpy < mintBlocksPerYearFloor || bpy > mintBlocksPerYearCeiling {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"mint.MsgUpdateParams: blocks_per_year outside [%d, %d] (proposed=%d)",
			mintBlocksPerYearFloor, mintBlocksPerYearCeiling, bpy)
	}
	if cur := current.BlocksPerYear; bpy != cur {
		// Allowed band: [cur/2, cur*3/2]. bpy*2 cannot overflow (bpy is at
		// most the ceiling); cur+cur/2 only overflows for cur > ~1.2e19,
		// where the lower-bound test has already refused.
		if bpy*2 < cur || bpy > cur+cur/2 {
			return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
				"mint.MsgUpdateParams: blocks_per_year may change by at most 50%% per proposal (current=%d, proposed=%d)",
				cur, bpy)
		}
		now := sdk.UnwrapSDKContext(ctx).BlockTime()
		// Fail closed: only a missing record means "never changed". Any
		// other store error refuses the change rather than waiving the
		// cooldown.
		last, err := ms.k.LastBlocksPerYearChange.Get(ctx)
		switch {
		case err == nil:
			if next := time.Unix(last, 0).Add(mintBlocksPerYearChangeCooldown); now.Before(next) {
				return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
					"mint.MsgUpdateParams: blocks_per_year may change at most once per %v (next change allowed at %s)",
					mintBlocksPerYearChangeCooldown, next.UTC().Format(time.RFC3339))
			}
		case !errors.Is(err, collections.ErrNotFound):
			return errorsmod.Wrap(err, "mint.MsgUpdateParams: blocks_per_year cooldown read")
		}
		// Written in the proposal's execution context: if the inner update
		// fails, gov discards this write with it.
		if err := ms.k.LastBlocksPerYearChange.Set(ctx, now.Unix()); err != nil {
			return err
		}
	}
	return nil
}

// filterStakingUpdateParams rejects changes to bond_denom and keeps the
// other fields in two-sided bounds: unbonding_time between a floor (a
// validator that can leave instantly escapes double-sign slashing), the
// current consensus evidence age (the SDK's requirement, checked from this
// side too) and a ceiling (unbonding entries keep their completion time, so
// a years-long period strands every delegator who unbonds before it is
// reverted); max_validators above a floor (a tiny set centralises
// consensus); max_entries, historical_entries and min_commission_rate
// inside operational ranges.
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
	if m.Params.UnbondingTime < stakingUnbondingTimeFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"staking.MsgUpdateParams: unbonding_time below floor (proposed=%v, floor=%v)",
			m.Params.UnbondingTime, stakingUnbondingTimeFloor)
	}
	if m.Params.UnbondingTime > stakingUnbondingTimeCeiling {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"staking.MsgUpdateParams: unbonding_time above ceiling (proposed=%v, ceiling=%v)",
			m.Params.UnbondingTime, stakingUnbondingTimeCeiling)
	}
	if ev := sdk.UnwrapSDKContext(ctx).ConsensusParams().Evidence; ev != nil && m.Params.UnbondingTime < ev.MaxAgeDuration {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"staking.MsgUpdateParams: unbonding_time below consensus evidence.max_age_duration (proposed=%v, evidence=%v)",
			m.Params.UnbondingTime, ev.MaxAgeDuration)
	}
	if m.Params.MaxValidators < stakingMaxValidatorsFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"staking.MsgUpdateParams: max_validators below floor (proposed=%d, floor=%d)",
			m.Params.MaxValidators, stakingMaxValidatorsFloor)
	}
	if m.Params.MaxEntries < stakingMaxEntriesFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"staking.MsgUpdateParams: max_entries below floor (proposed=%d, floor=%d)",
			m.Params.MaxEntries, stakingMaxEntriesFloor)
	}
	if m.Params.HistoricalEntries > stakingHistoricalEntriesCeiling {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"staking.MsgUpdateParams: historical_entries above ceiling (proposed=%d, ceiling=%d)",
			m.Params.HistoricalEntries, stakingHistoricalEntriesCeiling)
	}
	if err := requireDecs("staking.MsgUpdateParams", []namedDec{{"min_commission_rate", m.Params.MinCommissionRate}}); err != nil {
		return err
	}
	if m.Params.MinCommissionRate.GT(stakingMinCommissionRateCeiling) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"staking.MsgUpdateParams: min_commission_rate above ceiling (proposed=%s, ceiling=%s)",
			m.Params.MinCommissionRate, stakingMinCommissionRateCeiling)
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

	// gov.voting_period / expedited_voting_period floors are per network
	// (NetworkBounds in network_bounds.go): 6 h / 1 h on testnet and
	// mainnet, lower on devnet whose genesis ships 1 h / 30 min.
	// gov.voting_period ceiling: a captured gov could otherwise set
	// decades and freeze itself (and so guardian) for good. Genesis is
	// 5 days (mainnet); 28 days leaves room for slow deliberation.
	govVotingPeriodCeiling          = 28 * 24 * time.Hour
	govExpeditedVotingPeriodCeiling = 28 * 24 * time.Hour
	// gov.quorum / threshold / veto_threshold floors. Defaults are
	// 0.334 / 0.5 / 0.334; we floor at 0.2 / 0.5 / 0.2.
	govQuorumFloor        = math.LegacyNewDecWithPrec(20, 2)
	govThresholdFloor     = math.LegacyNewDecWithPrec(50, 2)
	govVetoThresholdFloor = math.LegacyNewDecWithPrec(20, 2)
	// ... and ceilings, so gov cannot freeze itself: quorum 1 needs every
	// bonded token to vote, a threshold near 1 never passes. Genesis is
	// 0.334 / 0.5 / 0.334 with expedited_threshold 0.667, which keeps its
	// supermajority floor.
	govQuorumCeiling           = math.LegacyNewDecWithPrec(50, 2)
	govThresholdCeiling        = math.LegacyNewDecWithPrec(67, 2)
	govVetoThresholdCeiling    = math.LegacyNewDecWithPrec(50, 2)
	govExpeditedThresholdFloor = math.LegacyNewDecWithPrec(667, 3)
	// min_deposit_ratio is parsed by every MsgSubmitProposal and MsgDeposit
	// (SDK AddDeposit) but not validated upstream: an unparseable value
	// freezes all proposal submission. Genesis is 0.01; it is the effective
	// anti-spam gate (min_initial_deposit_ratio is 0).
	govMinDepositRatioFloor = math.LegacyNewDecWithPrec(1, 3)
	// gov deposits: an absolute floor on the native min_deposit (genesis is
	// 50 SPARK; 5 SPARK at 6 decimals), and a ceiling on the deposit period
	// (genesis 2 days on mainnet; the e2e chain uses 30 s, so no floor).
	govMinDepositFloor         = math.NewInt(5_000_000)
	govMaxDepositPeriodCeiling = 28 * 24 * time.Hour

	// slashing fractions: defaults 0.05 (double-sign) and 0.0001 (downtime).
	// Floors prevent gov from setting them to zero.
	slashFractionDoubleSignFloor = math.LegacyNewDecWithPrec(1, 2) // 1%
	slashFractionDowntimeFloor   = math.LegacyNewDecWithPrec(1, 5) // 0.00001
	signedBlocksWindowCeiling    = int64(1_000_000)
	// Ceilings keep enforcement from turning punitive (genesis 0.05 / 0.01 /
	// 0.5 / 10 min).
	slashFractionDoubleSignCeiling = math.LegacyNewDecWithPrec(33, 2)
	slashFractionDowntimeCeiling   = math.LegacyNewDecWithPrec(5, 2)
	slashMinSignedPerWindowCeiling = math.LegacyNewDecWithPrec(95, 2)
	slashDowntimeJailCeiling       = 30 * 24 * time.Hour
	// Downtime enforcement is the pair (min_signed_per_window, jail): a
	// zero min_signed_per_window never jails, and a near-zero jail lets an
	// offline validator rejoin at once. Genesis is 0.5 / 600s / window 100.
	slashMinSignedPerWindowFloor = math.LegacyNewDecWithPrec(5, 2) // 5%
	slashDowntimeJailFloor       = 10 * time.Minute
	signedBlocksWindowFloor      = int64(100)

	// staking: genesis unbonding_time is 21 days and max_validators 100.
	// The unbonding floor stays well above the consensus evidence-age floor
	// so double-sign evidence can always reach a still-bonded validator.
	stakingUnbondingTimeFloor = 7 * 24 * time.Hour
	stakingMaxValidatorsFloor = uint32(20)
	// Two-sided ranges around genesis (21 days, 7 entries, 10,000
	// historical entries, 0 commission).
	stakingUnbondingTimeCeiling     = 60 * 24 * time.Hour
	stakingMaxEntriesFloor          = uint32(7)
	stakingHistoricalEntriesCeiling = uint32(100_000)
	stakingMinCommissionRateCeiling = math.LegacyNewDecWithPrec(20, 2)

	// mint.blocks_per_year: [10 s blocks, 0.5 s blocks]. Genesis (and the
	// SDK default) is 6_311_520, about 5 s blocks.
	mintBlocksPerYearFloor   = uint64(3_153_600)
	mintBlocksPerYearCeiling = uint64(63_072_000)
	// At most one blocks_per_year change per week (GRD-11).
	mintBlocksPerYearChangeCooldown = 7 * 24 * time.Hour

	// auth gas costs: floors at the genesis defaults (10 per byte, 590 ed,
	// 1000 secp). Setting these to zero makes txs effectively free.
	authTxSizeCostPerByteFloor      = uint64(1)
	authSigVerifyCostEd25519Floor   = uint64(100)
	authSigVerifyCostSecp256k1Floor = uint64(100)
	// tx_sig_limit counts every sub-key of a multisig account (CountSubKeys),
	// so lowering it below the genesis value of 7 would lock out existing
	// native-multisig members (multisig-accounts plan §3.2).
	authTxSigLimitFloor = uint64(7)
	// Ceilings (GRD-09): about 10x genesis. A cost so high that no signed
	// tx fits in a block stops every tx, Technical Council votes included,
	// so not even an on-chain upgrade could repair it.
	authTxSizeCostPerByteCeiling      = uint64(100)
	authSigVerifyCostEd25519Ceiling   = uint64(10_000)
	authSigVerifyCostSecp256k1Ceiling = uint64(20_000)
	authTxSigLimitCeiling             = uint64(100)
	authMaxMemoCharactersCeiling      = uint64(10_000)

	// consensus floors / ceilings. block.max_gas must be finite (-1 means
	// unlimited, which leaves only max_bytes to bound per-block execution)
	// and at least minMaxGas. max_bytes must be at least minMaxBytes to
	// avoid bricking block production.
	consensusMinMaxBytes            = int64(200_000)
	consensusMinMaxGas              = int64(1_000_000)
	consensusMaxMaxGas              = int64(1_000_000_000) // 10x genesis
	consensusMinEvidenceAgeBlocks   = int64(1_000)
	consensusMinEvidenceAgeDuration = 1 * time.Hour
	consensusRequiredPubKeyType     = "ed25519"
)

// filterDistrUpdateParams keeps community_tax inside [floor, ceiling].
// withdraw_addr_enabled and the deprecated proposer-reward fields are
// passthrough.
func (ms msgServer) filterDistrUpdateParams(ctx context.Context, m *distrtypes.MsgUpdateParams) error {
	if err := requireDecs("distribution.MsgUpdateParams", []namedDec{{"community_tax", m.Params.CommunityTax}}); err != nil {
		return err
	}
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

// filterGovUpdateParams enforces floors on voting_period and expedited_voting_
// period (per network, NetworkBounds), quorum, threshold, and veto_threshold, ceilings on both voting
// periods, and a positive native-denom min_deposit / expedited_min_deposit.
// The floors block degenerate values that would let a hostile majority ram
// through proposals before the broader community can react; the ceilings
// stop a captured gov from freezing itself (and guardian) for years.
func (ms msgServer) filterGovUpdateParams(ctx context.Context, m *govtypesv1.MsgUpdateParams) error {
	floors := ms.k.bounds
	if m.Params.VotingPeriod == nil || *m.Params.VotingPeriod < floors.GovVotingPeriodFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: voting_period below floor (proposed=%v, floor=%v)",
			m.Params.VotingPeriod, floors.GovVotingPeriodFloor)
	}
	if *m.Params.VotingPeriod > govVotingPeriodCeiling {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: voting_period above ceiling (proposed=%v, ceiling=%v)",
			*m.Params.VotingPeriod, govVotingPeriodCeiling)
	}
	if m.Params.ExpeditedVotingPeriod == nil || *m.Params.ExpeditedVotingPeriod < floors.GovExpeditedVotingPeriodFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: expedited_voting_period below floor (proposed=%v, floor=%v)",
			m.Params.ExpeditedVotingPeriod, floors.GovExpeditedVotingPeriodFloor)
	}
	if *m.Params.ExpeditedVotingPeriod > govExpeditedVotingPeriodCeiling {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: expedited_voting_period above ceiling (proposed=%v, ceiling=%v)",
			*m.Params.ExpeditedVotingPeriod, govExpeditedVotingPeriodCeiling)
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
	if quorum.GT(govQuorumCeiling) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: quorum above ceiling (proposed=%s, ceiling=%s)", quorum, govQuorumCeiling)
	}
	if threshold.GT(govThresholdCeiling) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: threshold above ceiling (proposed=%s, ceiling=%s)", threshold, govThresholdCeiling)
	}
	if veto.GT(govVetoThresholdCeiling) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: veto_threshold above ceiling (proposed=%s, ceiling=%s)", veto, govVetoThresholdCeiling)
	}
	expedited, err := math.LegacyNewDecFromStr(m.Params.ExpeditedThreshold)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: expedited_threshold is not a valid decimal: %v", err)
	}
	if expedited.LT(govExpeditedThresholdFloor) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: expedited_threshold below floor (proposed=%s, floor=%s)", expedited, govExpeditedThresholdFloor)
	}
	// min_deposit_ratio is parsed on every deposit and submission; SDK
	// ValidateBasic never checks it (GRD-07).
	ratio, err := math.LegacyNewDecFromStr(m.Params.MinDepositRatio)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: min_deposit_ratio is not a valid decimal: %v", err)
	}
	if ratio.LT(govMinDepositRatioFloor) || ratio.GT(math.LegacyOneDec()) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: min_deposit_ratio outside [%s, 1] (proposed=%s)", govMinDepositRatioFloor, ratio)
	}
	initialRatio, err := math.LegacyNewDecFromStr(m.Params.MinInitialDepositRatio)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: min_initial_deposit_ratio is not a valid decimal: %v", err)
	}
	if initialRatio.IsNegative() || initialRatio.GT(math.LegacyOneDec()) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: min_initial_deposit_ratio outside [0, 1] (proposed=%s)", initialRatio)
	}
	if m.Params.MaxDepositPeriod == nil || *m.Params.MaxDepositPeriod > govMaxDepositPeriodCeiling {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: max_deposit_period must be set and at most %v (proposed=%v)",
			govMaxDepositPeriodCeiling, m.Params.MaxDepositPeriod)
	}
	// min_deposit / expedited_min_deposit: an empty or zero deposit makes
	// proposals free (spam). Both must carry a positive amount of the
	// native bond denom, which needs the sealed identity (fail closed).
	minDeposit, err := ms.checkGovDeposit(ctx, "min_deposit", m.Params.MinDeposit, govMinDepositFloor)
	if err != nil {
		return err
	}
	if _, err := ms.checkGovDeposit(ctx, "expedited_min_deposit", m.Params.ExpeditedMinDeposit, minDeposit); err != nil {
		return err
	}
	return nil
}

// checkGovDeposit requires a gov deposit param to carry at least floor of
// the native bond denom, and returns that amount.
func (ms msgServer) checkGovDeposit(ctx context.Context, field string, deposit sdk.Coins, floor math.Int) (math.Int, error) {
	if ms.k.identityKeeper == nil {
		return math.Int{}, errorsmod.Wrapf(guardiantypes.ErrIdentityNotSealed,
			"identity keeper not wired into guardian; refusing to route gov.MsgUpdateParams (%s check)", field)
	}
	sealed, err := ms.k.identityKeeper.GetSealedIdentity(ctx)
	if err != nil {
		return math.Int{}, errorsmod.Wrapf(guardiantypes.ErrIdentityNotSealed, "sealed-identity lookup failed: %v", err)
	}
	amount := deposit.AmountOf(sealed.BondDenom)
	if !amount.IsPositive() || amount.LT(floor) {
		return math.Int{}, errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"gov.MsgUpdateParams: %s must include at least %s%s (proposed=%s)",
			field, floor, sealed.BondDenom, deposit)
	}
	return amount, nil
}

// filterSlashingUpdateParams enforces nonzero slash fractions, a bounded
// signed_blocks_window, and floors on min_signed_per_window and
// downtime_jail_duration. Setting either fraction to zero would disable the
// matching slash path entirely; a zero min_signed_per_window or a
// near-zero jail does the same to downtime enforcement while the fraction
// floor is still met.
func (ms msgServer) filterSlashingUpdateParams(ctx context.Context, m *slashingtypes.MsgUpdateParams) error {
	if err := requireDecs("slashing.MsgUpdateParams", []namedDec{
		{"slash_fraction_double_sign", m.Params.SlashFractionDoubleSign},
		{"slash_fraction_downtime", m.Params.SlashFractionDowntime},
		{"min_signed_per_window", m.Params.MinSignedPerWindow},
	}); err != nil {
		return err
	}
	if m.Params.SlashFractionDoubleSign.GT(slashFractionDoubleSignCeiling) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: slash_fraction_double_sign above ceiling (proposed=%s, ceiling=%s)",
			m.Params.SlashFractionDoubleSign, slashFractionDoubleSignCeiling)
	}
	if m.Params.SlashFractionDowntime.GT(slashFractionDowntimeCeiling) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: slash_fraction_downtime above ceiling (proposed=%s, ceiling=%s)",
			m.Params.SlashFractionDowntime, slashFractionDowntimeCeiling)
	}
	if m.Params.MinSignedPerWindow.GT(slashMinSignedPerWindowCeiling) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: min_signed_per_window above ceiling (proposed=%s, ceiling=%s)",
			m.Params.MinSignedPerWindow, slashMinSignedPerWindowCeiling)
	}
	if m.Params.DowntimeJailDuration > slashDowntimeJailCeiling {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: downtime_jail_duration above ceiling (proposed=%v, ceiling=%v)",
			m.Params.DowntimeJailDuration, slashDowntimeJailCeiling)
	}
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
	if m.Params.SignedBlocksWindow < signedBlocksWindowFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: signed_blocks_window below floor (proposed=%d, floor=%d)",
			m.Params.SignedBlocksWindow, signedBlocksWindowFloor)
	}
	if m.Params.MinSignedPerWindow.IsNil() || m.Params.MinSignedPerWindow.LT(slashMinSignedPerWindowFloor) {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: min_signed_per_window below floor (proposed=%s, floor=%s)",
			m.Params.MinSignedPerWindow, slashMinSignedPerWindowFloor)
	}
	if m.Params.DowntimeJailDuration < slashDowntimeJailFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"slashing.MsgUpdateParams: downtime_jail_duration below floor (proposed=%v, floor=%v)",
			m.Params.DowntimeJailDuration, slashDowntimeJailFloor)
	}
	return nil
}

// filterAuthUpdateParams enforces minimum gas costs so gov cannot make
// txs effectively free (a DOS surface), and a tx_sig_limit floor so gov
// cannot lock out native-multisig accounts.
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
	if m.Params.TxSigLimit < authTxSigLimitFloor {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"auth.MsgUpdateParams: tx_sig_limit below floor (proposed=%d, floor=%d)",
			m.Params.TxSigLimit, authTxSigLimitFloor)
	}
	for _, c := range []struct {
		name           string
		value, ceiling uint64
	}{
		{"tx_size_cost_per_byte", m.Params.TxSizeCostPerByte, authTxSizeCostPerByteCeiling},
		{"sig_verify_cost_ed25519", m.Params.SigVerifyCostED25519, authSigVerifyCostEd25519Ceiling},
		{"sig_verify_cost_secp256k1", m.Params.SigVerifyCostSecp256k1, authSigVerifyCostSecp256k1Ceiling},
		{"tx_sig_limit", m.Params.TxSigLimit, authTxSigLimitCeiling},
		{"max_memo_characters", m.Params.MaxMemoCharacters, authMaxMemoCharactersCeiling},
	} {
		if c.value > c.ceiling {
			return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
				"auth.MsgUpdateParams: %s above ceiling (proposed=%d, ceiling=%d)", c.name, c.value, c.ceiling)
		}
	}
	return nil
}

// filterConsensusUpdateParams enforces ABCI / cometbft floors. Consensus
// params do not live in the SDK keeper (cometbft owns them), so this
// filter validates the proposed values directly against absolute floors
// rather than comparing to current. The one cross-module check reads the
// staking keeper: evidence must stay admissible for no longer than the
// unbonding period (the SDK's documented requirement; a validator that has
// unbonded cannot be slashed for it).
//
// All four fields are required by the proto contract ("NOTE: All parameters
// must be supplied"), so nil is treated as an error.
func (ms msgServer) filterConsensusUpdateParams(ctx context.Context, m *consensustypes.MsgUpdateParams) error {
	if m.Block == nil {
		return errorsmod.Wrap(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: block params required")
	}
	if m.Block.MaxBytes < consensusMinMaxBytes {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: block.max_bytes below floor (proposed=%d, floor=%d)",
			m.Block.MaxBytes, consensusMinMaxBytes)
	}
	// max_gas must be finite: -1 (unlimited) is refused.
	if m.Block.MaxGas < consensusMinMaxGas || m.Block.MaxGas > consensusMaxMaxGas {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: block.max_gas must be finite and within [%d, %d] (proposed=%d)",
			consensusMinMaxGas, consensusMaxMaxGas, m.Block.MaxGas)
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
	if ms.k.stakingKeeper == nil {
		return errorsmod.Wrap(guardiantypes.ErrImmutableField,
			"staking keeper not wired into guardian; refusing to route consensus.MsgUpdateParams without filter context")
	}
	stakingParams, err := ms.k.stakingKeeper.GetParams(ctx)
	if err != nil {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField, "current staking params lookup failed: %v", err)
	}
	if m.Evidence.MaxAgeDuration > stakingParams.UnbondingTime {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: evidence.max_age_duration exceeds staking unbonding_time (proposed=%v, unbonding=%v)",
			m.Evidence.MaxAgeDuration, stakingParams.UnbondingTime)
	}
	if m.Validator == nil {
		return errorsmod.Wrap(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: validator params required")
	}
	// validator.pub_key_types must keep ed25519: every validator's consensus
	// key is ed25519, and CometBFT refuses a validator update whose key type
	// is not listed, which halts the chain at the next set change.
	hasEd25519 := false
	for _, t := range m.Validator.PubKeyTypes {
		if t == consensusRequiredPubKeyType {
			hasEd25519 = true
			break
		}
	}
	if !hasEd25519 {
		return errorsmod.Wrapf(guardiantypes.ErrImmutableField,
			"consensus.MsgUpdateParams: validator.pub_key_types must include %q (proposed=%v)",
			consensusRequiredPubKeyType, m.Validator.PubKeyTypes)
	}
	// abci params: passthrough (legacy field, mostly empty in v0.50+).
	return nil
}

// requireDecs refuses a message whose decimal fields were left out: a
// non-nullable LegacyDec absent from the wire decodes with a nil integer,
// and comparing it panics (gov recovers, but the proposal fails with no
// usable error).
func requireDecs(msg string, fields []namedDec) error {
	for _, f := range fields {
		if f.value.IsNil() {
			return errorsmod.Wrapf(guardiantypes.ErrImmutableField, "%s: %s is required", msg, f.name)
		}
	}
	return nil
}

// namedDec is one decimal field for requireDecs, in a fixed order so the
// reported field is deterministic.
type namedDec struct {
	name  string
	value math.LegacyDec
}
