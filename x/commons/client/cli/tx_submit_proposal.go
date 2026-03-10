package cli

import (
	"encoding/json"
	"os"

	"sparkdream/x/commons/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

// proposalJSON is the JSON file format for submit-proposal.
type proposalJSON struct {
	PolicyAddress string            `json:"policy_address"`
	Messages      []json.RawMessage `json:"messages"`
	Metadata      string            `json:"metadata"`
}

// CmdSubmitProposal returns a CLI command to submit a council proposal from a JSON file.
func CmdSubmitProposal() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit-proposal [proposal-file]",
		Short: "Submit a council proposal from a JSON file",
		Long: `Submit a proposal to a council policy from a JSON file.

The JSON file should have the format:
{
  "policy_address": "<council-policy-address>",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSomething",
      "authority": "<policy-address>",
      ...
    }
  ],
  "metadata": "optional description"
}`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			proposalFile := args[0]
			contents, err := os.ReadFile(proposalFile)
			if err != nil {
				return err
			}

			var proposal proposalJSON
			if err := json.Unmarshal(contents, &proposal); err != nil {
				return err
			}

			// Convert raw JSON messages to Any types
			cdc := clientCtx.Codec
			var msgs []*codectypes.Any
			for _, rawMsg := range proposal.Messages {
				var sdkMsg sdk.Msg
				if err := cdc.(*codec.ProtoCodec).UnmarshalInterfaceJSON(rawMsg, &sdkMsg); err != nil {
					return err
				}
				anyMsg, err := codectypes.NewAnyWithValue(sdkMsg)
				if err != nil {
					return err
				}
				msgs = append(msgs, anyMsg)
			}

			proposer := clientCtx.GetFromAddress().String()

			msg := &types.MsgSubmitProposal{
				Proposer:      proposer,
				PolicyAddress: proposal.PolicyAddress,
				Messages:      msgs,
				Metadata:      proposal.Metadata,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
