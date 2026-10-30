package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Limits on achievement and title definitions.
const (
	MaxDefinitionNameLength        = 64
	MaxDefinitionDescriptionLength = 512
)

// validateDefinition checks the fields achievements and titles share: a
// bounded name and description, and a rarity and requirement type that are
// defined enum values (UNSPECIFIED included: a definition granted by other
// means has no requirement). An undefined requirement type would be indexed
// and could never be earned.
func validateDefinition(errType *errorsmod.Error, name, description string, rarity Rarity, req RequirementType) error {
	if name == "" || len(name) > MaxDefinitionNameLength {
		return errorsmod.Wrapf(errType, "name must be 1-%d bytes", MaxDefinitionNameLength)
	}
	if len(description) > MaxDefinitionDescriptionLength {
		return errorsmod.Wrapf(errType, "description must be at most %d bytes", MaxDefinitionDescriptionLength)
	}
	if _, ok := Rarity_name[int32(rarity)]; !ok {
		return errorsmod.Wrapf(errType, "undefined rarity %d", rarity)
	}
	if _, ok := RequirementType_name[int32(req)]; !ok {
		return errorsmod.Wrapf(errType, "undefined requirement type %d", req)
	}
	return nil
}

// ValidateDefinition checks an achievement definition (create and update).
func (a Achievement) ValidateDefinition() error {
	return validateDefinition(ErrInvalidAchievementId, a.Name, a.Description, a.Rarity, a.RequirementType)
}

// ValidateDefinition checks a title definition (create and update).
func (t Title) ValidateDefinition() error {
	return validateDefinition(ErrInvalidTitleId, t.Name, t.Description, t.Rarity, t.RequirementType)
}
