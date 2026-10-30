package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
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
	cmd.AddCommand(CmdCreateRecurringPull(), CmdCreateGrant(), CmdExecSession())
	return cmd
}

// CmdCreateGrant creates a grant of any payload type from a JSON file
// holding the MsgCreateGrant payload oneof, e.g.
// {"session_key": {"allowed_msg_types": [...], "spend_limit": {...}, "max_exec_count": "100"}}
// or {"scheduled_oneshot": {...}}, {"spending_allowance": {...}},
// {"sign_in": {"service": "login.example.org"}}. The granter is the
// signer; grantee, expiry and note come from the arguments and flags.
func CmdCreateGrant() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-grant [grantee] [expires-at] [payload-json-file]",
		Short: "Create a grant of any type from a JSON payload file",
		Long: `Create a grant (MsgCreateGrant). payload-json-file holds the payload oneof
in proto-JSON, for example {"session_key": {...}}, {"spending_allowance": {...}},
{"scheduled_oneshot": {...}}, {"recurring_pull": {...}} or {"sign_in": {...}}.
expires-at is RFC 3339 or +<duration> relative to now.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			expires, err := parseExpiry(args[1], time.Now())
			if err != nil {
				return err
			}
			bz, err := os.ReadFile(args[2])
			if err != nil {
				return err
			}
			msg := &types.MsgCreateGrant{}
			if err := clientCtx.Codec.UnmarshalJSON(bz, msg); err != nil {
				return fmt.Errorf("payload-json-file: %w", err)
			}
			if msg.Payload == nil {
				return fmt.Errorf("payload-json-file sets no payload")
			}
			note, _ := cmd.Flags().GetString(flagNote)
			msg.Granter = clientCtx.GetFromAddress().String()
			msg.Grantee = args[0]
			msg.ExpiresAt = expires
			msg.Note = note
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagNote, "", "Human-readable purpose (256 characters max)")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// CmdExecSession executes messages as granter through the signer's session
// key. msgs-json-file holds a JSON array of messages in proto-JSON with
// "@type"; each one's signer field is rewritten to the granter on chain.
func CmdExecSession() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec-session [granter] [msgs-json-file]",
		Short: "Execute messages as granter through your session key",
		Long: `Execute up to 10 messages as granter (MsgExecSession). msgs-json-file holds
a JSON array of messages in proto-JSON with "@type", e.g.
[{"@type": "/sparkdream.blog.v1.MsgCreatePost", "creator": "<granter>", ...}].
The fee is paid by the granter, within the session's spend limit.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			bz, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			var raw []json.RawMessage
			if err := json.Unmarshal(bz, &raw); err != nil {
				return fmt.Errorf("msgs-json-file must be a JSON array: %w", err)
			}
			if len(raw) == 0 {
				return fmt.Errorf("msgs-json-file holds no messages")
			}
			anys := make([]*codectypes.Any, 0, len(raw))
			for i, r := range raw {
				var m sdk.Msg
				if err := clientCtx.Codec.UnmarshalInterfaceJSON(r, &m); err != nil {
					return fmt.Errorf("message %d: %w", i, err)
				}
				a, err := codectypes.NewAnyWithValue(m)
				if err != nil {
					return fmt.Errorf("message %d: %w", i, err)
				}
				anys = append(anys, a)
			}
			msg := &types.MsgExecSession{
				Grantee: clientCtx.GetFromAddress().String(),
				Granter: args[0],
				Msgs:    anys,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
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
