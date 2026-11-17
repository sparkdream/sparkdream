package types

import (
	"fmt"
)

// DefaultGenesis returns the default genesis state: default params and the
// arweave backend. No operator or scanner is approved at genesis.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:   DefaultParams(),
		Backends: []Backend{DefaultArweaveBackend()},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure. It sees only this module's state; the checks against x/service and
// the bank are made in InitGenesis.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	p := gs.Params
	backends := make(map[string]Backend, len(gs.Backends))
	for _, b := range gs.Backends {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("backend %q: %w", b.Name, err)
		}
		if _, dup := backends[b.Name]; dup {
			return fmt.Errorf("backend %q listed twice", b.Name)
		}
		if b.DeclinesToClose > p.MaxBarredPerRequest {
			return fmt.Errorf("backend %q: declines_to_close is above max_barred_per_request", b.Name)
		}
		backends[b.Name] = b
	}

	perBackend := map[string]uint32{}
	seenOp := map[string]bool{}
	for _, a := range gs.ApprovedOperators {
		if _, ok := backends[a.Backend]; !ok {
			return fmt.Errorf("approved operator %s: unknown backend %q", a.Operator, a.Backend)
		}
		if a.Operator == "" || a.Controller == "" {
			return fmt.Errorf("approved operator needs an operator and a controller")
		}
		key := a.Backend + "/" + a.Operator
		if seenOp[key] {
			return fmt.Errorf("operator %s approved twice for %q", a.Operator, a.Backend)
		}
		seenOp[key] = true
		perBackend[a.Backend]++
		if perBackend[a.Backend] > p.MaxOperatorsPerBackend {
			return fmt.Errorf("backend %q has more than max_operators_per_backend approvals", a.Backend)
		}
	}
	if uint64(len(gs.ApprovedScanners)) > uint64(p.MaxApprovedScanners) {
		return fmt.Errorf("more than max_approved_scanners scanners approved")
	}
	seenScanner := map[string]bool{}
	for _, a := range gs.ApprovedScanners {
		if a.Scanner == "" || a.Controller == "" {
			return fmt.Errorf("approved scanner needs a scanner and a controller")
		}
		if seenScanner[a.Scanner] {
			return fmt.Errorf("scanner %s approved twice", a.Scanner)
		}
		seenScanner[a.Scanner] = true
	}

	flagged := make(map[string]RemovedHash, len(gs.RemovedHashes))
	for _, rec := range gs.RemovedHashes {
		if err := ValidateHash(rec.Sha256); err != nil {
			return fmt.Errorf("removed hash %q: %w", rec.Sha256, err)
		}
		if _, dup := flagged[rec.Sha256]; dup {
			return fmt.Errorf("removed hash %s listed twice", rec.Sha256)
		}
		if len(rec.Flags) == 0 || uint64(len(rec.Flags)) > uint64(p.MaxFlagsPerHash) {
			return fmt.Errorf("removed hash %s must have 1 to %d flags", rec.Sha256, p.MaxFlagsPerHash)
		}
		scanners := map[string]bool{}
		for _, f := range rec.Flags {
			if scanners[f.Scanner] {
				return fmt.Errorf("removed hash %s flagged twice by %s", rec.Sha256, f.Scanner)
			}
			scanners[f.Scanner] = true
		}
		flagged[rec.Sha256] = rec
	}

	requests := make(map[uint64]Request, len(gs.Requests))
	live := map[string]uint64{}
	for _, r := range gs.Requests {
		if r.Id == 0 || r.Id > gs.RequestSeq {
			return fmt.Errorf("request id %d is outside (0, request_seq]", r.Id)
		}
		if _, dup := requests[r.Id]; dup {
			return fmt.Errorf("request %d listed twice", r.Id)
		}
		if _, ok := backends[r.Backend]; !ok {
			return fmt.Errorf("request %d: unknown backend %q", r.Id, r.Backend)
		}
		if err := ValidateHash(r.Sha256); err != nil {
			return fmt.Errorf("request %d: %w", r.Id, err)
		}
		if r.Requester == "" {
			return fmt.Errorf("request %d has no requester", r.Id)
		}
		if r.Status == RequestStatus_REQUEST_STATUS_UNSPECIFIED {
			return fmt.Errorf("request %d has no status", r.Id)
		}
		if r.CopyFee.IsNil() || r.CopyFee.IsNegative() || r.PaidAmount.IsNil() || r.PaidAmount.IsNegative() {
			return fmt.Errorf("request %d has an invalid amount", r.Id)
		}
		holds := r.Status.IsOpen() || (r.Status == RequestStatus_REQUEST_STATUS_REJECTED && r.ForfeitAt != 0)
		if !holds && !r.CopyFee.IsZero() {
			return fmt.Errorf("request %d (%s) must hold no copy fee", r.Id, r.Status)
		}
		if (r.Status == RequestStatus_REQUEST_STATUS_CLAIMED || r.Status == RequestStatus_REQUEST_STATUS_COMPLETED) && r.Operator == "" {
			return fmt.Errorf("request %d (%s) has no operator", r.Id, r.Status)
		}
		if r.ForfeitAt != 0 && (r.Status != RequestStatus_REQUEST_STATUS_REJECTED || r.ClosedReason != ClosedReason_CLOSED_REASON_FLAGGED) {
			return fmt.Errorf("request %d: only a flagged request holds a fee", r.Id)
		}
		if uint64(len(r.Sources)) > uint64(p.MaxSourcesPerRequest) || len(r.Sources) == 0 ||
			uint64(len(r.Barred)) > uint64(p.MaxBarredPerRequest) ||
			uint64(len(r.DeclinedBy)) > uint64(p.MaxBarredPerRequest) ||
			uint64(len(r.Attestations)) > uint64(p.MaxAttestationsPerCompletion) {
			return fmt.Errorf("request %d: a list is outside its cap", r.Id)
		}
		if r.Status.IsLive() {
			key := r.Backend + "/" + r.Sha256
			if other, dup := live[key]; dup {
				return fmt.Errorf("requests %d and %d are both live for one file", other, r.Id)
			}
			live[key] = r.Id
		}
		if r.Status.IsOpen() {
			if _, bad := flagged[r.Sha256]; bad {
				return fmt.Errorf("open request %d has a flagged hash", r.Id)
			}
		}
		requests[r.Id] = r
	}

	held := map[uint64]bool{}
	for _, rec := range gs.RemovedHashes {
		for _, id := range rec.Held {
			r, ok := requests[id]
			if !ok || r.Sha256 != rec.Sha256 || r.ForfeitAt == 0 {
				return fmt.Errorf("removed hash %s holds request %d, which is not a flagged request with a held fee", rec.Sha256, id)
			}
			if held[id] {
				return fmt.Errorf("request %d is held twice", id)
			}
			held[id] = true
		}
	}
	for _, r := range gs.Requests {
		if r.ForfeitAt != 0 && !held[r.Id] {
			return fmt.Errorf("request %d holds a flagged fee but is on no held list", r.Id)
		}
	}

	seenCounter := map[string]bool{}
	for _, c := range gs.Counters {
		if c.Address == "" || c.Kind == CounterKind_COUNTER_KIND_UNSPECIFIED {
			return fmt.Errorf("counter needs an address and a kind")
		}
		key := fmt.Sprintf("%s/%d", c.Address, c.Kind)
		if seenCounter[key] {
			return fmt.Errorf("counter %s listed twice", key)
		}
		seenCounter[key] = true
	}
	return nil
}
