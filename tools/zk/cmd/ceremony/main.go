// Command ceremony runs the Groth16 multi-party trusted setup for the shield
// circuit, using gnark's mpcsetup (Phase 1: powers of tau, Phase 2: circuit
// specific). It replaces the single-party tools/zk/cmd/setup for any key that
// goes on chain.
//
// The setup is sound as long as ONE contributor generated their randomness
// honestly and discarded it. Contributions happen in memory; nothing secret
// is ever written to disk. Every step reads and writes a transcript directory
// that can be handed from one contributor to the next and published at the end.
//
// Usage (each contributor runs the contribute step on their own machine, in
// turn, on the directory the previous contributor handed them):
//
//	go run ./tools/zk/cmd/ceremony contribute-phase1 -dir ceremony
//	go run ./tools/zk/cmd/ceremony seal-phase1       -dir ceremony -beacon <hex>
//	go run ./tools/zk/cmd/ceremony contribute-phase2 -dir ceremony
//	go run ./tools/zk/cmd/ceremony seal-phase2       -dir ceremony -beacon <hex>
//	go run ./tools/zk/cmd/ceremony verify            -dir ceremony [-genesis genesis.json]
//
// Each beacon should be public randomness fixed in advance and only known after
// the last contribution of its phase, e.g. a future drand round's "randomness"
// (curl https://api.drand.sh/public/<round>).
//
// Outputs of seal-phase2, in the same formats tools/zk/cmd/setup writes:
// proving_key.bin, verifying_key.bin, verifying_key.hex (for genesis
// shield.verification_keys, circuit_id "shield_v1") and circuit.r1cs, plus
// proving_key.raw.bin (uncompressed, what web clients load).
//
// More contributors can be added later by contributing on top of the unsealed
// transcript and sealing again with a new beacon; the result is a new VK.
// If the circuit changes, Phase 1 is reused as long as the circuit still fits
// its domain, and only Phase 2 is redone.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/groth16/bn254/mpcsetup"
	cs "github.com/consensys/gnark/constraint/bn254"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	"sparkdream/tools/crypto"
	"sparkdream/tools/zk/circuit"
	"sparkdream/tools/zk/prover"
)

const (
	phase1Prefix    = "phase1_"
	phase2Prefix    = "phase2_"
	commonsFile     = "srs_commons.bin"
	beacon1File     = "beacon_phase1.txt"
	beacon2File     = "beacon_phase2.txt"
	pkFile          = "proving_key.bin"
	pkRawFile       = "proving_key.raw.bin"
	vkFile          = "verifying_key.bin"
	vkHexFile       = "verifying_key.hex"
	r1csFile        = "circuit.r1cs"
	shieldCircuitID = "shield_v1"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dir := fs.String("dir", "ceremony", "transcript directory")
	beacon := fs.String("beacon", "", "public beacon, hex (seal steps only)")
	genesis := fs.String("genesis", "", "genesis.json whose shield VK must match (verify only)")
	_ = fs.Parse(args)

	var err error
	switch cmd {
	case "contribute-phase1":
		err = contributePhase1(*dir)
	case "seal-phase1":
		err = sealPhase1(*dir, *beacon)
	case "contribute-phase2":
		err = contributePhase2(*dir)
	case "seal-phase2":
		err = sealPhase2(*dir, *beacon)
	case "verify":
		err = verify(*dir, *genesis)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ceremony <contribute-phase1|seal-phase1|contribute-phase2|seal-phase2|verify> -dir <transcript> [-beacon <hex>] [-genesis <file>]")
	os.Exit(2)
}

// ---------------------------------------------------------------------------
// Phase 1 (powers of tau, circuit independent up to its domain size)
// ---------------------------------------------------------------------------

func contributePhase1(dir string) error {
	if exists(filepath.Join(dir, commonsFile)) {
		return errors.New("phase 1 is already sealed; contribute to phase 2 instead")
	}
	ccs, err := compileCircuit()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	contribs, err := listContributions(dir, phase1Prefix)
	if err != nil {
		return err
	}
	var p mpcsetup.Phase1
	if len(contribs) == 0 {
		p.Initialize(domainSize(ccs))
	} else if err := readFrom(filepath.Join(dir, contribs[len(contribs)-1]), &p); err != nil {
		return err
	}

	start := time.Now()
	p.Contribute()
	out := contributionName(phase1Prefix, len(contribs)+1)
	digest, err := writeTo(filepath.Join(dir, out), &p)
	if err != nil {
		return err
	}
	fmt.Printf("Phase 1 contribution #%d written to %s in %v\n", len(contribs)+1, out, time.Since(start).Round(time.Millisecond))
	fmt.Printf("sha256: %s  (publish this so others can check your contribution is in the transcript)\n", digest)
	return nil
}

func sealPhase1(dir, beaconHex string) error {
	beacon, err := decodeBeacon(beaconHex)
	if err != nil {
		return err
	}
	ccs, err := compileCircuit()
	if err != nil {
		return err
	}
	commons, err := verifyPhase1(dir, ccs, beacon)
	if err != nil {
		return err
	}
	if _, err := writeTo(filepath.Join(dir, commonsFile), &commons); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, beacon1File), []byte(beaconHex+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("Phase 1 verified and sealed; wrote %s\n", commonsFile)
	return nil
}

func verifyPhase1(dir string, ccs *cs.R1CS, beacon []byte) (mpcsetup.SrsCommons, error) {
	contribs, err := listContributions(dir, phase1Prefix)
	if err != nil {
		return mpcsetup.SrsCommons{}, err
	}
	if len(contribs) == 0 {
		return mpcsetup.SrsCommons{}, errors.New("no phase 1 contributions")
	}
	ps := make([]*mpcsetup.Phase1, len(contribs))
	for i, name := range contribs {
		ps[i] = new(mpcsetup.Phase1)
		if err := readFrom(filepath.Join(dir, name), ps[i]); err != nil {
			return mpcsetup.SrsCommons{}, err
		}
	}
	commons, err := mpcsetup.VerifyPhase1(domainSize(ccs), beacon, ps...)
	if err != nil {
		return mpcsetup.SrsCommons{}, fmt.Errorf("phase 1 transcript invalid: %w", err)
	}
	fmt.Printf("Phase 1: %d contribution(s) verified\n", len(contribs))
	return commons, nil
}

// ---------------------------------------------------------------------------
// Phase 2 (specific to the shield circuit)
// ---------------------------------------------------------------------------

func contributePhase2(dir string) error {
	if exists(filepath.Join(dir, vkFile)) {
		return errors.New("phase 2 is already sealed; remove the sealed outputs to add contributions and reseal")
	}
	ccs, err := compileCircuit()
	if err != nil {
		return err
	}
	contribs, err := listContributions(dir, phase2Prefix)
	if err != nil {
		return err
	}

	var p mpcsetup.Phase2
	if len(contribs) == 0 {
		var commons mpcsetup.SrsCommons
		if err := readFrom(filepath.Join(dir, commonsFile), &commons); err != nil {
			return fmt.Errorf("phase 1 must be sealed first: %w", err)
		}
		p.Initialize(ccs, &commons)
	} else if err := readFrom(filepath.Join(dir, contribs[len(contribs)-1]), &p); err != nil {
		return err
	}

	start := time.Now()
	p.Contribute()
	out := contributionName(phase2Prefix, len(contribs)+1)
	digest, err := writeTo(filepath.Join(dir, out), &p)
	if err != nil {
		return err
	}
	fmt.Printf("Phase 2 contribution #%d written to %s in %v\n", len(contribs)+1, out, time.Since(start).Round(time.Millisecond))
	fmt.Printf("sha256: %s  (publish this so others can check your contribution is in the transcript)\n", digest)
	return nil
}

func sealPhase2(dir, beaconHex string) error {
	beacon, err := decodeBeacon(beaconHex)
	if err != nil {
		return err
	}
	ccs, err := compileCircuit()
	if err != nil {
		return err
	}
	commons, err := sealedPhase1(dir, ccs)
	if err != nil {
		return err
	}
	pk, vk, err := verifyPhase2(dir, ccs, &commons, beacon)
	if err != nil {
		return err
	}

	if err := smokeTest(ccs, pk, vk); err != nil {
		return fmt.Errorf("sealed keys failed a test proof: %w", err)
	}

	if _, err := writeTo(filepath.Join(dir, pkFile), pk); err != nil {
		return err
	}
	if _, err := writeTo(filepath.Join(dir, pkRawFile), rawWriter{pk}); err != nil {
		return err
	}
	if _, err := writeTo(filepath.Join(dir, vkFile), vk); err != nil {
		return err
	}
	if _, err := writeTo(filepath.Join(dir, r1csFile), ccs); err != nil {
		return err
	}
	vkBytes, err := serialize(vk)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, vkHexFile), []byte(hex.EncodeToString(vkBytes)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, beacon2File), []byte(beaconHex+"\n"), 0o644); err != nil {
		return err
	}

	fmt.Printf("Phase 2 verified and sealed; a test proof verified against the new keys.\n")
	fmt.Printf("Wrote %s, %s, %s, %s, %s\n", pkFile, pkRawFile, vkFile, vkHexFile, r1csFile)
	fmt.Printf("Genesis: add {\"circuit_id\": %q, \"vk_bytes\": %q} to shield.verification_keys\n",
		shieldCircuitID, base64.StdEncoding.EncodeToString(vkBytes))
	return nil
}

// sealedPhase1 recomputes the Phase 1 output from the phase1_* contributions
// and the recorded beacon, and requires srs_commons.bin to match it, so the
// keys sealed in Phase 2 always rest on a Phase 1 transcript that verifies.
func sealedPhase1(dir string, ccs *cs.R1CS) (mpcsetup.SrsCommons, error) {
	path := filepath.Join(dir, commonsFile)
	if !exists(path) {
		return mpcsetup.SrsCommons{}, errors.New("phase 1 must be sealed first")
	}
	beaconHex, err := readBeaconFile(filepath.Join(dir, beacon1File))
	if err != nil {
		return mpcsetup.SrsCommons{}, err
	}
	beacon, err := decodeBeacon(beaconHex)
	if err != nil {
		return mpcsetup.SrsCommons{}, err
	}
	commons, err := verifyPhase1(dir, ccs, beacon)
	if err != nil {
		return mpcsetup.SrsCommons{}, err
	}
	want, err := serialize(&commons)
	if err != nil {
		return mpcsetup.SrsCommons{}, err
	}
	have, err := os.ReadFile(path)
	if err != nil {
		return mpcsetup.SrsCommons{}, err
	}
	if !bytes.Equal(want, have) {
		return mpcsetup.SrsCommons{}, fmt.Errorf("%s does not match the phase 1 transcript", commonsFile)
	}
	return commons, nil
}

func verifyPhase2(dir string, ccs *cs.R1CS, commons *mpcsetup.SrsCommons, beacon []byte) (groth16.ProvingKey, groth16.VerifyingKey, error) {
	contribs, err := listContributions(dir, phase2Prefix)
	if err != nil {
		return nil, nil, err
	}
	if len(contribs) == 0 {
		return nil, nil, errors.New("no phase 2 contributions")
	}
	ps := make([]*mpcsetup.Phase2, len(contribs))
	for i, name := range contribs {
		ps[i] = new(mpcsetup.Phase2)
		if err := readFrom(filepath.Join(dir, name), ps[i]); err != nil {
			return nil, nil, err
		}
	}
	pk, vk, err := mpcsetup.VerifyPhase2(ccs, commons, beacon, ps...)
	if err != nil {
		return nil, nil, fmt.Errorf("phase 2 transcript invalid: %w", err)
	}
	fmt.Printf("Phase 2: %d contribution(s) verified\n", len(contribs))
	return pk, vk, nil
}

// ---------------------------------------------------------------------------
// Verify: recompute everything from the transcript and the recorded beacons
// ---------------------------------------------------------------------------

func verify(dir, genesisPath string) error {
	beacon1Hex, err := readBeaconFile(filepath.Join(dir, beacon1File))
	if err != nil {
		return err
	}
	beacon2Hex, err := readBeaconFile(filepath.Join(dir, beacon2File))
	if err != nil {
		return err
	}
	beacon1, err := decodeBeacon(beacon1Hex)
	if err != nil {
		return err
	}
	beacon2, err := decodeBeacon(beacon2Hex)
	if err != nil {
		return err
	}

	ccs, err := compileCircuit()
	if err != nil {
		return err
	}
	commons, err := verifyPhase1(dir, ccs, beacon1)
	if err != nil {
		return err
	}
	_, vk, err := verifyPhase2(dir, ccs, &commons, beacon2)
	if err != nil {
		return err
	}
	vkBytes, err := serialize(vk)
	if err != nil {
		return err
	}

	published, err := os.ReadFile(filepath.Join(dir, vkFile))
	if err != nil {
		return err
	}
	if !bytes.Equal(vkBytes, published) {
		return fmt.Errorf("%s does not match the key the transcript produces", vkFile)
	}
	fmt.Printf("%s matches the transcript\n", vkFile)

	if genesisPath != "" {
		onChain, err := genesisVK(genesisPath)
		if err != nil {
			return err
		}
		if !bytes.Equal(vkBytes, onChain) {
			return fmt.Errorf("genesis %s VK does not match the transcript", shieldCircuitID)
		}
		fmt.Printf("genesis %s VK matches the transcript\n", shieldCircuitID)
	}
	return nil
}

func genesisVK(path string) ([]byte, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		AppState struct {
			Shield struct {
				VerificationKeys []struct {
					CircuitID string `json:"circuit_id"`
					VkBytes   []byte `json:"vk_bytes"`
				} `json:"verification_keys"`
			} `json:"shield"`
		} `json:"app_state"`
	}
	if err := json.Unmarshal(bz, &doc); err != nil {
		return nil, err
	}
	for _, vk := range doc.AppState.Shield.VerificationKeys {
		if vk.CircuitID == shieldCircuitID {
			return vk.VkBytes, nil
		}
	}
	return nil, fmt.Errorf("no %s verification key in %s", shieldCircuitID, path)
}

// ---------------------------------------------------------------------------
// Smoke test: prove and verify one membership with the sealed keys
// ---------------------------------------------------------------------------

func smokeTest(ccs *cs.R1CS, pk groth16.ProvingKey, vk groth16.VerifyingKey) error {
	pkBytes, err := serialize(pk)
	if err != nil {
		return err
	}
	ccsBytes, err := serialize(ccs)
	if err != nil {
		return err
	}
	vkBytes, err := serialize(vk)
	if err != nil {
		return err
	}

	sk, err := prover.GenerateSecretKey()
	if err != nil {
		return err
	}
	const trustLevel = 2
	tree := crypto.NewMerkleTree(circuit.TreeDepth)
	if err := tree.AddLeaf(crypto.ComputeLeaf(prover.GetPublicKey(sk), trustLevel)); err != nil {
		return err
	}
	if err := tree.Build(); err != nil {
		return err
	}
	path, err := tree.GetProof(0)
	if err != nil {
		return err
	}

	p, err := prover.NewShieldProverFromBytes(pkBytes, ccsBytes)
	if err != nil {
		return err
	}
	out, err := p.GenerateProof(&prover.ShieldProofInput{
		SecretKey:      sk,
		TrustLevel:     trustLevel,
		MinTrustLevel:  1,
		Scope:          7,
		RateLimitEpoch: 7,
		MessageHash:    crypto.ShieldMessageHash("/sparkdream.test.v1.MsgCeremony", []byte("smoke")),
		MerkleRoot:     tree.Root(),
		MerkleProof:    path,
	})
	if err != nil {
		return err
	}
	v, err := prover.NewShieldVerifierFromBytes(vkBytes)
	if err != nil {
		return err
	}
	return v.Verify(out)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func compileCircuit() (*cs.R1CS, error) {
	var c circuit.ShieldCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &c)
	if err != nil {
		return nil, fmt.Errorf("compile shield circuit: %w", err)
	}
	return ccs.(*cs.R1CS), nil
}

func domainSize(ccs *cs.R1CS) uint64 {
	return ecc.NextPowerOfTwo(uint64(ccs.GetNbConstraints()))
}

func contributionName(prefix string, n int) string {
	return fmt.Sprintf("%s%04d.bin", prefix, n)
}

// listContributions returns the transcript's contribution files for a phase,
// in order. Zero-padded names sort numerically.
func listContributions(dir, prefix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".bin") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for i, name := range names {
		if name != contributionName(prefix, i+1) {
			return nil, fmt.Errorf("transcript gap: expected %s, found %s", contributionName(prefix, i+1), name)
		}
	}
	return names, nil
}

func decodeBeacon(beaconHex string) ([]byte, error) {
	if beaconHex == "" {
		return nil, errors.New("-beacon is required: public randomness fixed in advance, as hex")
	}
	b, err := hex.DecodeString(strings.TrimSpace(beaconHex))
	if err != nil {
		return nil, fmt.Errorf("beacon must be hex: %w", err)
	}
	if len(b) < 16 {
		return nil, errors.New("beacon must carry at least 16 bytes of public randomness")
	}
	return b, nil
}

func readBeaconFile(path string) (string, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("transcript is not sealed (%s missing): %w", filepath.Base(path), err)
	}
	return strings.TrimSpace(string(bz)), nil
}

// rawWriter writes a proving key uncompressed. Browsers load it with
// UnsafeReadFrom, skipping point decompression and checks; serve it with a
// pinned hash.
type rawWriter struct{ pk groth16.ProvingKey }

func (r rawWriter) WriteTo(w io.Writer) (int64, error) { return r.pk.WriteRawTo(w) }

func serialize(v io.WriterTo) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := v.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeTo writes v to path and returns the sha256 of what was written.
func writeTo(path string, v io.WriterTo) (string, error) {
	bz, err := serialize(v)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, bz, 0o644); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bz)
	return hex.EncodeToString(sum[:]), nil
}

func readFrom(path string, v io.ReaderFrom) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := v.ReadFrom(f); err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
