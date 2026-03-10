package cli

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/spf13/cobra"
)

// GetTxCmd returns the transaction commands for this module.
// Only commands that can't be handled by autocli go here.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "commons",
		Short:                      "Transactions commands for the commons module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdSubmitProposal())

	return cmd
}
