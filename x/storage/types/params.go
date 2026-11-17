package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// maxWindowBlocks keeps every window far from overflowing a block height.
const maxWindowBlocks int64 = 1 << 31

// MaxTrustLevel is the highest x/rep trust level (CORE).
const MaxTrustLevel uint32 = 4

// DefaultRequestFee is 1 SPARK in micro-units.
var DefaultRequestFee = math.NewInt(1_000_000)

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return Params{
		Enabled:                                    true,
		MinRequesterTrustLevel:                     1, // PROVISIONAL
		RequestFee:                                 DefaultRequestFee,
		RequestTtlBlocks:                           defaultRequestTTLBlocks(),
		ClaimWindowBlocks:                          defaultClaimWindowBlocks(),
		SettleWindowBlocks:                         defaultSettleWindowBlocks(),
		DisputeWindowBlocks:                        defaultDisputeWindowBlocks(),
		FlagHoldBlocks:                             defaultFlagHoldBlocks(),
		MinUnreachableBlocks:                       defaultMinUnreachableBlocks(),
		TerminalRetentionBlocks:                    defaultTerminalRetentionBlocks(),
		CounterWindowBlocks:                        defaultCounterWindowBlocks(),
		DeadlineRetryBlocks:                        defaultDeadlineRetryBlocks(),
		MaxSourcesPerRequest:                       4,
		MaxAttestationsPerCompletion:               8,
		MaxOperatorsPerBackend:                     16,
		MaxApprovedScanners:                        16,
		MaxOpenClaimsPerOperator:                   50,
		MaxLapsesPerWindow:                         10,
		MaxMismatchDeclinesPerOperatorPerWindow:    20,
		MaxUnreachableDeclinesPerOperatorPerWindow: 20,
		MaxCompletionsPerOperatorPerWindow:         100,
		MaxFlagsPerScannerPerWindow:                50,
		MaxFlagsPerHash:                            8,
		MaxHashesPerUnflag:                         50,
		MaxUrlBytes:                                512,
		MaxReasonBytes:                             256,
		MaxBarredPerRequest:                        32,
		MaxOpenRequestsPerRequester:                20,
		MaxDeadlinesPerBlock:                       200,
	}
}

// HoldSpan is what an operator's unbonding period must be longer than, so
// that the bond is still held while a completion can be voided. An operator
// that starts to unbond can no longer claim, so what it needs is the time to
// finish a claim it holds (the claim and its one extension) and then settle
// and dispute. The flag hold and a second settle and dispute are margin: a
// lift restores a completion only while its operator is not unbonding (see
// reopen), so no number of flags can carry a completion past the bond.
func (p Params) HoldSpan() int64 {
	return 2*p.ClaimWindowBlocks + 2*(p.SettleWindowBlocks+p.DisputeWindowBlocks) + p.FlagHoldBlocks
}

// Validate validates the set of params. Checks that read other state (the
// unbonding period of a backend's service type, approvals already given) are
// made by the handlers and InitGenesis.
func (p Params) Validate() error {
	if p.MinRequesterTrustLevel > MaxTrustLevel {
		return fmt.Errorf("min_requester_trust_level %d is above %d", p.MinRequesterTrustLevel, MaxTrustLevel)
	}
	if p.RequestFee.IsNil() || p.RequestFee.IsNegative() {
		return fmt.Errorf("request_fee must not be negative")
	}
	for _, w := range []struct {
		name string
		v    int64
	}{
		{"request_ttl_blocks", p.RequestTtlBlocks},
		{"claim_window_blocks", p.ClaimWindowBlocks},
		{"settle_window_blocks", p.SettleWindowBlocks},
		{"dispute_window_blocks", p.DisputeWindowBlocks},
		{"flag_hold_blocks", p.FlagHoldBlocks},
		{"min_unreachable_blocks", p.MinUnreachableBlocks},
		{"terminal_retention_blocks", p.TerminalRetentionBlocks},
		{"counter_window_blocks", p.CounterWindowBlocks},
		{"deadline_retry_blocks", p.DeadlineRetryBlocks},
	} {
		if w.v <= 0 {
			return fmt.Errorf("%s must be positive", w.name)
		}
		// Windows are added to block heights; keep them far from overflow.
		if w.v > maxWindowBlocks {
			return fmt.Errorf("%s is too large", w.name)
		}
	}
	for _, c := range []struct {
		name string
		v    uint32
	}{
		{"max_sources_per_request", p.MaxSourcesPerRequest},
		{"max_attestations_per_completion", p.MaxAttestationsPerCompletion},
		{"max_operators_per_backend", p.MaxOperatorsPerBackend},
		{"max_approved_scanners", p.MaxApprovedScanners},
		{"max_open_claims_per_operator", p.MaxOpenClaimsPerOperator},
		{"max_lapses_per_window", p.MaxLapsesPerWindow},
		{"max_mismatch_declines_per_operator_per_window", p.MaxMismatchDeclinesPerOperatorPerWindow},
		{"max_unreachable_declines_per_operator_per_window", p.MaxUnreachableDeclinesPerOperatorPerWindow},
		{"max_completions_per_operator_per_window", p.MaxCompletionsPerOperatorPerWindow},
		{"max_flags_per_scanner_per_window", p.MaxFlagsPerScannerPerWindow},
		{"max_flags_per_hash", p.MaxFlagsPerHash},
		{"max_hashes_per_unflag", p.MaxHashesPerUnflag},
		{"max_url_bytes", p.MaxUrlBytes},
		{"max_reason_bytes", p.MaxReasonBytes},
		{"max_barred_per_request", p.MaxBarredPerRequest},
		{"max_open_requests_per_requester", p.MaxOpenRequestsPerRequester},
		{"max_deadlines_per_block", p.MaxDeadlinesPerBlock},
	} {
		if c.v == 0 {
			return fmt.Errorf("%s must be positive", c.name)
		}
	}
	// Bounded lists live on the request record; keep them small.
	if p.MaxSourcesPerRequest > 16 || p.MaxAttestationsPerCompletion > 64 ||
		p.MaxBarredPerRequest > 128 || p.MaxFlagsPerHash > 64 || p.MaxHashesPerUnflag > 200 ||
		p.MaxOperatorsPerBackend > 64 || p.MaxApprovedScanners > 64 {
		return fmt.Errorf("a list cap is above its hard bound")
	}
	if p.MaxUrlBytes < 16 || p.MaxUrlBytes > 2048 {
		return fmt.Errorf("max_url_bytes must be in [16, 2048]")
	}
	if p.MaxReasonBytes > 1024 {
		return fmt.Errorf("max_reason_bytes must be at most 1024")
	}
	if p.ClaimWindowBlocks >= p.RequestTtlBlocks {
		return fmt.Errorf("claim_window_blocks must be below request_ttl_blocks")
	}
	if p.MinUnreachableBlocks >= p.RequestTtlBlocks {
		return fmt.Errorf("min_unreachable_blocks must be below request_ttl_blocks")
	}
	if p.DisputeWindowBlocks < p.SettleWindowBlocks {
		return fmt.Errorf("dispute_window_blocks must not be below settle_window_blocks")
	}
	if p.TerminalRetentionBlocks <= p.FlagHoldBlocks {
		return fmt.Errorf("terminal_retention_blocks must be above flag_hold_blocks")
	}
	return nil
}
