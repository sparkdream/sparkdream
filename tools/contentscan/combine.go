package contentscan

import (
	"crypto/ed25519"
	"sort"
	"time"
)

// TrustedWorker is an ACTIVE content-scanner operator and the verdict key it
// published in its operator metadata (§6 step 1). Trust follows operator
// status: a worker that is slashed, unbonding or retired is simply absent.
type TrustedWorker struct {
	Address    string
	VerdictKey ed25519.PublicKey
}

// Result is the combined verdict for one subject.
type Result struct {
	Verdict  string // removed / held / clean / unchecked
	Category string // category of the deciding verdict ("" when unchecked/clean)
	// Conflict is set when trusted workers disagree (removed or held against
	// clean); removed/held wins and the conflict goes to council review (§4.4).
	Conflict bool
	// CleanVotes counts distinct trusted workers that said clean for the
	// requested hash.
	CleanVotes int
}

// Combine applies the §4.4 rule to the attestations about one subject:
//
//  1. removed if any trusted worker says removed;
//  2. otherwise held if any trusted worker says held;
//  3. otherwise clean if at least quorum distinct trusted workers say clean
//     for the same hash (sha256);
//  4. otherwise unchecked.
//
// Attestations that fail signature verification, come from untrusted
// workers, concern another hash, or have expired at now are ignored. A
// worker's latest verdict (by issued_at) is the one that counts. quorum < 1
// is treated as 1.
func Combine(atts []Attestation, workers []TrustedWorker, sha256Hex string, quorum int, now time.Time) Result {
	if quorum < 1 {
		quorum = 1
	}
	keys := make(map[string]ed25519.PublicKey, len(workers))
	for _, w := range workers {
		keys[w.Address] = w.VerdictKey
	}

	latest := map[string]Attestation{}
	for _, a := range atts {
		key, trusted := keys[a.Worker]
		if !trusted || a.Subject.SHA256 != sha256Hex {
			continue
		}
		if !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt) {
			continue
		}
		if Verify(a, key) != nil {
			continue
		}
		if prev, ok := latest[a.Worker]; ok && !a.IssuedAt.After(prev.IssuedAt) {
			continue
		}
		latest[a.Worker] = a
	}

	var res Result
	var removed, held *Attestation
	workersSorted := make([]string, 0, len(latest))
	for w := range latest {
		workersSorted = append(workersSorted, w)
	}
	sort.Strings(workersSorted) // deterministic category choice
	for _, w := range workersSorted {
		a := latest[w]
		switch a.Verdict {
		case VerdictRemoved:
			if removed == nil {
				removed = &a
			}
		case VerdictHeld:
			if held == nil {
				held = &a
			}
		case VerdictClean:
			res.CleanVotes++
		}
	}
	switch {
	case removed != nil:
		res.Verdict, res.Category = VerdictRemoved, removed.Category
		res.Conflict = res.CleanVotes > 0
	case held != nil:
		res.Verdict, res.Category = VerdictHeld, held.Category
		res.Conflict = res.CleanVotes > 0
	case res.CleanVotes >= quorum:
		res.Verdict = VerdictClean
	default:
		res.Verdict = VerdictUnchecked
	}
	return res
}

// EffectiveScannedHeight is the lowest scanned height among the quorum most
// advanced trusted workers (§4.4): flagged content above it is unchecked.
// heights holds one entry per trusted worker (its manifest or on-chain
// checkpoint height). Returns 0 when fewer than quorum workers report.
func EffectiveScannedHeight(heights []int64, quorum int) int64 {
	if quorum < 1 {
		quorum = 1
	}
	if len(heights) < quorum {
		return 0
	}
	sorted := append([]int64(nil), heights...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] > sorted[j] })
	return sorted[quorum-1]
}

// QuorumFor returns the quorum that applies to a subject: the service type's
// elevated_attestation_quorum for media from low-trust authors (§9), else
// attestation_quorum.
func QuorumFor(attestationQuorum, elevatedQuorum uint32, lowTrustAuthor bool) int {
	if lowTrustAuthor && elevatedQuorum > attestationQuorum {
		return int(elevatedQuorum)
	}
	return int(attestationQuorum)
}
