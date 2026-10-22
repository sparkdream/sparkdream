package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"sparkdream/x/session/types"
)

const (
	flagMaxPerEpoch = "max-per-epoch"
	flagStartTime   = "start-time"
	flagNote        = "grant-note"
)

// GetTxCmd returns the transaction commands autocli cannot build: the
// payload-typed MsgCreateGrant variants.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(CmdCreateRecurringPull())
	return cmd
}

// CmdCreateRecurringPull creates a RECURRING_PULL grant: the grantee may
// pull amount every period until expires-at (docs/x-session-spec.md). An
// x/artifact Harberger holder grants one to the artifact module address
// to pay the token's tax.
func CmdCreateRecurringPull() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-recurring-pull [grantee] [amount-per-period] [period-seconds] [expires-at]",
		Short: "Grant a periodic pull of your funds (RECURRING_PULL)",
		Long: `Create a RECURRING_PULL grant: the grantee may claim amount-per-period once
every period-seconds until expires-at (RFC 3339, e.g. 2027-01-31T00:00:00Z,
or +<duration> relative to now, e.g. +720h).

--max-per-epoch caps what may be pulled per UTC day (defaults to the amount
per period); --start-time (unix seconds, default now) sets when the first
period starts.`,
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			amount, err := sdk.ParseCoinNormalized(args[1])
			if err != nil {
				return fmt.Errorf("amount-per-period: %w", err)
			}
			period, err := strconv.ParseInt(args[2], 10, 64)
			if err != nil {
				return fmt.Errorf("period-seconds: %w", err)
			}
			expires, err := parseExpiry(args[3], time.Now())
			if err != nil {
				return err
			}
			maxPerEpoch, _ := cmd.Flags().GetString(flagMaxPerEpoch)
			if maxPerEpoch == "" {
				maxPerEpoch = amount.Amount.String()
			}
			start, _ := cmd.Flags().GetInt64(flagStartTime)
			note, _ := cmd.Flags().GetString(flagNote)
			msg := &types.MsgCreateGrant{
				Granter:   clientCtx.GetFromAddress().String(),
				Grantee:   args[0],
				ExpiresAt: expires,
				Note:      note,
				Payload: &types.MsgCreateGrant_RecurringPull{RecurringPull: &types.RecurringPullPayload{
					AmountPerPeriod: amount,
					PeriodSeconds:   period,
					StartTime:       start,
					MaxPerEpoch:     maxPerEpoch,
				}},
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagMaxPerEpoch, "", "Per-UTC-day pull cap in the amount's denom (default: amount-per-period)")
	cmd.Flags().Int64(flagStartTime, 0, "Unix seconds the first period starts (default: now)")
	cmd.Flags().String(flagNote, "", "Human-readable purpose (256 characters max)")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// parseExpiry reads an RFC 3339 time or a +duration relative to now.
func parseExpiry(s string, now time.Time) (time.Time, error) {
	if len(s) > 1 && s[0] == '+' {
		d, err := time.ParseDuration(s[1:])
		if err != nil {
			return time.Time{}, fmt.Errorf("expires-at: %w", err)
		}
		return now.Add(d).UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("expires-at must be RFC 3339 or +duration: %w", err)
	}
	return t.UTC(), nil
}
