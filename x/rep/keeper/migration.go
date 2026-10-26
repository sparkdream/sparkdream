package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// Member key migration lifecycle: request / reveal / force open a
// MemberMigration, the new address accepts, the EndBlocker finalizes after
// the path's delay (migration_finalize.go). docs/x-rep-spec.md, "Member key
// migration".

// SetMigrationHooks registers the cross-module hooks (commons, name, season,
// session, federation, service, artifact). Called from app.go after
// depinject.
func (k Keeper) SetMigrationHooks(h types.MemberMigrationHooks) {
	k.late.migrationHooks = h
}

// AddReservedAddress marks an address that can never be a migration target:
// module accounts and module-derived escrows, the x/shield public submitter
// whose private key is public, and so on. Wired from app.go.
func (k Keeper) AddReservedAddress(addr string) {
	if k.late.reservedAddresses == nil {
		k.late.reservedAddresses = map[string]bool{}
	}
	k.late.reservedAddresses[addr] = true
}

// SetVaultChecker late-wires the x/vault "is this a shared vault address"
// check (multisig-accounts plan). Until x/vault exists it stays nil and
// reports false.
func (k Keeper) SetVaultChecker(fn func(ctx context.Context, addr string) bool) {
	k.late.isVault = fn
}

func (k Keeper) migrationHooks() types.MemberMigrationHooks {
	if k.late.migrationHooks == nil {
		return types.MultiMemberMigrationHooks{}
	}
	return k.late.migrationHooks
}

// HasOpenMemberReport reports whether a report against member is PENDING,
// ESCALATED or META_APPEALED. The key path is refused while one is open, so
// a member about to be zeroed cannot start a migration first.
func (k Keeper) HasOpenMemberReport(ctx context.Context, member string) bool {
	report, err := k.MemberReport.Get(ctx, member)
	if err != nil {
		return false
	}
	switch report.Status {
	case types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED,
		types.MemberReportStatus_MEMBER_REPORT_STATUS_META_APPEALED:
		return true
	}
	return false
}

// IsHRCommitteePolicy reports whether addr is the Commons HR (governance)
// committee policy address or the gov authority: the only authorities that
// may force or cancel a council migration.
func (k Keeper) IsHRCommitteePolicy(ctx context.Context, addr string) bool {
	if k.IsGovAuthority(addr) {
		return true
	}
	if k.commonsKeeper == nil {
		return false
	}
	return k.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, addr, "commons", "hr")
}

// checkMigrationSource verifies the member a migration moves away from:
// an existing, non-tombstoned, non-zeroed member.
func (k Keeper) checkMigrationSource(ctx context.Context, from string) (types.Member, error) {
	if k.IsTombstoned(ctx, from) {
		return types.Member{}, types.ErrMemberTombstoned
	}
	m, err := k.Member.Get(ctx, from)
	if err != nil {
		return types.Member{}, types.ErrMemberNotFound
	}
	if m.Status == types.MemberStatus_MEMBER_STATUS_ZEROED {
		return types.Member{}, types.ErrMigrationZeroedMember
	}
	return m, nil
}

// checkTargetFresh enforces the freshness rules of plan §7.1 on the new
// address: no identity of its own anywhere (member record, pending
// invitation, tombstone, pending migration target), and a personal address
// (no module or module-derived account, no council or committee policy, no
// reserved address, no vault). Module-held identity (names, season profile,
// session grants as granter, operator records, federation links) is checked
// by the BeforeMemberMigration hooks the caller runs next.
func (k Keeper) checkTargetFresh(ctx context.Context, from, to string) error {
	if to == from {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address equals the old one")
	}
	if _, err := k.addressCodec.StringToBytes(to); err != nil {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "invalid new address")
	}
	if has, _ := k.Member.Has(ctx, to); has {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address is already a member")
	}
	if invID, err := k.InvitationsByInvitee.Get(ctx, to); err == nil {
		if inv, err := k.Invitation.Get(ctx, invID); err == nil && inv.Status == types.InvitationStatus_INVITATION_STATUS_PENDING {
			return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address has a pending invitation")
		}
	}
	if k.IsTombstoned(ctx, to) {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address is a tombstone")
	}
	if has, _ := k.PendingMigrationByTarget.Has(ctx, to); has {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address is already the target of a pending migration")
	}
	if err := k.IsPersonalAddress(ctx, to); err != nil {
		return errorsmod.Wrapf(types.ErrMigrationTargetNotFresh, "new address %v", err)
	}
	return nil
}

// IsPersonalAddress fails when addr cannot belong to a person: a reserved
// module or system address, a module account, a council or committee policy
// or an x/vault shared account. Membership is a person, so every entry point
// that creates or moves an identity (invitation, acceptance, migration target)
// runs this gate; x/vault runs it on signer sets.
func (k Keeper) IsPersonalAddress(ctx context.Context, addr string) error {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return fmt.Errorf("is not a valid address: %w", err)
	}
	if k.late.reservedAddresses[addr] {
		return fmt.Errorf("is a reserved module or system address")
	}
	if k.authKeeper != nil {
		if acc := k.authKeeper.GetAccount(ctx, addrBytes); acc != nil {
			if _, isModule := acc.(sdk.ModuleAccountI); isModule {
				return fmt.Errorf("is a module account")
			}
		}
	}
	if k.commonsKeeper != nil && k.commonsKeeper.IsGroupPolicyAddress(ctx, addr) {
		return fmt.Errorf("is a group policy")
	}
	if k.late.isVault != nil && k.late.isVault(ctx, addr) {
		return fmt.Errorf("is a vault")
	}
	return nil
}

// checkAliasCaps refuses a path once the member's alias counts hit the
// compiled ceilings. The KEY path keeps AliasRecoveryReserve recent slots for
// recovery and council migrations.
func (k Keeper) checkAliasCaps(ctx context.Context, from string, path types.MigrationPath) error {
	total, recent := k.aliasCounts(ctx, from)
	if total >= types.MaxLifetimeAliases {
		return errorsmod.Wrapf(types.ErrMigrationAliasCap, "%d lifetime aliases", total)
	}
	limit := types.MaxRecentAliases
	if path == types.MigrationPath_MIGRATION_PATH_KEY {
		limit -= types.AliasRecoveryReserve
	}
	if recent >= limit {
		return errorsmod.Wrapf(types.ErrMigrationAliasCap, "%d recent aliases (limit %d for this path)", recent, limit)
	}
	return nil
}

// RequestKeyMigration opens a KEY migration: the member signs the request,
// the new address accepts, finalization follows key_migration_delay later.
func (k Keeper) RequestKeyMigration(ctx context.Context, from, to string) (uint64, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if !params.MigrationEnabled {
		return 0, types.ErrMigrationDisabled
	}
	if !params.KeyPathEnabled {
		return 0, types.ErrKeyPathDisabled
	}
	m, err := k.checkMigrationSource(ctx, from)
	if err != nil {
		return 0, err
	}
	if k.HasOpenMemberReport(ctx, from) {
		return 0, types.ErrMigrationOpenReport
	}
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if m.LastMigratedAt > 0 && now < m.LastMigratedAt+params.MigrationCooldown {
		return 0, errorsmod.Wrapf(types.ErrMigrationCooldown, "until %d", m.LastMigratedAt+params.MigrationCooldown)
	}
	inWindow := 0
	for _, t := range m.KeyMigrationTimes {
		if t > now-params.KeyMigrationWindow {
			inWindow++
		}
	}
	if uint32(inWindow) >= params.MaxKeyMigrationsPerWindow {
		return 0, errorsmod.Wrapf(types.ErrMigrationWindowCap, "%d within key_migration_window", inWindow)
	}
	return k.openMigration(ctx, from, to, types.MigrationPath_MIGRATION_PATH_KEY, "")
}

// ForceMigration opens a COUNCIL migration on behalf of the HR committee or
// governance. An open member report does not block it: a theft victim should
// not wait on a report. The new address must still accept, and the old
// address may object to a jury during the delay.
func (k Keeper) ForceMigration(ctx context.Context, authority, from, to, evidenceURI string) (uint64, error) {
	if !k.IsHRCommitteePolicy(ctx, authority) {
		return 0, errorsmod.Wrap(types.ErrMigrationNotAuthorized, "only the Commons HR committee policy or governance may force a migration")
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if !params.MigrationEnabled {
		return 0, types.ErrMigrationDisabled
	}
	if _, err := k.checkMigrationSource(ctx, from); err != nil {
		return 0, err
	}
	return k.openMigration(ctx, from, to, types.MigrationPath_MIGRATION_PATH_COUNCIL, evidenceURI)
}

// openMigration is the shared open path. It runs the freshness and cap
// checks, applies precedence (RECOVERY > COUNCIL > KEY) against any pending
// migration of the member, asks every module hook for a veto, and writes a
// REQUESTED migration with its accept-window expiry.
func (k Keeper) openMigration(ctx context.Context, from, to string, path types.MigrationPath, evidenceURI string) (uint64, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if _, err := k.checkMigrationSource(ctx, from); err != nil {
		return 0, err
	}
	if err := k.checkTargetFresh(ctx, from, to); err != nil {
		return 0, err
	}
	if err := k.checkAliasCaps(ctx, from, path); err != nil {
		return 0, err
	}

	// Precedence against the member's pending migration, if any.
	if pendingID, err := k.PendingMigrationByMember.Get(ctx, from); err == nil {
		pending, err := k.MemberMigrations.Get(ctx, pendingID)
		if err == nil && types.IsPendingMigrationStatus(pending.Status) {
			if types.MigrationPathRank(pending.Path) >= types.MigrationPathRank(path) {
				return 0, errorsmod.Wrapf(types.ErrMigrationPending, "migration %d (%s) is pending", pending.Id, pending.Path)
			}
			if err := k.cancelMigration(ctx, &pending, types.MigrationStatus_MIGRATION_STATUS_CANCELLED,
				fmt.Sprintf("superseded by a %s migration", path)); err != nil {
				return 0, err
			}
		}
	}

	fromAddr, err := k.addressCodec.StringToBytes(from)
	if err != nil {
		return 0, err
	}
	toAddr, err := k.addressCodec.StringToBytes(to)
	if err != nil {
		return 0, err
	}
	if err := k.migrationHooks().BeforeMemberMigration(ctx, fromAddr, toAddr, path); err != nil {
		return 0, errorsmod.Wrap(types.ErrMigrationVetoed, err.Error())
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	id, err := k.MemberMigrationSeq.Next(ctx)
	if err != nil {
		return 0, err
	}
	id++ // ids start at 1; 0 is "none" in queries and events
	m := types.MemberMigration{
		Id:            id,
		FromAddress:   from,
		ToAddress:     to,
		Path:          path,
		Status:        types.MigrationStatus_MIGRATION_STATUS_REQUESTED,
		RequestedAt:   now,
		EvidenceUri:   evidenceURI,
		ObjectionBond: math.ZeroInt(),
	}
	if err := k.MemberMigrations.Set(ctx, id, m); err != nil {
		return 0, err
	}
	if err := k.PendingMigrationByMember.Set(ctx, from, id); err != nil {
		return 0, err
	}
	if err := k.PendingMigrationByTarget.Set(ctx, to, id); err != nil {
		return 0, err
	}
	if err := k.MigrationAcceptExpiry.Set(ctx, collections.Join(now+params.MigrationAcceptWindow, id)); err != nil {
		return 0, err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migration_requested",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("from", from),
		sdk.NewAttribute("to", to),
		sdk.NewAttribute("path", path.String()),
		sdk.NewAttribute("accept_by", fmt.Sprintf("%d", now+params.MigrationAcceptWindow)),
	))
	return id, nil
}

// AcceptMigration is signed by the new address and completes every path: it
// proves control of the target, sets executes_at and enqueues finalization.
func (k Keeper) AcceptMigration(ctx context.Context, signer string, id uint64) error {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return types.ErrMigrationNotFound
	}
	if m.ToAddress != signer {
		return errorsmod.Wrap(types.ErrMigrationNotAuthorized, "only the new address may accept")
	}
	if m.Status != types.MigrationStatus_MIGRATION_STATUS_REQUESTED {
		return errorsmod.Wrapf(types.ErrMigrationWrongStatus, "status %s", m.Status)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	// Re-verify what may have changed since the request.
	if _, err := k.checkMigrationSource(ctx, m.FromAddress); err != nil {
		return err
	}
	if err := k.checkTargetFreshAtAccept(ctx, m); err != nil {
		return err
	}
	fromAddr, _ := k.addressCodec.StringToBytes(m.FromAddress)
	toAddr, _ := k.addressCodec.StringToBytes(m.ToAddress)
	if err := k.migrationHooks().BeforeMemberMigration(ctx, fromAddr, toAddr, m.Path); err != nil {
		return errorsmod.Wrap(types.ErrMigrationVetoed, err.Error())
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	delay := params.KeyMigrationDelay
	switch m.Path {
	case types.MigrationPath_MIGRATION_PATH_RECOVERY:
		delay = params.RecoveryMigrationDelay
	case types.MigrationPath_MIGRATION_PATH_COUNCIL:
		delay = params.CouncilMigrationDelay
	}
	if err := k.MigrationAcceptExpiry.Remove(ctx, collections.Join(m.RequestedAt+params.MigrationAcceptWindow, id)); err != nil {
		return err
	}
	m.Status = types.MigrationStatus_MIGRATION_STATUS_ACCEPTED
	m.AcceptedAt = now
	m.ExecutesAt = now + delay
	if err := k.MemberMigrations.Set(ctx, id, m); err != nil {
		return err
	}
	if err := k.MigrationQueue.Set(ctx, collections.Join(m.ExecutesAt, id)); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migration_accepted",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("from", m.FromAddress),
		sdk.NewAttribute("to", m.ToAddress),
		sdk.NewAttribute("path", m.Path.String()),
		sdk.NewAttribute("executes_at", fmt.Sprintf("%d", m.ExecutesAt)),
	))
	return nil
}

// checkTargetFreshAtAccept re-runs the freshness check for an existing
// migration, ignoring the migration's own pending-target entry.
func (k Keeper) checkTargetFreshAtAccept(ctx context.Context, m types.MemberMigration) error {
	if err := k.PendingMigrationByTarget.Remove(ctx, m.ToAddress); err != nil {
		return err
	}
	freshErr := k.checkTargetFresh(ctx, m.FromAddress, m.ToAddress)
	if err := k.PendingMigrationByTarget.Set(ctx, m.ToAddress, m.Id); err != nil {
		return err
	}
	return freshErr
}

// CancelMigration cancels a pending migration. Who may cancel: the old or new
// address for KEY migrations; the HR committee policy for KEY and COUNCIL
// migrations; governance for any path (the only authority over a RECOVERY
// migration, whose commitment predates any compromise).
func (k Keeper) CancelMigration(ctx context.Context, signer string, id uint64) error {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return types.ErrMigrationNotFound
	}
	if !types.IsPendingMigrationStatus(m.Status) {
		return errorsmod.Wrapf(types.ErrMigrationWrongStatus, "status %s", m.Status)
	}
	allowed := k.IsGovAuthority(signer)
	switch m.Path {
	case types.MigrationPath_MIGRATION_PATH_KEY:
		allowed = allowed || signer == m.FromAddress || signer == m.ToAddress || k.IsHRCommitteePolicy(ctx, signer)
	case types.MigrationPath_MIGRATION_PATH_COUNCIL:
		allowed = allowed || k.IsHRCommitteePolicy(ctx, signer)
	}
	if !allowed {
		return errorsmod.Wrapf(types.ErrMigrationNotAuthorized, "%s may not cancel a %s migration", signer, m.Path)
	}
	return k.cancelMigration(ctx, &m, types.MigrationStatus_MIGRATION_STATUS_CANCELLED, "cancelled by "+signer)
}

// cancelMigration moves a pending migration to a terminal status, clears its
// queue and index entries and refunds an objection bond to the objector.
func (k Keeper) cancelMigration(ctx context.Context, m *types.MemberMigration, status types.MigrationStatus, reason string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if err := k.clearPendingIndexes(ctx, *m, params); err != nil {
		return err
	}
	if m.Status == types.MigrationStatus_MIGRATION_STATUS_OBJECTED && m.ObjectionBond.IsPositive() {
		if err := k.payObjectionBond(ctx, m, m.Objector); err != nil {
			return err
		}
	}
	m.Status = status
	m.FailureReason = reason
	if err := k.MemberMigrations.Set(ctx, m.Id, *m); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("member_migration_cancelled",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", m.Id)),
		sdk.NewAttribute("from", m.FromAddress),
		sdk.NewAttribute("to", m.ToAddress),
		sdk.NewAttribute("status", status.String()),
		sdk.NewAttribute("reason", reason),
	))
	return nil
}

// clearPendingIndexes removes a migration's pending pointers, accept-expiry
// and queue entries (whichever exist for its status).
func (k Keeper) clearPendingIndexes(ctx context.Context, m types.MemberMigration, params types.Params) error {
	if err := k.PendingMigrationByMember.Remove(ctx, m.FromAddress); err != nil {
		return err
	}
	if err := k.PendingMigrationByTarget.Remove(ctx, m.ToAddress); err != nil {
		return err
	}
	if err := k.MigrationAcceptExpiry.Remove(ctx, collections.Join(m.RequestedAt+params.MigrationAcceptWindow, m.Id)); err != nil {
		return err
	}
	if m.ExecutesAt > 0 {
		if err := k.MigrationQueue.Remove(ctx, collections.Join(m.ExecutesAt, m.Id)); err != nil {
			return err
		}
	}
	return nil
}

// payObjectionBond releases the escrowed objection bond to recipient.
func (k Keeper) payObjectionBond(ctx context.Context, m *types.MemberMigration, recipient string) error {
	to, err := k.addressCodec.StringToBytes(recipient)
	if err != nil {
		return err
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), m.ObjectionBond))
	if err := k.bankKeeper.SendCoins(ctx, AppealBondEscrowAddress(), to, coins); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("migration_objection_bond_paid",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", m.Id)),
		sdk.NewAttribute("recipient", recipient),
		sdk.NewAttribute("amount", m.ObjectionBond.String()),
	))
	m.ObjectionBond = math.ZeroInt()
	return nil
}

// ObjectMigration lets the old address contest a COUNCIL migration during
// its delay. It does not cancel the migration: in the stolen-key case the
// thief holds the key, so a veto would let the thief block recovery. The
// objector posts migration_objection_bond and an x/rep jury decides. The
// bond is paid to the new address if the migration is upheld and refunded
// otherwise, never burned (on the stolen-key path it is the victim's SPARK).
func (k Keeper) ObjectMigration(ctx context.Context, member string, id uint64, reason string) error {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return types.ErrMigrationNotFound
	}
	if m.FromAddress != member {
		return errorsmod.Wrap(types.ErrMigrationNotAuthorized, "only the migrating address may object")
	}
	if m.Path != types.MigrationPath_MIGRATION_PATH_COUNCIL {
		return errorsmod.Wrapf(types.ErrMigrationWrongStatus, "only council migrations can be objected to (path %s)", m.Path)
	}
	if m.Status != types.MigrationStatus_MIGRATION_STATUS_ACCEPTED {
		return errorsmod.Wrapf(types.ErrMigrationWrongStatus, "status %s", m.Status)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	memberAddr, err := k.addressCodec.StringToBytes(member)
	if err != nil {
		return err
	}
	bond := params.MigrationObjectionBond
	if bond.IsPositive() {
		coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), bond))
		if err := k.bankKeeper.SendCoins(ctx, memberAddr, AppealBondEscrowAddress(), coins); err != nil {
			return errorsmod.Wrap(types.ErrMigrationObjectionBond, err.Error())
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"type":         types.MigrationObjectionInitiativeType,
		"migration_id": id,
		"from":         m.FromAddress,
		"to":           m.ToAddress,
		"reason":       reason,
	})
	deadline := now + types.DefaultAppealDeadline
	reviewID, err := k.CreateAppealInitiative(ctx, types.MigrationObjectionInitiativeType, payload, deadline)
	if err != nil {
		return errorsmod.Wrap(err, "failed to open objection jury review")
	}
	// Seat a jury that excludes both parties. Best effort, like appeals: a
	// review nobody can be seated on resolves INCONCLUSIVE at its deadline.
	if jurors, jErr := k.selectModerationAppealJury(ctx, reviewID, []string{m.FromAddress, m.ToAddress}); jErr != nil {
		sdkCtx.Logger().Warn("failed to seat migration objection jury", "review_id", reviewID, "error", jErr)
	} else if len(jurors) > 0 {
		if review, rErr := k.GetJuryReview(ctx, reviewID); rErr == nil {
			review.Jurors = jurors
			review.RequiredVotes = uint32(k.appealRequiredVotes(ctx, len(jurors)))
			if err := k.JuryReview.Set(ctx, reviewID, review); err == nil {
				_ = k.AddJuryReviewToJurorIndex(ctx, review)
				_ = k.RecordJurySeating(ctx, jurors)
			}
		}
	}

	// Leave the queue while the jury sits; the verdict re-enqueues or
	// cancels.
	if err := k.MigrationQueue.Remove(ctx, collections.Join(m.ExecutesAt, id)); err != nil {
		return err
	}
	m.Status = types.MigrationStatus_MIGRATION_STATUS_OBJECTED
	m.JuryReviewId = reviewID
	m.Objector = member
	m.ObjectionBond = bond
	if err := k.MemberMigrations.Set(ctx, id, m); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migration_objected",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("from", m.FromAddress),
		sdk.NewAttribute("to", m.ToAddress),
		sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", reviewID)),
		sdk.NewAttribute("bond", bond.String()),
	))
	return nil
}

// resolveMigrationObjection applies a jury verdict to an OBJECTED migration.
// The objector is the "challenger": UPHOLD_CHALLENGE sustains the objection
// (migration cancelled, bond refunded); REJECT_CHALLENGE upholds the
// migration (finalizes at the next EndBlock, bond paid to the new address);
// INCONCLUSIVE cancels and refunds, so no identity ever moves without a
// positive jury decision. The HR committee may file again.
func (k Keeper) resolveMigrationObjection(ctx context.Context, review types.JuryReview, verdict types.Verdict) error {
	var payload struct {
		MigrationID uint64 `json:"migration_id"`
	}
	if err := json.Unmarshal([]byte(review.ReviewDeliverable), &payload); err != nil {
		return fmt.Errorf("decode migration objection payload: %w", err)
	}
	m, err := k.MemberMigrations.Get(ctx, payload.MigrationID)
	if err != nil {
		return types.ErrMigrationNotFound
	}
	if m.Status != types.MigrationStatus_MIGRATION_STATUS_OBJECTED || m.JuryReviewId != review.Id {
		return nil // already resolved or superseded; nothing to apply
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	switch verdict {
	case types.Verdict_VERDICT_REJECT_CHALLENGE:
		if m.ObjectionBond.IsPositive() {
			if err := k.payObjectionBond(ctx, &m, m.ToAddress); err != nil {
				return err
			}
		}
		m.Status = types.MigrationStatus_MIGRATION_STATUS_ACCEPTED
		m.ExecutesAt = sdkCtx.BlockTime().Unix()
		if err := k.MemberMigrations.Set(ctx, m.Id, m); err != nil {
			return err
		}
		if err := k.MigrationQueue.Set(ctx, collections.Join(m.ExecutesAt, m.Id)); err != nil {
			return err
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migration_objection_rejected",
			sdk.NewAttribute("migration_id", fmt.Sprintf("%d", m.Id)),
			sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", review.Id)),
		))
		return nil
	case types.Verdict_VERDICT_UPHOLD_CHALLENGE:
		return k.cancelMigration(ctx, &m, types.MigrationStatus_MIGRATION_STATUS_CANCELLED,
			fmt.Sprintf("objection upheld by jury review %d", review.Id))
	default:
		return k.cancelMigration(ctx, &m, types.MigrationStatus_MIGRATION_STATUS_CANCELLED,
			fmt.Sprintf("objection jury review %d inconclusive", review.Id))
	}
}

// SweepObjectedMigrations tallies objection reviews whose deadline passed
// without a verdict, so a silent jury cannot park a migration forever.
func (k Keeper) SweepObjectedMigrations(ctx context.Context) error {
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	var due []uint64
	err := k.PendingMigrationByMember.Walk(ctx, nil, func(_ string, id uint64) (bool, error) {
		m, err := k.MemberMigrations.Get(ctx, id)
		if err != nil || m.Status != types.MigrationStatus_MIGRATION_STATUS_OBJECTED {
			return false, nil
		}
		review, err := k.GetJuryReview(ctx, m.JuryReviewId)
		if err != nil || review.Verdict != types.Verdict_VERDICT_PENDING || review.Deadline > now {
			return false, nil
		}
		due = append(due, m.JuryReviewId)
		return false, nil
	})
	if err != nil {
		return err
	}
	for _, reviewID := range due {
		if err := k.TallyJuryVotes(ctx, reviewID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("failed to tally expired migration objection", "review_id", reviewID, "error", err)
		}
	}
	return nil
}

// SweepTombstone moves a tombstone's spendable SPARK to its canonical
// address. Unbonding completes to the tombstone through UndelegateCoins,
// which bypasses the send restriction, so SPARK keeps landing there after
// finalization; on the RECOVERY and COUNCIL paths the tombstone cannot
// MsgSend it, so the canonical address collects it here.
func (k Keeper) SweepTombstone(ctx context.Context, signer, tombstone string) (sdk.Coins, error) {
	t, found := k.GetTombstone(ctx, tombstone)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrNotTombstoneOwner, "%s is not a tombstone", tombstone)
	}
	if t.Canonical != signer {
		return nil, errorsmod.Wrapf(types.ErrNotTombstoneOwner, "canonical address is %s", t.Canonical)
	}
	from, err := k.addressCodec.StringToBytes(tombstone)
	if err != nil {
		return nil, err
	}
	to, err := k.addressCodec.StringToBytes(signer)
	if err != nil {
		return nil, err
	}
	spendable := k.bankKeeper.SpendableCoins(ctx, from)
	if spendable.IsZero() {
		return sdk.NewCoins(), nil
	}
	if err := k.bankKeeper.SendCoins(ctx, from, to, spendable); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("tombstone_swept",
		sdk.NewAttribute("tombstone", tombstone),
		sdk.NewAttribute("canonical", signer),
		sdk.NewAttribute("amount", spendable.String()),
	))
	return spendable, nil
}

// GetMemberMigration returns a migration by id.
func (k Keeper) GetMemberMigration(ctx context.Context, id uint64) (types.MemberMigration, bool) {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return types.MemberMigration{}, false
	}
	return m, true
}

// GetPendingMigration returns the member's pending migration, if any.
func (k Keeper) GetPendingMigration(ctx context.Context, member string) (types.MemberMigration, bool) {
	id, err := k.PendingMigrationByMember.Get(ctx, member)
	if err != nil {
		return types.MemberMigration{}, false
	}
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil || !types.IsPendingMigrationStatus(m.Status) {
		return types.MemberMigration{}, false
	}
	return m, true
}

// GetPendingMigrationByTarget returns the pending migration whose new address
// is to.
func (k Keeper) GetPendingMigrationByTarget(ctx context.Context, to string) (types.MemberMigration, bool) {
	id, err := k.PendingMigrationByTarget.Get(ctx, to)
	if err != nil {
		return types.MemberMigration{}, false
	}
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil || !types.IsPendingMigrationStatus(m.Status) {
		return types.MemberMigration{}, false
	}
	return m, true
}

// ZkKeyActivationDelaySeconds is the ZK key activation delay converted to
// seconds, the ceiling x/commons applies to council voting periods.
func (k Keeper) ZkKeyActivationDelaySeconds(ctx context.Context) int64 {
	if !types.VotingPeriodCapEnabled {
		return 0 // no cap in the test build
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0
	}
	return params.ZkKeyActivationDelaySeconds()
}

// checkZkDelayAgainstVotingPeriods refuses params that would lower the ZK
// key activation delay below a council voting period already in use
// (member-key-migration plan, decision 17).
func (k Keeper) checkZkDelayAgainstVotingPeriods(ctx context.Context, params types.Params) error {
	if k.commonsKeeper == nil || !types.VotingPeriodCapEnabled {
		return nil
	}
	if live := k.commonsKeeper.MaxLiveVotingPeriodSeconds(ctx); live > params.ZkKeyActivationDelaySeconds() {
		return errorsmod.Wrapf(types.ErrVotingPeriodExceedsZkDelay,
			"delay %ds, longest live voting period %ds", params.ZkKeyActivationDelaySeconds(), live)
	}
	return nil
}

// CheckMigrationTarget runs the full target-freshness check for a prospective
// migration, including every module's BeforeMemberMigration veto. Used by the
// simulation to avoid delivering a request the handler would refuse.
func (k Keeper) CheckMigrationTarget(ctx context.Context, from, to string, path types.MigrationPath) error {
	if err := k.checkTargetFresh(ctx, from, to); err != nil {
		return err
	}
	fromAddr, err := k.addressCodec.StringToBytes(from)
	if err != nil {
		return err
	}
	toAddr, err := k.addressCodec.StringToBytes(to)
	if err != nil {
		return err
	}
	return k.migrationHooks().BeforeMemberMigration(ctx, fromAddr, toAddr, path)
}
