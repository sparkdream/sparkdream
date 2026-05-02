package keeper_test

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestSetDisplayName_Success(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("alice_test_account_1")).String()

	_, err := ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{
		Authority:   addr,
		DisplayName: "Alice the Great",
	})
	require.NoError(t, err)

	info, err := f.keeper.Owners.Get(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, "Alice the Great", info.DisplayName)
	require.Equal(t, addr, info.Address)
	require.Equal(t, f.ctx.BlockTime().Unix(), info.LastActiveTime)
}

func TestSetDisplayName_PreservesPrimaryName(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("alice_test_account_1")).String()

	require.NoError(t, f.keeper.Owners.Set(f.ctx, addr, types.OwnerInfo{
		Address:     addr,
		PrimaryName: "alice",
	}))

	_, err := ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{
		Authority:   addr,
		DisplayName: "Alice",
	})
	require.NoError(t, err)

	info, err := f.keeper.Owners.Get(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, "alice", info.PrimaryName, "PrimaryName should be preserved")
	require.Equal(t, "Alice", info.DisplayName)
}

func TestSetDisplayName_EmptyClears(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("alice_test_account_1")).String()

	require.NoError(t, f.keeper.Owners.Set(f.ctx, addr, types.OwnerInfo{
		Address:     addr,
		DisplayName: "Old Name",
	}))

	_, err := ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{
		Authority:   addr,
		DisplayName: "",
	})
	require.NoError(t, err)

	info, err := f.keeper.Owners.Get(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, "", info.DisplayName)
}

func TestSetDisplayName_NoHandleRequired(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("zenith_no_handle____")).String()

	// No prior OwnerInfo, no registered handle.
	_, err := ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{
		Authority:   addr,
		DisplayName: "Zenith",
	})
	require.NoError(t, err)

	info, err := f.keeper.Owners.Get(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, "Zenith", info.DisplayName)
	require.Equal(t, addr, info.Address)
}

func TestSetDisplayName_CollisionsAllowed(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	a := sdk.AccAddress([]byte("alice_test_account_1")).String()
	b := sdk.AccAddress([]byte("bob_test_account____")).String()

	for _, addr := range []string{a, b} {
		_, err := ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{Authority: addr, DisplayName: "Same Name"})
		require.NoError(t, err)
	}
}

func TestSetDisplayName_InvalidAuthority(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{
		Authority:   "not-a-bech32",
		DisplayName: "Whatever",
	})
	require.ErrorIs(t, err, sdkerrors.ErrInvalidAddress)
}

func TestSetDisplayName_ValidationRules(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	addr := sdk.AccAddress([]byte("alice_test_account_1")).String()

	// Build a 33-codepoint string (invalid: > 32).
	tooLong := strings.Repeat("a", 33)

	tests := []struct {
		desc        string
		displayName string
	}{
		{"leading whitespace", " Alice"},
		{"trailing whitespace", "Alice "},
		{"too long (33 codepoints)", tooLong},
		{"control char (NUL)", "Alice\x00"},
		{"control char (tab)", "Alice\tBob"},
		{"newline LF", "Alice\nBob"},
		{"newline CR", "Alice\rBob"},
		{"newline VT", "Alice\vBob"},
		{"newline FF", "Alice\fBob"},
		{"newline NEL (U+0085)", "Alice\u0085Bob"},
		{"line separator (U+2028)", "Alice\u2028Bob"},
		{"paragraph separator (U+2029)", "Alice\u2029Bob"},
		{"zero-width space (U+200B)", "Al\u200Bice"},
		{"zero-width joiner (U+200D)", "Al\u200Dice"},
		{"left-to-right override (U+202D)", "Al\u202dice"},
		{"right-to-left override (U+202E)", "Al\u202eice"},
		{"BOM (U+FEFF)", "Al\ufeffice"},
		{"private use (U+E000)", "Al\ue000ice"},
		{"non-NFC (decomposed e + combining acute)", "Cafe\u0301"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{
				Authority:   addr,
				DisplayName: tc.displayName,
			})
			require.ErrorIs(t, err, types.ErrInvalidDisplayName, "expected ErrInvalidDisplayName for %q", tc.displayName)
		})
	}
}

func TestSetDisplayName_AcceptedForms(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	addr := sdk.AccAddress([]byte("alice_test_account_1")).String()

	// 32-codepoint exactly (boundary).
	exactly32 := strings.Repeat("a", 32)

	tests := []struct {
		desc        string
		displayName string
	}{
		{"plain ASCII", "Alice"},
		{"with ASCII space", "Alice the Great"},
		{"NFC-normalized accented (precomposed é)", "Caf\u00e9"},
		{"emoji", "Alice ✨"},
		{"32 codepoints exactly", exactly32},
		{"single character", "x"},
		{"empty (clear)", ""},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{
				Authority:   addr,
				DisplayName: tc.displayName,
			})
			require.NoError(t, err)

			info, err := f.keeper.Owners.Get(f.ctx, addr)
			require.NoError(t, err)
			require.Equal(t, tc.displayName, info.DisplayName)
		})
	}
}
