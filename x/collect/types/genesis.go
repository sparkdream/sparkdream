package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:              DefaultParams(),
		Collections:         []Collection{},
		CollectionCount:     1,
		Items:               []Item{},
		ItemCount:           1,
		Collaborators:       []Collaborator{},
		Curators:            []Curator{},
		CurationReviews:     []CurationReview{},
		CurationReviewCount: 1,
		CurationSummaries:   []CurationSummary{},
		SponsorshipRequests: []SponsorshipRequest{},
		Flags:               []CollectionFlag{},
		HideRecords:         []HideRecord{},
		HideRecordCount:     1,
		Endorsements:        []Endorsement{},
	}
}

// Validate performs basic genesis state validation returning an error upon any failure.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	// Validate collection IDs are unique
	collectionIDs := make(map[uint64]bool)
	for _, c := range gs.Collections {
		if collectionIDs[c.Id] {
			return fmt.Errorf("duplicate collection ID: %d", c.Id)
		}
		collectionIDs[c.Id] = true
		if c.Id >= gs.CollectionCount {
			return fmt.Errorf("collection ID %d >= collection_count %d", c.Id, gs.CollectionCount)
		}
	}

	// Validate item IDs are unique
	itemIDs := make(map[uint64]bool)
	for _, item := range gs.Items {
		if itemIDs[item.Id] {
			return fmt.Errorf("duplicate item ID: %d", item.Id)
		}
		itemIDs[item.Id] = true
		if item.Id >= gs.ItemCount {
			return fmt.Errorf("item ID %d >= item_count %d", item.Id, gs.ItemCount)
		}
		if !collectionIDs[item.CollectionId] {
			return fmt.Errorf("item %d references non-existent collection %d", item.Id, item.CollectionId)
		}
	}

	// Validate collaborators reference valid collections
	for _, collab := range gs.Collaborators {
		if !collectionIDs[collab.CollectionId] {
			return fmt.Errorf("collaborator %s references non-existent collection %d", collab.Address, collab.CollectionId)
		}
	}

	// Validate curation review IDs are unique
	reviewIDs := make(map[uint64]bool)
	for _, r := range gs.CurationReviews {
		if reviewIDs[r.Id] {
			return fmt.Errorf("duplicate curation review ID: %d", r.Id)
		}
		reviewIDs[r.Id] = true
		if r.Id >= gs.CurationReviewCount {
			return fmt.Errorf("curation review ID %d >= curation_review_count %d", r.Id, gs.CurationReviewCount)
		}
	}

	return nil
}
