package cli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"sparkdream/x/vault/types"
)

const (
	flagExecute = "execute"
	flagMemo    = "memo"
)

// GetTxCmd returns the transaction commands autocli cannot build: the
// repeated VaultSigner set and the repeated Any message list.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(CmdCreateVault(), CmdPropose(), CmdApprove())
	return cmd
}

// parseSigners parses "addr:weight,addr:weight".
func parseSigners(s string) ([]types.VaultSigner, error) {
	var out []types.VaultSigner
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addr, w, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("signer %q: want address:weight", part)
		}
		weight, err := strconv.ParseUint(w, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("signer %q: weight: %w", part, err)
		}
		out = append(out, types.VaultSigner{Address: addr, Weight: weight})
	}
	return out, nil
}

// CmdCreateVault: create-vault <addr:weight,...> <threshold> <proposal-ttl-seconds> [label]
func CmdCreateVault() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-vault [signers] [threshold] [proposal-ttl-seconds] [label]",
		Short: "Create a shared vault (burns the creation fee); signers as addr:weight,addr:weight",
		Args:  cobra.RangeArgs(3, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			signers, err := parseSigners(args[0])
			if err != nil {
				return err
			}
			threshold, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("threshold: %w", err)
			}
			ttl, err := strconv.ParseInt(args[2], 10, 64)
			if err != nil {
				return fmt.Errorf("proposal-ttl: %w", err)
			}
			label := ""
			if len(args) == 4 {
				label = args[3]
			}
			msg := &types.MsgCreateVault{
				Creator: clientCtx.GetFromAddress().String(), Signers: signers,
				Threshold: threshold, ProposalTtl: ttl, Label: label,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

type proposalFile struct {
	Messages []json.RawMessage `json:"messages"`
	Memo     string            `json:"memo"`
}

// CmdPropose: propose <vault-id> <messages.json> [--execute] [--memo]
func CmdPropose() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "propose [vault-id] [messages-file]",
		Short: "Open a vault proposal from a JSON file {\"messages\":[{\"@type\":...}],\"memo\":\"\"}; every message is signed by the vault address",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			vaultID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("vault-id: %w", err)
			}
			raw, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			var pf proposalFile
			if err := json.Unmarshal(raw, &pf); err != nil {
				return err
			}
			pc, ok := clientCtx.Codec.(*codec.ProtoCodec)
			if !ok {
				return fmt.Errorf("codec is not a ProtoCodec")
			}
			var msgs []*codectypes.Any
			for _, r := range pf.Messages {
				var m sdk.Msg
				if err := pc.UnmarshalInterfaceJSON(r, &m); err != nil {
					return err
				}
				a, err := codectypes.NewAnyWithValue(m)
				if err != nil {
					return err
				}
				msgs = append(msgs, a)
			}
			execute, _ := cmd.Flags().GetBool(flagExecute)
			memo, _ := cmd.Flags().GetString(flagMemo)
			if memo == "" {
				memo = pf.Memo
			}
			msg := &types.MsgPropose{
				Signer: clientCtx.GetFromAddress().String(), VaultId: vaultID,
				Messages: msgs, Memo: memo, Execute: execute,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().Bool(flagExecute, false, "execute at once if the threshold is already met")
	cmd.Flags().String(flagMemo, "", "memo (overrides the file's)")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// CmdApprove: approve <proposal-id> <messages-hash-hex> [--execute]. The hash
// is deliberately a required argument: the approver states what they approve.
func CmdApprove() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve [proposal-id] [messages-hash-hex]",
		Short: "Approve a vault proposal; the hash must match the proposal's messages_hash (query tally / get-vault-proposal)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			id, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("proposal-id: %w", err)
			}
			h, err := hex.DecodeString(strings.TrimPrefix(args[1], "0x"))
			if err != nil {
				return fmt.Errorf("messages-hash: %w", err)
			}
			execute, _ := cmd.Flags().GetBool(flagExecute)
			msg := &types.MsgApprove{Signer: clientCtx.GetFromAddress().String(), ProposalId: id, MessagesHash: h, Execute: execute}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().Bool(flagExecute, false, "execute at once if this approval meets the threshold")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
