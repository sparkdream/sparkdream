// aminogolden exports what a client needs to sign sparkdream messages in
// amino-JSON mode (Keplr+Ledger) exactly as the chain renders them:
//
//	aminogolden schema   descriptors of every sparkdream Msg (and the cosmos
//	                     messages clients wrap them in), with the field options
//	                     cosmossdk.io/x/tx/signing/aminojson acts on
//	aminogolden golden   sample messages as protobuf bytes next to the amino
//	                     JSON the chain's aminojson encoder renders for them
//	aminogolden verify   reads amino-signed transactions (JSONL on stdin) and
//	                     verifies each signature as the ante handler does, from
//	                     sign bytes the SDK's aminojson sign mode rebuilds
//	                     out of the decoded tx body
//
// Both golden and verify render a message from its bytes as the chain's tx
// decoding re-encodes them (see gogoCanonical), which is what the ante
// handler signs from: non-nullable fields a client left out are present.
//
// sparkdreamjs builds its amino converters from the schema
// (src-overrides/chain-amino.ts) and checks them against the golden records,
// and sparkdream-ui checks its signing setup against them too:
//
//	go run ./cmd/aminogolden schema | node <sparkdreamjs>/packages/sparkdreamjs/scripts/amino-schema.js
//	go run ./cmd/aminogolden golden > <sparkdreamjs>/packages/sparkdreamjs/__tests__/amino-golden.jsonl
//	go run ./cmd/aminogolden golden > ../sparkdream-ui/src/lib/__fixtures__/amino-golden.jsonl
//
// Rerun both after any proto change. The UI's signing test can write the
// transactions it signs for verify (see AMINO_SIGNED_OUT in
// src/lib/aminoSigning.test.ts).
package main

import (
	"fmt"
	"os"

	"sparkdream/app"
	"sparkdream/tools/aminogolden"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "schema" && os.Args[1] != "golden" && os.Args[1] != "verify") {
		fmt.Fprintln(os.Stderr, "usage: aminogolden schema|golden|verify")
		os.Exit(2)
	}
	// Importing app links every module, so the proto registry is complete.
	os.Exit(aminogolden.Run(os.Args[1], os.Stdin, os.Stdout, app.AccountAddressPrefix))
}
