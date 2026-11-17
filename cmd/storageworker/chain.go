package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	commontypes "sparkdream/x/common/types"
	"strconv"

	"sparkdream/tools/contentscan"
)

// num is an integer the LCD writes as a JSON string or a JSON number.
type num int64

func (n *num) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(s) >= 2 && s[0] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("not an integer: %s", string(b))
	}
	*n = num(v)
	return nil
}

// request mirrors the fields of an x/storage Request the worker reads.
type request struct {
	ID                   num      `json:"id"`
	Requester            string   `json:"requester"`
	TrustLevel           num      `json:"requester_trust_level"`
	Backend              string   `json:"backend"`
	Sha256               string   `json:"sha256"`
	SizeBytes            num      `json:"size_bytes"`
	ContentType          string   `json:"content_type"`
	Scanned              bool     `json:"scanned"`
	Status               string   `json:"status"`
	OpenedAt             num      `json:"opened_at"`
	Deadline             num      `json:"deadline"`
	Operator             string   `json:"operator"`
	ClaimExpiresAt       num      `json:"claim_expires_at"`
	ClaimExtended        bool     `json:"claim_extended"`
	RequiredAttestations num      `json:"required_attestations"`
	Barred               []string `json:"barred"`
	Sources              []struct {
		URL     string `json:"url"`
		AddedAt num    `json:"added_at"`
	} `json:"sources"`
}

const (
	statusPending = "REQUEST_STATUS_PENDING"
	statusClaimed = "REQUEST_STATUS_CLAIMED"
)

// id is the request's id as the chain's messages carry it.
func (r request) id() uint64 { return commontypes.Saturate[uint64](int64(r.ID)) }

type backendType struct {
	ContentType string `json:"content_type"`
	Scanned     bool   `json:"scanned"`
	MaxSize     num    `json:"max_size"`
}

type backend struct {
	Name        string        `json:"name"`
	ServiceType string        `json:"service_type"`
	Types       []backendType `json:"types"`
}

func (b backend) maxSize(contentType string) int64 {
	for _, t := range b.Types {
		if t.ContentType == contentType {
			return int64(t.MaxSize)
		}
	}
	return 0
}

type storageParams struct {
	ClaimWindowBlocks            num `json:"claim_window_blocks"`
	MinUnreachableBlocks         num `json:"min_unreachable_blocks"`
	MaxAttestationsPerCompletion num `json:"max_attestations_per_completion"`
}

type standing struct {
	Approved bool   `json:"approved"`
	CanClaim bool   `json:"can_claim"`
	Reason   string `json:"reason"`
}

const pageSize = 100

// pageAll walks an LCD list endpoint by offset until a short page.
func pageAll(ctx context.Context, c chainReader, path string, decode func(raw json.RawMessage) (int, error)) error {
	for offset := 0; ; offset += pageSize {
		var raw json.RawMessage
		if err := c.GetJSON(ctx, fmt.Sprintf("%s?pagination.offset=%d&pagination.limit=%d", path, offset, pageSize), &raw); err != nil {
			return err
		}
		n, err := decode(raw)
		if err != nil {
			return err
		}
		if n < pageSize {
			return nil
		}
	}
}

func (w *Worker) height(ctx context.Context) (int64, error) {
	var out struct {
		Block struct {
			Header struct {
				Height num `json:"height"`
			} `json:"header"`
		} `json:"block"`
	}
	if err := w.chain.GetJSON(ctx, "/cosmos/base/tendermint/v1beta1/blocks/latest", &out); err != nil {
		return 0, err
	}
	return int64(out.Block.Header.Height), nil
}

func (w *Worker) getRequest(ctx context.Context, id int64) (request, error) {
	var out struct {
		Request request `json:"request"`
	}
	err := w.chain.GetJSON(ctx, "/sparkdream/storage/v1/request/"+strconv.FormatInt(id, 10), &out)
	return out.Request, err
}

// listRequests pages one of the request list queries.
func (w *Worker) listRequests(ctx context.Context, path string) ([]request, error) {
	var out []request
	err := pageAll(ctx, w.chain, path, func(raw json.RawMessage) (int, error) {
		var resp struct {
			Requests []request `json:"requests"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return 0, err
		}
		out = append(out, resp.Requests...)
		return len(resp.Requests), nil
	})
	return out, err
}

// flagged reports whether a hash is flagged as removed content. A query
// error other than "not flagged" counts as flagged: when the worker cannot
// tell, it does not store.
func (w *Worker) flagged(ctx context.Context, sha256Hex string) bool {
	var out struct {
		RemovedHash struct {
			Flags []json.RawMessage `json:"flags"`
		} `json:"removed_hash"`
	}
	err := w.chain.GetJSON(ctx, "/sparkdream/storage/v1/removed_hash/"+url.PathEscape(sha256Hex), &out)
	if err != nil {
		return !isNotFound(err)
	}
	return len(out.RemovedHash.Flags) > 0
}

// scanners returns the content scanners x/storage counts now: approved
// there (and not stale), ACTIVE in x/service, with a usable verdict key and
// feed address in their operator metadata.
func (w *Worker) scanners(ctx context.Context) ([]scanner, error) {
	var approved []string
	if err := pageAll(ctx, w.chain, "/sparkdream/storage/v1/approved_scanners", func(raw json.RawMessage) (int, error) {
		var resp struct {
			Scanners []struct {
				Scanner string `json:"scanner"`
				Stale   bool   `json:"stale"`
			} `json:"scanners"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return 0, err
		}
		for _, s := range resp.Scanners {
			if !s.Stale {
				approved = append(approved, s.Scanner)
			}
		}
		return len(resp.Scanners), nil
	}); err != nil {
		return nil, err
	}
	var out []scanner
	for _, addr := range approved {
		var resp struct {
			Operator struct {
				Status   string `json:"status"`
				Metadata string `json:"metadata"`
			} `json:"operator"`
		}
		if err := w.chain.GetJSON(ctx, "/sparkdream/service/v1/operator/"+url.PathEscape(addr)+"/content-scanner", &resp); err != nil {
			continue
		}
		if resp.Operator.Status != "OPERATOR_STATUS_ACTIVE" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(resp.Operator.Metadata)
		if err != nil {
			continue
		}
		meta, err := contentscan.ParseOperatorMetadata(raw)
		if err != nil {
			continue
		}
		key, err := meta.PublicKey()
		if err != nil {
			continue
		}
		out = append(out, scanner{TrustedWorker: contentscan.TrustedWorker{Address: addr, VerdictKey: key}, feedURL: meta.FeedURL})
	}
	return out, nil
}

// quorum returns the number of clean verdicts a request needs: the
// content-scanner type's quorum, or its elevated quorum for a requester at
// trust level NEW or PROVISIONAL, as the chain fixes it at the claim.
func (w *Worker) quorum(ctx context.Context, trustLevel int64) (int, error) {
	var out struct {
		Config struct {
			Quorum   num `json:"attestation_quorum"`
			Elevated num `json:"elevated_attestation_quorum"`
		} `json:"config"`
	}
	if err := w.chain.GetJSON(ctx, "/sparkdream/service/v1/service_type/content-scanner", &out); err != nil {
		return 0, err
	}
	q := int(out.Config.Quorum)
	if out.Config.Elevated > 0 && trustLevel <= 1 {
		q = int(out.Config.Elevated)
	}
	return q, nil
}
