package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"

	"sparkdream/tools/fileprofile"
	"sparkdream/x/storage/types"
)

// GetTxCmd returns the transaction commands autocli cannot build: the ones
// that read a local file.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(CmdRequestFileCopy())
	return cmd
}

// fileRequest reads a local file and builds the request for it: the hash and
// the size are computed here, and a file outside the storage profile of its
// type is refused before anything is paid.
func fileRequest(requester, backend, path, contentType, url string, maxFee math.Int) (*types.MsgRequestPermanentCopy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s is empty", path)
	}
	if err := fileprofile.Check(contentType, data); err != nil {
		return nil, fmt.Errorf("%s would be declined by storage operators: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return &types.MsgRequestPermanentCopy{
		Requester:   requester,
		Backend:     backend,
		Sha256:      hex.EncodeToString(sum[:]),
		SizeBytes:   uint64(len(data)),
		ContentType: contentType,
		Url:         url,
		MaxFee:      maxFee,
	}, nil
}

// CmdRequestFileCopy requests a permanent copy of a local file. Nothing is
// uploaded: the file must already be served at the given address.
func CmdRequestFileCopy() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request-file-copy [backend] [file] [content-type] [url] [max-fee]",
		Short: "Request a permanent copy of a local file that is already served at url",
		Long: `Request a permanent copy (MsgRequestPermanentCopy) of a local file.

The command computes the file's sha256 and size, and checks the file against
the storage profile of its content type (one still image, no transparency,
no metadata, no colour management). A file outside the profile is refused
here, naming what failed, so nothing is paid for a request an operator would
decline. Nothing is uploaded: url must already serve these exact bytes.

The request and its address are public and permanent.`,
		Args: cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			maxFee, ok := math.NewIntFromString(args[4])
			if !ok || maxFee.IsNegative() {
				return fmt.Errorf("max-fee must be a non-negative integer")
			}
			msg, err := fileRequest(clientCtx.GetFromAddress().String(), args[0], args[1], args[2], args[3], maxFee)
			if err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
