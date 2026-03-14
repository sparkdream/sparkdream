// Command seed-tle generates test TLE key material for E2E testing of x/shield
// encrypted batch mode. It bypasses the DKG ceremony by directly producing
// a master keypair, a single-validator share, and pre-computed epoch decryption
// keys that can be injected into genesis.json.
//
// Subcommands:
//
//	keygen   - Generate TLE key material (master key, validator share, decryption keys)
//	encrypt  - ECIES-encrypt a payload using an epoch's decryption key
//	payload  - Build + encrypt a MsgShieldedExec fragment containing a blog CreatePost
//
// Usage:
//
//	# 1. Generate key material
//	go run ./tools/zk/cmd/seed-tle keygen \
//	    --validator-addr=<valoper_addr> \
//	    --epochs=10 \
//	    --output=tle_keys.json
//
//	# 2. Encrypt raw bytes (hex) for a given epoch
//	go run ./tools/zk/cmd/seed-tle encrypt \
//	    --key-file=tle_keys.json \
//	    --epoch=0 \
//	    --input-hex=deadbeef
//
//	# 3. Build an encrypted blog CreatePost payload
//	go run ./tools/zk/cmd/seed-tle payload \
//	    --key-file=tle_keys.json \
//	    --epoch=0 \
//	    --shield-addr=<shield_module_addr> \
//	    --title="Test Post" \
//	    --body="Hello from encrypted batch"
package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cosmos/gogoproto/proto"
	any "github.com/cosmos/gogoproto/types/any"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/pairing/bn256"

	blogtypes "sparkdream/x/blog/types"
	shieldtypes "sparkdream/x/shield/types"
)

var suite = bn256.NewSuiteG1()

// KeygenOutput is the JSON output of the keygen subcommand.
type KeygenOutput struct {
	MasterSecretHex    string                `json:"master_secret_hex"`
	MasterPublicKeyB64 string                `json:"master_public_key_b64"`
	ValidatorAddress   string                `json:"validator_address"`
	PublicShareB64     string                `json:"public_share_b64"`
	ShareIndex         uint32                `json:"share_index"`
	DecryptionKeys     map[string]DecKeyInfo `json:"decryption_keys"`
}

// DecKeyInfo holds a decryption key and its derived ECIES public key.
type DecKeyInfo struct {
	DecryptionKeyB64  string `json:"decryption_key_b64"`
	ECIESPublicKeyB64 string `json:"ecies_public_key_b64"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "keygen":
		cmdKeygen(os.Args[2:])
	case "encrypt":
		cmdEncrypt(os.Args[2:])
	case "payload":
		cmdPayload(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: seed-tle <keygen|encrypt|payload> [flags]\n")
	fmt.Fprintf(os.Stderr, "  keygen   Generate TLE key material\n")
	fmt.Fprintf(os.Stderr, "  encrypt  ECIES-encrypt raw bytes for a given epoch\n")
	fmt.Fprintf(os.Stderr, "  payload  Build + encrypt a MsgShieldedExec blog CreatePost\n")
}

// cmdKeygen generates TLE key material.
func cmdKeygen(args []string) {
	var validatorAddr string
	var epochs int
	var outputFile string

	// Simple flag parsing (no flag package to keep it minimal)
	for i := 0; i < len(args); i++ {
		switch {
		case matchFlag(args, i, "--validator-addr"):
			validatorAddr = flagVal(args, &i)
		case matchFlag(args, i, "--epochs"):
			fmt.Sscanf(flagVal(args, &i), "%d", &epochs)
		case matchFlag(args, i, "--output"):
			outputFile = flagVal(args, &i)
		default:
			fatal("unknown flag: %s", args[i])
		}
	}

	if validatorAddr == "" {
		fatal("--validator-addr is required")
	}
	if epochs <= 0 {
		epochs = 10
	}
	if outputFile == "" {
		outputFile = "tle_keys.json"
	}

	// Generate master secret key (random BN256 scalar)
	masterSecret := suite.Scalar().Pick(suite.RandomStream())

	// Compute master public key = masterSecret * G
	masterPublicKey := suite.Point().Mul(masterSecret, nil)

	masterSecretBytes, err := masterSecret.MarshalBinary()
	if err != nil {
		fatal("marshal master secret: %v", err)
	}
	masterPubBytes, err := masterPublicKey.MarshalBinary()
	if err != nil {
		fatal("marshal master public key: %v", err)
	}

	// For a single-validator setup with threshold 1/1, the validator's
	// public share at index 1 equals the master public key.
	// (Feldman with degree-0 polynomial: C_0 = masterSecret * G)
	pubShareBytes := masterPubBytes // Same as master key for single validator

	// Compute epoch decryption keys for epochs 0..N-1
	decKeys := make(map[string]DecKeyInfo)
	for epoch := 0; epoch < epochs; epoch++ {
		epochTag := computeEpochTag(uint64(epoch))
		// decryption_key = masterSecret * H_to_G1("shield_epoch_<N>")
		decryptionKeyPoint := suite.Point().Mul(masterSecret, epochTag)
		decKeyBytes, err := decryptionKeyPoint.MarshalBinary()
		if err != nil {
			fatal("marshal decryption key for epoch %d: %v", epoch, err)
		}

		// Derive the ECIES public key from the decryption key
		// (matches decryptPayload: scalar = XOF(decKeyBytes), pub = scalar * G)
		eciesScalar := suite.Scalar().Pick(suite.XOF(decKeyBytes))
		eciesPub := suite.Point().Mul(eciesScalar, nil)
		eciesPubBytes, err := eciesPub.MarshalBinary()
		if err != nil {
			fatal("marshal ECIES pub key for epoch %d: %v", epoch, err)
		}

		decKeys[fmt.Sprintf("%d", epoch)] = DecKeyInfo{
			DecryptionKeyB64:  base64.StdEncoding.EncodeToString(decKeyBytes),
			ECIESPublicKeyB64: base64.StdEncoding.EncodeToString(eciesPubBytes),
		}
	}

	output := KeygenOutput{
		MasterSecretHex:    hex.EncodeToString(masterSecretBytes),
		MasterPublicKeyB64: base64.StdEncoding.EncodeToString(masterPubBytes),
		ValidatorAddress:   validatorAddr,
		PublicShareB64:     base64.StdEncoding.EncodeToString(pubShareBytes),
		ShareIndex:         1,
		DecryptionKeys:     decKeys,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fatal("marshal output: %v", err)
	}

	if err := os.WriteFile(outputFile, data, 0644); err != nil {
		fatal("write output: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Key material written to %s\n", outputFile)
	fmt.Fprintf(os.Stderr, "  Master public key: %s\n", output.MasterPublicKeyB64)
	fmt.Fprintf(os.Stderr, "  Validator: %s (share index %d)\n", validatorAddr, output.ShareIndex)
	fmt.Fprintf(os.Stderr, "  Decryption keys: epochs 0-%d\n", epochs-1)
}

// cmdEncrypt ECIES-encrypts raw bytes for a given epoch.
func cmdEncrypt(args []string) {
	var keyFile string
	var epoch int
	var inputHex string

	for i := 0; i < len(args); i++ {
		switch {
		case matchFlag(args, i, "--key-file"):
			keyFile = flagVal(args, &i)
		case matchFlag(args, i, "--epoch"):
			fmt.Sscanf(flagVal(args, &i), "%d", &epoch)
		case matchFlag(args, i, "--input-hex"):
			inputHex = flagVal(args, &i)
		default:
			fatal("unknown flag: %s", args[i])
		}
	}

	if keyFile == "" {
		fatal("--key-file is required")
	}
	if inputHex == "" {
		fatal("--input-hex is required")
	}

	keys := loadKeys(keyFile)
	plaintext, err := hex.DecodeString(inputHex)
	if err != nil {
		fatal("decode input hex: %v", err)
	}

	ciphertext := encryptForEpoch(keys, uint64(epoch), plaintext)
	fmt.Print(base64.StdEncoding.EncodeToString(ciphertext))
}

// cmdPayload builds and encrypts a MsgShieldedExec containing a blog CreatePost.
func cmdPayload(args []string) {
	var keyFile string
	var epoch int
	var shieldAddr string
	var title, body string

	for i := 0; i < len(args); i++ {
		switch {
		case matchFlag(args, i, "--key-file"):
			keyFile = flagVal(args, &i)
		case matchFlag(args, i, "--epoch"):
			fmt.Sscanf(flagVal(args, &i), "%d", &epoch)
		case matchFlag(args, i, "--shield-addr"):
			shieldAddr = flagVal(args, &i)
		case matchFlag(args, i, "--title"):
			title = flagVal(args, &i)
		case matchFlag(args, i, "--body"):
			body = flagVal(args, &i)
		default:
			fatal("unknown flag: %s", args[i])
		}
	}

	if keyFile == "" {
		fatal("--key-file is required")
	}
	if shieldAddr == "" {
		fatal("--shield-addr is required")
	}
	if title == "" {
		title = "ShieldBatchTest"
	}
	if body == "" {
		body = "Encrypted batch test post"
	}

	keys := loadKeys(keyFile)

	// Build inner message: MsgCreatePost with shield module as creator
	innerMsg := &blogtypes.MsgCreatePost{
		Creator: shieldAddr,
		Title:   title,
		Body:    body,
	}

	innerMsgBytes, err := proto.Marshal(innerMsg)
	if err != nil {
		fatal("marshal inner message: %v", err)
	}

	// Build MsgShieldedExec fragment (only InnerMessage — other fields
	// are cleartext and restored from the PendingShieldedOp at execution time)
	execFragment := &shieldtypes.MsgShieldedExec{
		InnerMessage: &any.Any{
			TypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
			Value:   innerMsgBytes,
		},
	}

	fragmentBytes, err := proto.Marshal(execFragment)
	if err != nil {
		fatal("marshal exec fragment: %v", err)
	}

	// Encrypt with the epoch's ECIES key
	ciphertext := encryptForEpoch(keys, uint64(epoch), fragmentBytes)

	// Output as base64
	fmt.Print(base64.StdEncoding.EncodeToString(ciphertext))
}

// encryptForEpoch ECIES-encrypts plaintext using the derived public key for an epoch.
func encryptForEpoch(keys KeygenOutput, epoch uint64, plaintext []byte) []byte {
	epochStr := fmt.Sprintf("%d", epoch)
	dk, ok := keys.DecryptionKeys[epochStr]
	if !ok {
		fatal("no decryption key for epoch %d", epoch)
	}

	eciesPubBytes, err := base64.StdEncoding.DecodeString(dk.ECIESPublicKeyB64)
	if err != nil {
		fatal("decode ECIES public key: %v", err)
	}

	eciesPub := suite.Point()
	if err := eciesPub.UnmarshalBinary(eciesPubBytes); err != nil {
		fatal("unmarshal ECIES public key: %v", err)
	}

	ciphertext, err := ecies.Encrypt(suite, eciesPub, plaintext, nil)
	if err != nil {
		fatal("ECIES encrypt: %v", err)
	}

	return ciphertext
}

// computeEpochTag computes H_to_G1("shield_epoch_<N>") matching tle_crypto.go.
func computeEpochTag(epoch uint64) kyber.Point {
	data := fmt.Appendf(nil, "shield_epoch_%d", epoch)
	return suite.Point().Pick(suite.XOF(data))
}

// loadKeys reads the keygen output JSON file.
func loadKeys(path string) KeygenOutput {
	data, err := os.ReadFile(path)
	if err != nil {
		fatal("read key file: %v", err)
	}
	var keys KeygenOutput
	if err := json.Unmarshal(data, &keys); err != nil {
		fatal("parse key file: %v", err)
	}
	return keys
}

// Flag parsing helpers

func matchFlag(args []string, i int, name string) bool {
	if i >= len(args) {
		return false
	}
	// Support --flag=value and --flag value
	return args[i] == name || len(args[i]) > len(name) && args[i][:len(name)+1] == name+"="
}

func flagVal(args []string, i *int) string {
	arg := args[*i]
	if idx := indexOf(arg, '='); idx >= 0 {
		return arg[idx+1:]
	}
	*i++
	if *i >= len(args) {
		fatal("missing value for flag %s", arg)
	}
	return args[*i]
}

func indexOf(s string, c byte) int {
	for i := range s {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
