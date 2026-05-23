package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// Small wrappers around collections.Join* / NewPrefixed* used by this
// file so call sites read cleanly. No semantic difference from the
// underlying collections API.
func newPrefixedRangeOnAddr(addr []byte) collections.Ranger[collections.Triple[[]byte, string, int64]] {
	return collections.NewPrefixedTripleRange[[]byte, string, int64](addr)
}

func newPrefixedRangeOnOpBytes(addr []byte) collections.Ranger[collections.Triple[[]byte, string, uint64]] {
	return collections.NewPrefixedTripleRange[[]byte, string, uint64](addr)
}

func newTriple(a []byte, b string, c uint64) collections.Triple[[]byte, string, uint64] {
	return collections.Join3(a, b, c)
}

func newPair(a int64, b uint64) collections.Pair[int64, uint64] {
	return collections.Join(a, b)
}

// public_api.go exposes the §6.7 Keeper API methods that other modules
// (notably x/federation) consume. These are pure keeper methods — no
// msg-server wrappers — so callers don't have to construct + dispatch
// MsgRegisterOperator etc. when integrating natively.
//
// All mutating methods respect the same state-machine and bond-pool
// invariants as the msg-server paths and emit the same events.

// SlashSource is consumed by SlashOperator (§6.7) to apply the right
// invariants per slash origin.
type SlashSource int

const (
	SlashSourceTier1     SlashSource = 1 // controller tier-1; honors cooldown + aggregate cap
	SlashSourceTier2Jury SlashSource = 2 // jury verdict; no cooldown/cap, deposit refund logic on caller
	SlashSourceMigration SlashSource = 3 // chain-upgrade reconciliation; bypasses all gates
)

// IsActiveOperator reports whether (address, service_type) is in the
// live store with status ACTIVE. Used by off-chain consumers and
// inter-module callers (§6.7).
func (k Keeper) IsActiveOperator(ctx context.Context, addr sdk.AccAddress, serviceType string) bool {
	op, exists := k.GetOperator(ctx, addr.Bytes(), serviceType)
	if !exists {
		return false
	}
	return op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE
}

// GetAvailableBond returns the operator's current bond (excludes any
// SPARK sitting in Tier1Escrow — that amount has already been moved
// out of the bond pool). Returns zero coin if the operator doesn't
// exist or is in a terminal state.
func (k Keeper) GetAvailableBond(ctx context.Context, addr sdk.AccAddress, serviceType string) sdk.Coin {
	bondDenom := k.BondDenom(ctx)
	op, exists := k.GetOperator(ctx, addr.Bytes(), serviceType)
	if !exists {
		return sdk.NewCoin(bondDenom, sdkmath.ZeroInt())
	}
	return sdk.NewCoin(bondDenom, op.BondAmount)
}

// GetServiceTypeConfig fetches the registry entry for the given
// service_type or returns false if missing.
func (k Keeper) GetServiceTypeConfig(ctx context.Context, serviceType string) (types.ServiceTypeConfig, bool) {
	cfg, err := k.ServiceTypes.Get(ctx, serviceType)
	if err != nil {
		return types.ServiceTypeConfig{}, false
	}
	return cfg, true
}

// GetArchivedOperators returns all archived (SLASHED/RETIRED) records
// for (address, service_type), ordered by retired_at ascending.
func (k Keeper) GetArchivedOperators(ctx context.Context, addr sdk.AccAddress, serviceType string) ([]types.Operator, error) {
	addrBytes := addr.Bytes()
	rng := newPrefixedRangeOnAddr(addrBytes)
	iter, err := k.ArchivedOperators.Iterate(ctx, rng)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var out []types.Operator
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return nil, err
		}
		if key.K2() != serviceType {
			continue
		}
		op, err := k.ArchivedOperators.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, nil
}

// ListPendingReportsAgainst returns all PENDING and ESCALATED reports
// for (address, service_type).
func (k Keeper) ListPendingReportsAgainst(ctx context.Context, addr sdk.AccAddress, serviceType string) ([]types.Report, error) {
	opBytes := addr.Bytes()
	rng := newPrefixedRangeOnOpBytes(opBytes)
	iter, err := k.ReportsByOperator.Iterate(ctx, rng)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var out []types.Report
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return nil, err
		}
		if key.K2() != serviceType {
			continue
		}
		r, err := k.Reports.Get(ctx, key.K3())
		if err != nil {
			continue
		}
		if r.Status == types.ReportStatus_REPORT_STATUS_PENDING ||
			r.Status == types.ReportStatus_REPORT_STATUS_ESCALATED {
			out = append(out, r)
		}
	}
	return out, nil
}

// RegisterOperator is the public-keeper-API equivalent of the
// MsgRegisterOperator handler. Used by x/federation's bridge-operator
// flow to register an operator without going through the msg-server.
//
// The `source` parameter controls registration-gate enforcement:
//   - normal (default): all gates from §5.1 enforced;
//   - SlashSourceMigration: bypasses IsGroupPolicyAddress / min_bond /
//     max_active_operators_per_address checks for chain-upgrade state
//     migration (§15).
func (k Keeper) RegisterOperator(
	ctx context.Context,
	creator string,
	serviceType string,
	controller string,
	bondAmount sdkmath.Int,
	metadata []byte,
	source SlashSource,
) (types.Operator, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	creatorBytes, err := k.addrBytes(creator)
	if err != nil {
		return types.Operator{}, types.ErrInvalidSigner.Wrap("invalid creator address")
	}
	if _, err := k.addrBytes(controller); err != nil {
		return types.Operator{}, types.ErrControllerNotGroup.Wrap("invalid controller address")
	}

	bypassChecks := source == SlashSourceMigration

	if !bypassChecks && creator == controller {
		return types.Operator{}, types.ErrSelfController
	}

	if k.commonsKeeper() != nil && !bypassChecks {
		if !k.commonsKeeper().IsGroupPolicyAddress(ctx, controller) {
			return types.Operator{}, types.ErrControllerNotGroup
		}
	}

	cfg, err := k.resolveServiceTypeConfig(ctx, serviceType)
	if err != nil {
		return types.Operator{}, err
	}
	if !bypassChecks && !cfg.Enabled {
		return types.Operator{}, types.ErrServiceTypeDisabled.Wrap(serviceType)
	}

	bondDenom := k.BondDenom(ctx)
	if bondAmount.IsNil() || bondAmount.IsNegative() {
		return types.Operator{}, types.ErrInsufficientBond.Wrap("bond_amount must be a positive Int")
	}
	if !bypassChecks && bondAmount.LT(cfg.MinBondAmount) {
		return types.Operator{}, types.ErrInsufficientBond.Wrapf("bond %s%s < min %s%s", bondAmount, bondDenom, cfg.MinBondAmount, bondDenom)
	}
	bond := sdk.NewCoin(bondDenom, bondAmount)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.Operator{}, err
	}
	if uint32(len(metadata)) > params.MaxMetadataBytes {
		return types.Operator{}, types.ErrInvalidMetadataSize.Wrapf("%d > %d", len(metadata), params.MaxMetadataBytes)
	}

	if _, exists := k.GetOperator(ctx, creatorBytes, serviceType); exists {
		return types.Operator{}, types.ErrOperatorAlreadyExists.Wrapf("%s / %s", creator, serviceType)
	}
	hasSlashed, err := k.HasSlashedRecord(ctx, creatorBytes, serviceType)
	if err != nil {
		return types.Operator{}, err
	}
	if hasSlashed {
		return types.Operator{}, types.ErrOperatorPreviouslySlashed.Wrapf("%s / %s", creator, serviceType)
	}

	if !bypassChecks {
		liveCount, err := k.CountLiveOperatorsForAddress(ctx, creatorBytes)
		if err != nil {
			return types.Operator{}, err
		}
		if liveCount >= params.MaxActiveOperatorsPerAddress {
			return types.Operator{}, types.ErrTooManyOperatorsForAddress
		}
	}

	// Escrow bond to module account (skip for migration — caller has
	// already moved funds in via SendCoinsFromModuleToModule).
	if !bypassChecks {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sdk.AccAddress(creatorBytes), types.ModuleName, sdk.NewCoins(bond)); err != nil {
			return types.Operator{}, err
		}
	}

	currentHeight := sdkCtx.BlockHeight()
	op := types.Operator{
		Address:                 creator,
		ServiceType:             serviceType,
		Controller:              controller,
		BondAmount:              bondAmount,
		Metadata:                metadata,
		Status:                  types.OperatorStatus_OPERATOR_STATUS_ACTIVE,
		Tier1SlashedInWindow:    sdkmath.ZeroInt(),
		Tier1WindowStart:        currentHeight,
		Tier1WindowStartBond:    bondAmount,
		RegisteredAt:            currentHeight,
		TotalLifetimeBondBlocks: sdkmath.ZeroInt(),
		LastBondBlockUpdateAt:   currentHeight,
	}
	if err := k.PutOperator(ctx, op); err != nil {
		return types.Operator{}, err
	}

	sdkCtx.EventManager().EmitEvent(types.NewOperatorRegisteredEvent(creator, serviceType, controller, bond))

	return op, nil
}

// SlashOperator is the public-keeper-API slash entry-point (§6.7).
// Used by x/federation to slash a bridge operator for proven
// misbehavior outside of the standard report-then-resolve flow.
//
//   - SlashSourceTier1: applies cooldown + aggregate-cap; routes through
//     Tier1Escrow with the report_contest_window_blocks release delay.
//     `reportID` MUST be a valid report ID owned by this operator
//     (Tier1Escrow.ReportId field).
//   - SlashSourceTier2Jury: bypasses cooldown/cap; direct transfer to
//     community pool. `reportID` may be zero.
//   - SlashSourceMigration: bypasses all gates; direct debit + transfer.
//     `reportID` may be zero.
//
// Returns the actual slash amount (post-clamping). Does NOT dissolve on
// bond → 0 — caller must invoke TerminateOperator if dissolution is
// desired.
func (k Keeper) SlashOperator(
	ctx context.Context,
	addr sdk.AccAddress,
	serviceType string,
	slashBps uint32,
	reportID uint64,
	source SlashSource,
) (sdk.Coin, error) {
	if slashBps == 0 || slashBps > 10000 {
		return sdk.Coin{}, types.ErrSlashCapExceeded.Wrapf("slash_bps must be in (0, 10000], got %d", slashBps)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()
	addrBytes := addr.Bytes()

	op, exists := k.GetOperator(ctx, addrBytes, serviceType)
	if !exists {
		return sdk.Coin{}, types.ErrOperatorNotFound
	}
	cfg, err := k.resolveServiceTypeConfig(ctx, serviceType)
	if err != nil {
		return sdk.Coin{}, err
	}

	bondDenom := k.BondDenom(ctx)
	slashAmount := computeSlashAmount(op.BondAmount, slashBps)
	if slashAmount.IsZero() {
		return sdk.NewCoin(bondDenom, sdkmath.ZeroInt()), nil
	}

	// Tier-1 only: cap + cooldown.
	if source == SlashSourceTier1 {
		controllerBytes, err := k.addrBytes(op.Controller)
		if err != nil {
			return sdk.Coin{}, err
		}
		if err := k.checkTier1Cooldown(ctx, cfg, controllerBytes, addrBytes, serviceType, currentHeight); err != nil {
			return sdk.Coin{}, err
		}
		if err := k.checkAndAdvanceTier1Window(&op, cfg, slashAmount, currentHeight); err != nil {
			return sdk.Coin{}, err
		}
	}

	preStatus := op.Status
	k.settleBondBlocks(&op, currentHeight)
	k.applySlashToBond(&op, cfg, slashAmount, currentHeight)
	if source == SlashSourceTier1 {
		op.Tier1SlashedInWindow = op.Tier1SlashedInWindow.Add(slashAmount)
	}
	underfundedTransition := preStatus == types.OperatorStatus_OPERATOR_STATUS_ACTIVE &&
		op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED

	slashCoin := sdk.NewCoin(bondDenom, slashAmount)

	switch source {
	case SlashSourceTier1:
		// Route to Tier1Escrow.
		escrowID, err := k.NextEscrowID.Next(ctx)
		if err != nil {
			return sdk.Coin{}, err
		}
		if escrowID == 0 {
			escrowID, err = k.NextEscrowID.Next(ctx)
			if err != nil {
				return sdk.Coin{}, err
			}
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return sdk.Coin{}, err
		}
		releaseAt := currentHeight + params.ReportContestWindowBlocks
		escrow := types.Tier1EscrowEntry{
			EscrowId: escrowID, ReportId: reportID,
			OperatorAddress: op.Address, ServiceType: op.ServiceType,
			Amount: slashAmount, ReleaseAt: releaseAt,
		}
		if err := k.Tier1Escrow.Set(ctx, escrowID, escrow); err != nil {
			return sdk.Coin{}, err
		}
		if err := k.Tier1EscrowByOperator.Set(ctx, newTriple(addrBytes, op.ServiceType, escrowID)); err != nil {
			return sdk.Coin{}, err
		}
		if err := k.Tier1EscrowReleaseQueue.Set(ctx, newPair(releaseAt, escrowID)); err != nil {
			return sdk.Coin{}, err
		}
		if err := k.recordTier1Slash(ctx, op.Controller, op.Address, op.ServiceType, currentHeight); err != nil {
			return sdk.Coin{}, err
		}
		sdkCtx.EventManager().EmitEvents(sdk.Events{
			types.NewOperatorSlashedEvent(op.Address, op.ServiceType, slashCoin, sdk.NewCoin(bondDenom, op.BondAmount), types.TierTier1, reportID),
			types.NewTier1EscrowLockedEvent(escrowID, reportID, op.Address, op.ServiceType, slashCoin, releaseAt),
		})

	case SlashSourceTier2Jury, SlashSourceMigration:
		// Direct transfer to community pool.
		if k.distributionKeeper() != nil {
			if err := k.distributionKeeper().FundCommunityPool(ctx, sdk.NewCoins(slashCoin), k.bankModuleAddress()); err != nil {
				return sdk.Coin{}, err
			}
		}
		tier := types.TierTier2
		if source == SlashSourceMigration {
			tier = "MIGRATION"
		}
		sdkCtx.EventManager().EmitEvent(types.NewOperatorSlashedEvent(
			op.Address, op.ServiceType, slashCoin, sdk.NewCoin(bondDenom, op.BondAmount), tier, reportID,
		))

	default:
		return sdk.Coin{}, fmt.Errorf("unknown slash source %d", source)
	}

	if err := k.PutOperator(ctx, op); err != nil {
		return sdk.Coin{}, err
	}

	// Fire underfunded hook after state writes. Phase 0 federation→
	// service migration.
	if underfundedTransition && k.hooks() != nil {
		k.hooks().AfterOperatorUnderfunded(ctx, addr, op.ServiceType)
	}

	return slashCoin, nil
}

// TerminateOperator is the public-keeper-API dissolution entry-point
// (§6.7). Used to transition an operator to SLASHED + archive without
// going through the jury path — e.g. from a SlashSourceMigration code
// path that wants to force-dissolve as part of a chain upgrade.
//
// Caller is responsible for any associated event-causing reportID.
func (k Keeper) TerminateOperator(
	ctx context.Context,
	addr sdk.AccAddress,
	serviceType string,
	causingReportID uint64,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	op, exists := k.GetOperator(ctx, addr.Bytes(), serviceType)
	if !exists {
		return types.ErrOperatorNotFound
	}

	return k.dissolveOperator(ctx, &op, currentHeight, causingReportID)
}

// SetServiceTypeConfig is a convenience for genesis bootstrapping /
// upgrade migration — write a registry entry without going through
// MsgUpdateServiceTypeConfig (which requires gov authority). Caller
// is responsible for validation.
func (k Keeper) SetServiceTypeConfig(ctx context.Context, cfg types.ServiceTypeConfig) error {
	return k.ServiceTypes.Set(ctx, cfg.ServiceType, cfg)
}
