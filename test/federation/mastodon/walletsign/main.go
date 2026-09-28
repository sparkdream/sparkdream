// Command walletsign stands in for Keplr in wallet_login_e2e.sh: from a
// test seed it derives a secp256k1 key and prints its address, or an
// ADR-036 signArbitrary result over a message, as the login page would send
// it. Test keys only: the seed is hashed into the private key.
//
//	walletsign address <seed> <prefix>
//	walletsign hex <seed> <prefix>      (the raw address: sdaplogin's sub)
//	walletsign sign <seed> <prefix> <message>
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/types/bech32"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: walletsign address <seed> <prefix> | sign <seed> <prefix> <message>")
		os.Exit(2)
	}
	sum := sha256.Sum256([]byte(os.Args[2]))
	priv := &secp256k1.PrivKey{Key: sum[:]}
	addr, err := bech32.ConvertAndEncode(os.Args[3], priv.PubKey().Address())
	if err != nil {
		fail(err)
	}
	switch os.Args[1] {
	case "address":
		fmt.Println(addr)
	case "hex":
		fmt.Println(hex.EncodeToString(priv.PubKey().Address()))
	case "sign":
		if len(os.Args) < 5 {
			fail(fmt.Errorf("sign needs a message"))
		}
		// the ADR-036 sign doc, in amino JSON's sorted key order
		doc, err := json.Marshal(map[string]any{
			"account_number": "0",
			"chain_id":       "",
			"fee":            map[string]any{"amount": []any{}, "gas": "0"},
			"memo":           "",
			"msgs": []any{map[string]any{
				"type":  "sign/MsgSignData",
				"value": map[string]string{"data": base64.StdEncoding.EncodeToString([]byte(os.Args[4])), "signer": addr},
			}},
			"sequence": "0",
		})
		if err != nil {
			fail(err)
		}
		sig, err := priv.Sign(doc)
		if err != nil {
			fail(err)
		}
		out, _ := json.Marshal(map[string]any{
			"address": addr,
			"signature": map[string]any{
				"pub_key":   map[string]string{"type": "tendermint/PubKeySecp256k1", "value": base64.StdEncoding.EncodeToString(priv.PubKey().Bytes())},
				"signature": base64.StdEncoding.EncodeToString(sig),
			},
		})
		fmt.Println(string(out))
	default:
		fail(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "walletsign:", err)
	os.Exit(1)
}
