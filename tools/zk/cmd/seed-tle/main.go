// Command seed-tle seeds and exercises x/shield encrypted batch mode without
// a multi-validator DKG: it produces a master keypair, a single-validator
// share and pre-computed epoch decryption keys for genesis, and encrypts
// payloads the way any client does (to the G2 master public key only).
//
// Subcommands:
//
//	keygen   - Generate TLE key material (master keys, validator share, decryption keys)
//	encrypt  - Encrypt raw bytes for a given epoch
//	payload  - Build + encrypt a MsgShieldedExec fragment containing a blog CreatePost
//	decrypt  - Open a payload with an epoch decryption key (debugging)
//
// encrypt and payload take the master public key either from the keygen
// file (--key-file) or straight from the chain (--mpk-g2, the
// master_public_key_g2 of `q shield tle-master-public-key`); they never need
// secret material. The crypto is tools/tle, shared with the chain.
//
// Usage:
//
//	go run ./tools/zk/cmd/seed-tle keygen --validator-addr=<valoper> --epochs=10 --output=tle_keys.json
//	go run ./tools/zk/cmd/seed-tle encrypt --mpk-g2=<b64> --epoch=0 --input-hex=deadbeef
//	go run ./tools/zk/cmd/seed-tle payload --mpk-g2=<b64> --epoch=0 --shield-addr=<addr> \
//	    --title="Test Post" --body="Hello from encrypted batch" [--proof-b64=<b64>]
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cosmos/gogoproto/proto"
	"go.dedis.ch/kyber/v4/util/random"

	"sparkdream/tools/tle"
	blogtypes "sparkdream/x/blog/types"
	shieldtypes "sparkdream/x/shield/types"
)

// KeygenOutput is the JSON output of the keygen subcommand.
type KeygenOutput struct {
	MasterSecretHex      string                `json:"master_secret_hex"`
	MasterPublicKeyB64   string                `json:"master_public_key_b64"`
	MasterPublicKeyG2B64 string                `json:"master_public_key_g2_b64"`
	ValidatorAddress     string                `json:"validator_address"`
	PublicShareB64       string                `json:"public_share_b64"`
	PublicShareG2B64     string                `json:"public_share_g2_b64"`
	ShareIndex           uint32                `json:"share_index"`
	DecryptionKeys       map[string]DecKeyInfo `json:"decryption_keys"`
}

// DecKeyInfo holds an epoch's decryption key s*H1(epoch).
type DecKeyInfo struct {
	DecryptionKeyB64 string `json:"decryption_key_b64"`
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
	case "decrypt":
		cmdDecrypt(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: seed-tle <keygen|encrypt|payload|decrypt> [flags]\n")
	fmt.Fprintf(os.Stderr, "  keygen   Generate TLE key material\n")
	fmt.Fprintf(os.Stderr, "  encrypt  Encrypt raw bytes for a given epoch (--key-file or --mpk-g2)\n")
	fmt.Fprintf(os.Stderr, "  payload  Build + encrypt a MsgShieldedExec blog CreatePost (--key-file or --mpk-g2)\n")
	fmt.Fprintf(os.Stderr, "  decrypt  Open a payload with an epoch decryption key\n")
}

// cmdKeygen generates TLE key material.
func cmdKeygen(args []string) {
	var validatorAddr string
	var epochs int
	var outputFile string

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

	masterSecret := tle.Suite.G1().Scalar().Pick(random.New())
	masterSecretBytes, err := masterSecret.MarshalBinary()
	if err != nil {
		fatal("marshal master secret: %v", err)
	}
	mpkG1 := tle.MasterPublicKeyG1(masterSecret)
	mpkG2 := tle.MasterPublicKeyG2(masterSecret)

	// Single validator, threshold 1/1: a degree-0 polynomial, so the
	// validator's share is the master secret and its public shares are the
	// master public keys.
	decKeys := make(map[string]DecKeyInfo, epochs)
	for epoch := 0; epoch < epochs; epoch++ {
		decKeys[fmt.Sprintf("%d", epoch)] = DecKeyInfo{
			DecryptionKeyB64: base64.StdEncoding.EncodeToString(tle.EpochDecryptionKey(masterSecret, uint64(epoch))),
		}
	}

	output := KeygenOutput{
		MasterSecretHex:      hex.EncodeToString(masterSecretBytes),
		MasterPublicKeyB64:   base64.StdEncoding.EncodeToString(mpkG1),
		MasterPublicKeyG2B64: base64.StdEncoding.EncodeToString(mpkG2),
		ValidatorAddress:     validatorAddr,
		PublicShareB64:       base64.StdEncoding.EncodeToString(mpkG1),
		PublicShareG2B64:     base64.StdEncoding.EncodeToString(mpkG2),
		ShareIndex:           1,
		DecryptionKeys:       decKeys,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fatal("marshal output: %v", err)
	}
	// 0600: the file holds the master secret.
	if err := os.WriteFile(outputFile, data, 0o600); err != nil {
		fatal("write output: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Key material written to %s\n", outputFile)
	fmt.Fprintf(os.Stderr, "  Master public key (G2, encrypt to this): %s\n", output.MasterPublicKeyG2B64)
	fmt.Fprintf(os.Stderr, "  Validator: %s (share index %d)\n", validatorAddr, output.ShareIndex)
	fmt.Fprintf(os.Stderr, "  Decryption keys: epochs 0-%d\n", epochs-1)
}

// keySource resolves the G2 master public key from --key-file or --mpk-g2.
type keySource struct {
	keyFile string
	mpkG2   string
}

func (k *keySource) match(args []string, i *int) bool {
	switch {
	case matchFlag(args, *i, "--key-file"):
		k.keyFile = flagVal(args, i)
	case matchFlag(args, *i, "--mpk-g2"):
		k.mpkG2 = flagVal(args, i)
	default:
		return false
	}
	return true
}

func (k *keySource) masterPublicKeyG2() []byte {
	b64 := k.mpkG2
	if b64 == "" {
		if k.keyFile == "" {
			fatal("--key-file or --mpk-g2 is required")
		}
		b64 = loadKeys(k.keyFile).MasterPublicKeyG2B64
		if b64 == "" {
			fatal("key file has no master_public_key_g2_b64; re-run keygen")
		}
	}
	bz, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		fatal("decode master public key: %v", err)
	}
	return bz
}

// cmdEncrypt encrypts raw bytes for a given epoch.
func cmdEncrypt(args []string) {
	var ks keySource
	var epoch int
	var inputHex string

	for i := 0; i < len(args); i++ {
		switch {
		case ks.match(args, &i):
		case matchFlag(args, i, "--epoch"):
			fmt.Sscanf(flagVal(args, &i), "%d", &epoch)
		case matchFlag(args, i, "--input-hex"):
			inputHex = flagVal(args, &i)
		default:
			fatal("unknown flag: %s", args[i])
		}
	}
	if inputHex == "" {
		fatal("--input-hex is required")
	}
	plaintext, err := hex.DecodeString(inputHex)
	if err != nil {
		fatal("decode input hex: %v", err)
	}
	ct, err := tle.Encrypt(ks.masterPublicKeyG2(), uint64(epoch), plaintext)
	if err != nil {
		fatal("encrypt: %v", err)
	}
	fmt.Print(base64.StdEncoding.EncodeToString(ct))
}

// cmdPayload builds and encrypts a MsgShieldedExec fragment containing a
// blog CreatePost. --proof-b64 attaches the operation's Groth16 proof and
// --nullifier-hex its nullifier (on a chain with a verifying key the batch
// verifies both at execution; test builds without one accept an empty proof
// and any 32-byte nullifier, which is generated when the flag is absent).
// The submission proof the outer MsgShieldedExec needs is bound to the
// printed payload: prove with domain BatchSubmissionDomain, scope
// BatchSubmissionScope(payload) and the payload as the message; both are
// printed to stderr.
func cmdPayload(args []string) {
	var ks keySource
	var epoch int
	var shieldAddr string
	var title, body, proofB64, nullifierHex string

	for i := 0; i < len(args); i++ {
		switch {
		case ks.match(args, &i):
		case matchFlag(args, i, "--epoch"):
			fmt.Sscanf(flagVal(args, &i), "%d", &epoch)
		case matchFlag(args, i, "--shield-addr"):
			shieldAddr = flagVal(args, &i)
		case matchFlag(args, i, "--title"):
			title = flagVal(args, &i)
		case matchFlag(args, i, "--body"):
			body = flagVal(args, &i)
		case matchFlag(args, i, "--proof-b64"):
			proofB64 = flagVal(args, &i)
		case matchFlag(args, i, "--nullifier-hex"):
			nullifierHex = flagVal(args, &i)
		default:
			fatal("unknown flag: %s", args[i])
		}
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
	var proof []byte
	if proofB64 != "" {
		var err error
		if proof, err = base64.StdEncoding.DecodeString(proofB64); err != nil {
			fatal("decode proof: %v", err)
		}
	}

	var nullifier []byte
	if nullifierHex != "" {
		var err error
		if nullifier, err = hex.DecodeString(nullifierHex); err != nil || len(nullifier) != 32 {
			fatal("--nullifier-hex must be 32 bytes of hex")
		}
	} else {
		nullifier = make([]byte, 32)
		if _, err := rand.Read(nullifier); err != nil {
			fatal("random nullifier: %v", err)
		}
	}

	innerMsg := &blogtypes.MsgCreatePost{Creator: shieldAddr, Title: title, Body: body}
	innerBytes, err := proto.Marshal(innerMsg)
	if err != nil {
		fatal("marshal inner message: %v", err)
	}
	fragment, err := tle.EncodeExecFragment("/sparkdream.blog.v1.MsgCreatePost", innerBytes, proof, nullifier)
	if err != nil {
		fatal("encode fragment: %v", err)
	}
	ct, err := tle.Encrypt(ks.masterPublicKeyG2(), uint64(epoch), fragment)
	if err != nil {
		fatal("encrypt: %v", err)
	}
	fmt.Fprintf(os.Stderr, "  Operation nullifier (sealed): %x\n", nullifier)
	fmt.Fprintf(os.Stderr, "  Submission proof: domain %d, scope %d, message type %s over the payload\n",
		shieldtypes.BatchSubmissionDomain, shieldtypes.BatchSubmissionScope(ct), shieldtypes.BatchSubmissionTypeURL)
	fmt.Print(base64.StdEncoding.EncodeToString(ct))
}

// cmdDecrypt opens a payload with an epoch decryption key (from --key-file
// or --dec-key-b64) and prints the plaintext as hex.
func cmdDecrypt(args []string) {
	var keyFile, decKeyB64, payloadB64 string
	var epoch int
	for i := 0; i < len(args); i++ {
		switch {
		case matchFlag(args, i, "--key-file"):
			keyFile = flagVal(args, &i)
		case matchFlag(args, i, "--dec-key-b64"):
			decKeyB64 = flagVal(args, &i)
		case matchFlag(args, i, "--epoch"):
			fmt.Sscanf(flagVal(args, &i), "%d", &epoch)
		case matchFlag(args, i, "--payload-b64"):
			payloadB64 = flagVal(args, &i)
		default:
			fatal("unknown flag: %s", args[i])
		}
	}
	if payloadB64 == "" {
		fatal("--payload-b64 is required")
	}
	if decKeyB64 == "" {
		if keyFile == "" {
			fatal("--key-file or --dec-key-b64 is required")
		}
		dk, ok := loadKeys(keyFile).DecryptionKeys[fmt.Sprintf("%d", epoch)]
		if !ok {
			fatal("no decryption key for epoch %d in %s", epoch, keyFile)
		}
		decKeyB64 = dk.DecryptionKeyB64
	}
	decKey, err := base64.StdEncoding.DecodeString(decKeyB64)
	if err != nil {
		fatal("decode decryption key: %v", err)
	}
	payload, err := base64.StdEncoding.DecodeString(payloadB64)
	if err != nil {
		fatal("decode payload: %v", err)
	}
	pt, err := tle.Decrypt(decKey, uint64(epoch), payload)
	if err != nil {
		fatal("decrypt: %v", err)
	}
	fmt.Print(hex.EncodeToString(pt))
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
