package keeper

import (
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/hashicorp/go-metrics"
)

// telemetry.go centralises the §14 metric emission. Telemetry calls
// are no-ops when telemetry is disabled (the SDK's wrapper functions
// short-circuit). Metric names map 1:1 with the spec table.
//
// Naming convention: dots translate to a sequence of key segments per
// the SDK's telemetry convention — e.g. `service.operator.registered`
// is recorded as `[]string{"service", "operator", "registered"}`. The
// labels are emitted as Prometheus / Datadog tags via
// `telemetry.NewLabel`.

func emitOperatorRegistered(serviceType string) {
	telemetry.IncrCounterWithLabels(
		[]string{"service", "operator", "registered"}, 1,
		[]metrics.Label{telemetry.NewLabel("service_type", serviceType)},
	)
}

func emitOperatorUnbondStarted(serviceType, source string) {
	telemetry.IncrCounterWithLabels(
		[]string{"service", "operator", "unbond_started"}, 1,
		[]metrics.Label{
			telemetry.NewLabel("service_type", serviceType),
			telemetry.NewLabel("source", source),
		},
	)
}

func emitOperatorDissolved(serviceType, tier string) {
	telemetry.IncrCounterWithLabels(
		[]string{"service", "operator", "dissolved"}, 1,
		[]metrics.Label{
			telemetry.NewLabel("service_type", serviceType),
			telemetry.NewLabel("tier", tier),
		},
	)
}

func emitOperatorLiveCount(serviceType, status string, count float32) {
	telemetry.SetGaugeWithLabels(
		[]string{"service", "operator", "live_count"}, count,
		[]metrics.Label{
			telemetry.NewLabel("service_type", serviceType),
			telemetry.NewLabel("status", status),
		},
	)
}

func emitBondLocked(totalUSpark float32) {
	telemetry.SetGauge(totalUSpark, "service", "bond", "locked")
}

func emitReportFiled(serviceType string) {
	telemetry.IncrCounterWithLabels(
		[]string{"service", "report", "filed"}, 1,
		[]metrics.Label{telemetry.NewLabel("service_type", serviceType)},
	)
}

func emitReportResolved(serviceType, tier, verdict string) {
	telemetry.IncrCounterWithLabels(
		[]string{"service", "report", "resolved"}, 1,
		[]metrics.Label{
			telemetry.NewLabel("service_type", serviceType),
			telemetry.NewLabel("tier", tier),
			telemetry.NewLabel("verdict", verdict),
		},
	)
}

func emitTier1EscrowInFlight(totalUSpark float32) {
	telemetry.SetGauge(totalUSpark, "service", "tier1_escrow", "in_flight")
}

func emitSlashAmount(serviceType, tier string, amountUSpark float32) {
	telemetry.IncrCounterWithLabels(
		[]string{"service", "slash", "amount"}, amountUSpark,
		[]metrics.Label{
			telemetry.NewLabel("service_type", serviceType),
			telemetry.NewLabel("tier", tier),
		},
	)
}

func emitControllerTransferCaseOpened(serviceType string) {
	telemetry.IncrCounterWithLabels(
		[]string{"service", "controller_transfer_case", "opened"}, 1,
		[]metrics.Label{telemetry.NewLabel("service_type", serviceType)},
	)
}

func emitControllerTransferCaseFinalized(serviceType, verdict string) {
	telemetry.IncrCounterWithLabels(
		[]string{"service", "controller_transfer_case", "finalized"}, 1,
		[]metrics.Label{
			telemetry.NewLabel("service_type", serviceType),
			telemetry.NewLabel("verdict", verdict),
		},
	)
}

func emitSweepDuration(queue string, start time.Time) {
	telemetry.ModuleMeasureSince("service", start, "endblocker", "sweep_duration_ms", queue)
}

func emitSweptRecords(queue string, count float32) {
	telemetry.IncrCounterWithLabels(
		[]string{"service", "endblocker", "swept_records"}, count,
		[]metrics.Label{telemetry.NewLabel("queue", queue)},
	)
}
