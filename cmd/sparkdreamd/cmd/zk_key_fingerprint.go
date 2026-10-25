package cmd

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/spf13/cobra"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/tools/zk/prover"
	reptypes "sparkdream/x/rep/types"
)

// ZkKeyFingerprintCmd prints the fingerprint of a shield public key so the
// co-signers of a multisig member can check *whose* key a
// MsgRegisterZkPublicKey carries before they sign it (multisig-accounts plan
// §2.1). The threshold only controls whether a key is registered; a
// compromised device could slip an attacker's key into the tx, and co-signers
// who sign blindly would hand over the member's anonymous identity.
//
//	sparkdreamd zk-key-fingerprint tx <unsigned-tx.json>   fingerprint of each MsgRegisterZkPublicKey key in the tx
//	sparkdreamd zk-key-fingerprint key <hex-public-key>    fingerprint of a raw 32-byte public key
//	sparkdreamd zk-key-fingerprint seed <seed> [salt]      public key + fingerprint derived from a shield seed
//
// A device holding the seed runs `seed`; a device that does not runs `tx` and
// compares the two fingerprints by eye. The seed is the person's 256-bit shield
// seed (never a passphrase; the derivation is an unstretched hash).
func ZkKeyFingerprintCmd(txConfig client.TxConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "zk-key-fingerprint",
		Short: "Fingerprint a shield (ZK) public key before co-signing its registration",
		Long: `Prints a short fingerprint (first 8 bytes of SHA-256 of the key, hex in
groups of four) of a shield public key. Co-signers of a multisig member compare
the fingerprint of the key inside a MsgRegisterZkPublicKey tx with the one a
seed-holding device derives, and refuse to sign on a mismatch.`,
	}
	cmd.AddCommand(zkFingerprintTxCmd(txConfig), zkFingerprintKeyCmd(), zkFingerprintSeedCmd())
	return cmd
}

func zkFingerprintTxCmd(txConfig client.TxConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "tx <tx.json>",
		Short: "Fingerprint every MsgRegisterZkPublicKey key in a (generate-only) tx file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			tx, err := txConfig.TxJSONDecoder()(raw)
			if err != nil {
				return fmt.Errorf("decode tx: %w", err)
			}
			found := 0
			for i, msg := range tx.GetMsgs() {
				reg, ok := msg.(*reptypes.MsgRegisterZkPublicKey)
				if !ok {
					continue
				}
				found++
				fmt.Fprintf(cmd.OutOrStdout(), "msg %d: member %s\n  key         %s\n  fingerprint %s\n",
					i, reg.Member, hex.EncodeToString(reg.ZkPublicKey), zkcrypto.ZkKeyFingerprint(reg.ZkPublicKey))
			}
			if found == 0 {
				return fmt.Errorf("no MsgRegisterZkPublicKey in %s", args[0])
			}
			return nil
		},
	}
}

func zkFingerprintKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "key <hex-public-key>",
		Short: "Fingerprint a raw 32-byte shield public key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := hex.DecodeString(strings.TrimPrefix(args[0], "0x"))
			if err != nil {
				return fmt.Errorf("decode key: %w", err)
			}
			if len(key) != 32 {
				return fmt.Errorf("key must be 32 bytes, got %d", len(key))
			}
			fmt.Fprintln(cmd.OutOrStdout(), zkcrypto.ZkKeyFingerprint(key))
			return nil
		},
	}
}

func zkFingerprintSeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed <seed> [salt]",
		Short: "Derive the shield public key from a seed and print its fingerprint",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			salt := ""
			if len(args) == 2 {
				salt = args[1]
			}
			sk := prover.DeriveSecretKeyFromSeed(args[0], salt)
			pk := prover.GetPublicKey(sk)
			fmt.Fprintf(cmd.OutOrStdout(), "key         %s\nfingerprint %s\n",
				hex.EncodeToString(pk), zkcrypto.ZkKeyFingerprint(pk))
			return nil
		},
	}
}
