package cli

import (
	"encoding/hex"
	"fmt"
	"strconv"

	"sparkdream/x/shield/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	gogoanytypes "github.com/cosmos/gogoproto/types/any"
)

const (
	FlagInnerMessage       = "inner-message"
	FlagProof              = "proof"
	FlagNullifier          = "nullifier"
	FlagRateLimitNullifier = "rate-limit-nullifier"
	FlagMerkleRoot         = "merkle-root"
	FlagProofDomain        = "proof-domain"
	FlagMinTrustLevel      = "min-trust-level"
	FlagExecMode           = "exec-mode"
	FlagEncryptedPayload   = "encrypted-payload"
	FlagTargetEpoch        = "target-epoch"
)

// CmdShieldedExec returns a CLI command to submit a shielded execution.
func CmdShieldedExec() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shielded-exec",
		Short: "Submit a shielded execution",
		Long: `Submit a shielded execution through the shield module.

For immediate mode, provide --inner-message (JSON with @type), --proof, and set --exec-mode 0.
For encrypted batch mode, provide --encrypted-payload, --target-epoch, and set --exec-mode 1.

Example (immediate mode):
  sparkdreamd tx shield shielded-exec \
    --inner-message '{"@type":"/sparkdream.blog.v1.MsgCreatePost","creator":"<shield-module-addr>","title":"Anon","body":"Hello"}' \
    --proof deadbeef \
    --nullifier <hex> \
    --rate-limit-nullifier <hex> \
    --merkle-root <hex> \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from <submitter> \
    --fees 500000uspark`,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			submitter := clientCtx.GetFromAddress().String()

			// Parse nullifier (hex)
			nullifierHex, err := cmd.Flags().GetString(FlagNullifier)
			if err != nil {
				return err
			}
			nullifier, err := hex.DecodeString(nullifierHex)
			if err != nil {
				return fmt.Errorf("invalid nullifier hex: %w", err)
			}

			// Parse rate-limit nullifier (hex)
			rateLimitHex, err := cmd.Flags().GetString(FlagRateLimitNullifier)
			if err != nil {
				return err
			}
			rateLimitNullifier, err := hex.DecodeString(rateLimitHex)
			if err != nil {
				return fmt.Errorf("invalid rate-limit-nullifier hex: %w", err)
			}

			// Parse merkle root (hex)
			merkleRootHex, err := cmd.Flags().GetString(FlagMerkleRoot)
			if err != nil {
				return err
			}
			merkleRoot, err := hex.DecodeString(merkleRootHex)
			if err != nil {
				return fmt.Errorf("invalid merkle-root hex: %w", err)
			}

			// Parse proof domain
			proofDomainInt, err := cmd.Flags().GetUint32(FlagProofDomain)
			if err != nil {
				return err
			}
			proofDomain := types.ProofDomain(proofDomainInt)

			// Parse min trust level
			minTrustLevel, err := cmd.Flags().GetUint32(FlagMinTrustLevel)
			if err != nil {
				return err
			}

			// Parse exec mode
			execModeInt, err := cmd.Flags().GetUint32(FlagExecMode)
			if err != nil {
				return err
			}
			execMode := types.ShieldExecMode(execModeInt)

			msg := &types.MsgShieldedExec{
				Submitter:          submitter,
				Nullifier:          nullifier,
				RateLimitNullifier: rateLimitNullifier,
				MerkleRoot:         merkleRoot,
				ProofDomain:        proofDomain,
				MinTrustLevel:      minTrustLevel,
				ExecMode:           execMode,
			}

			// Handle immediate mode fields
			if execMode == types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE {
				// Parse inner message JSON -> Any
				innerMsgJSON, err := cmd.Flags().GetString(FlagInnerMessage)
				if err != nil {
					return err
				}
				if innerMsgJSON == "" {
					return fmt.Errorf("--inner-message is required for immediate mode")
				}

				cdc := clientCtx.Codec
				var sdkMsg sdk.Msg
				if err := cdc.(*codec.ProtoCodec).UnmarshalInterfaceJSON([]byte(innerMsgJSON), &sdkMsg); err != nil {
					return fmt.Errorf("failed to parse inner-message JSON: %w", err)
				}
				anyMsg, err := codectypes.NewAnyWithValue(sdkMsg)
				if err != nil {
					return fmt.Errorf("failed to encode inner-message as Any: %w", err)
				}

				// Convert cosmos-sdk Any to gogoproto Any
				msg.InnerMessage = &gogoanytypes.Any{
					TypeUrl: anyMsg.TypeUrl,
					Value:   anyMsg.Value,
				}

				// Parse proof (hex)
				proofHex, err := cmd.Flags().GetString(FlagProof)
				if err != nil {
					return err
				}
				proof, err := hex.DecodeString(proofHex)
				if err != nil {
					return fmt.Errorf("invalid proof hex: %w", err)
				}
				msg.Proof = proof
			} else {
				// Handle encrypted batch mode fields
				encPayloadHex, err := cmd.Flags().GetString(FlagEncryptedPayload)
				if err != nil {
					return err
				}
				if encPayloadHex != "" {
					encPayload, err := hex.DecodeString(encPayloadHex)
					if err != nil {
						return fmt.Errorf("invalid encrypted-payload hex: %w", err)
					}
					msg.EncryptedPayload = encPayload
				}

				targetEpochStr, err := cmd.Flags().GetString(FlagTargetEpoch)
				if err != nil {
					return err
				}
				if targetEpochStr != "" {
					targetEpoch, err := strconv.ParseUint(targetEpochStr, 10, 64)
					if err != nil {
						return fmt.Errorf("invalid target-epoch: %w", err)
					}
					msg.TargetEpoch = targetEpoch
				}
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(FlagInnerMessage, "", "Inner message as JSON with @type (immediate mode)")
	cmd.Flags().String(FlagProof, "", "ZK proof bytes as hex (immediate mode)")
	cmd.Flags().String(FlagNullifier, "", "32-byte nullifier as hex")
	cmd.Flags().String(FlagRateLimitNullifier, "", "32-byte rate-limit nullifier as hex")
	cmd.Flags().String(FlagMerkleRoot, "", "Merkle root as hex")
	cmd.Flags().Uint32(FlagProofDomain, 1, "Proof domain (1=TRUST_TREE)")
	cmd.Flags().Uint32(FlagMinTrustLevel, 0, "Minimum trust level proven")
	cmd.Flags().Uint32(FlagExecMode, 0, "Execution mode (0=IMMEDIATE, 1=ENCRYPTED_BATCH)")
	cmd.Flags().String(FlagEncryptedPayload, "", "Encrypted payload as hex (encrypted batch mode)")
	cmd.Flags().String(FlagTargetEpoch, "", "Target shield epoch (encrypted batch mode)")

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
