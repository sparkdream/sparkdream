package cli

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/spf13/cobra"
)

// GetTxCmd returns the transaction commands for the shield module.
// Only commands that can't be handled by autocli go here.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "shield",
		Short:                      "Transactions commands for the shield module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdShieldedExec())

	return cmd
}
