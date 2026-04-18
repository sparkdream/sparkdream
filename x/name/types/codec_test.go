package types_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	ntypes "sparkdream/x/name/types"
)

func TestRegisterInterfaces(t *testing.T) {
	registry := types.NewInterfaceRegistry()
	ntypes.RegisterInterfaces(registry)

	msgs := []sdk.Msg{
		&ntypes.MsgUpdateName{},
		&ntypes.MsgUpdateParams{},
		&ntypes.MsgUpdateOperationalParams{},
		&ntypes.MsgRegisterName{},
		&ntypes.MsgSetPrimary{},
		&ntypes.MsgFileDispute{},
		&ntypes.MsgContestDispute{},
		&ntypes.MsgResolveDispute{},
	}

	for _, m := range msgs {
		typeURL := sdk.MsgTypeURL(m)
		if typeURL == "" {
			t.Errorf("empty type URL for %T", m)
			continue
		}
		if _, err := registry.Resolve(typeURL); err != nil {
			t.Errorf("message %T (%s) was not registered: %v", m, typeURL, err)
		}
	}
}
