package types_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// MsgUpdateOperationalParams is a FULL REPLACEMENT: the message carries a whole
// RepOperationalParams, so any field the caller omits arrives as its zero value
// and overwrites what was there. The e2e tests build that message by reading
// current params and re-listing every field in a jq object, which means those
// hand-maintained lists have to stay complete as params are added.
//
// They do not, on their own. When federation-verifier pay moved into x/rep it
// added eight operational params, and all three builders kept working with the
// old list -- so `verifier_reward_epoch_blocks` arrived as 0, Validate rejected
// it with "must be positive", and two suites (operational_params_test,
// project_lifecycle_test) failed several hours into a parallel e2e run. The
// silent version of the same bug is worse: a param whose zero value is *valid*
// gets reset by every op-params proposal and nothing reports it.
//
// This test moves that discovery from a multi-hour e2e run to `go test`.
func TestE2EOperationalParamsBuildersListEveryField(t *testing.T) {
	root := repoRoot(t)
	builders := map[string]int{
		"test/rep/operational_params_test.sh": 2,
		"test/rep/project_lifecycle_test.sh":  1,
	}

	want := operationalParamJSONNames()
	require.NotEmpty(t, want, "reflection found no json-tagged fields")

	// Matches the `jq '.params | { a, b, c }'` object-construction the builders use.
	blockRe := regexp.MustCompile(`(?s)jq '\.params \| \{(.*?)\}'`)
	identRe := regexp.MustCompile(`[a-z][a-z_0-9]*`)

	for rel, wantBlocks := range builders {
		path := filepath.Join(root, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v (if this builder was renamed or removed, update this test)", rel, err)
		}
		blocks := blockRe.FindAllStringSubmatch(string(raw), -1)
		require.Len(t, blocks, wantBlocks,
			"%s: expected %d params-builder block(s); the count changed, so re-check each one", rel, wantBlocks)

		for i, b := range blocks {
			listed := map[string]struct{}{}
			for _, id := range identRe.FindAllString(b[1], -1) {
				listed[id] = struct{}{}
			}
			var missing []string
			for _, f := range want {
				if _, ok := listed[f]; !ok {
					missing = append(missing, f)
				}
			}
			require.Emptyf(t, missing,
				"%s block %d omits %d operational param(s): %s\n"+
					"MsgUpdateOperationalParams is a full replacement, so each of these would be sent as its zero value and overwrite the live setting.",
				rel, i+1, len(missing), strings.Join(missing, ", "))
		}
	}
}

// TestE2EDecFieldListsCoverEveryDec guards the other half of the same builders:
// the CLI renders LegacyDec params as raw 18-precision integers, and the
// converter has to turn each one back into a decimal string before the proposal
// JSON will unmarshal. A Dec field missing from DEC_FIELDS is sent in the wrong
// form and the message is rejected.
func TestE2EDecFieldListsCoverEveryDec(t *testing.T) {
	root := repoRoot(t)
	decs := operationalParamDecNames()
	require.NotEmpty(t, decs)

	listRe := regexp.MustCompile(`(?s)DEC_FIELDS = \[(.*?)\]`)
	for _, rel := range []string{
		"test/rep/operational_params_test.sh",
		"test/rep/project_lifecycle_test.sh",
	} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		lists := listRe.FindAllStringSubmatch(string(raw), -1)
		require.NotEmpty(t, lists, "%s: no DEC_FIELDS list found", rel)

		for i, l := range lists {
			var missing []string
			for _, d := range decs {
				if !strings.Contains(l[1], "'"+d+"'") {
					missing = append(missing, d)
				}
			}
			require.Emptyf(t, missing,
				"%s DEC_FIELDS list %d omits %d LegacyDec param(s): %s\n"+
					"Each would be sent as a raw 18-precision integer instead of a decimal string, and the proposal would fail to unmarshal.",
				rel, i+1, len(missing), strings.Join(missing, ", "))
		}
	}
}

// operationalParamJSONNames returns the proto JSON name of every field on
// RepOperationalParams.
func operationalParamJSONNames() []string {
	t := reflect.TypeOf(types.RepOperationalParams{})
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if name := jsonTagName(t.Field(i)); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// operationalParamDecNames returns just the LegacyDec-typed fields, which are
// the ones needing the decimal-string conversion.
func operationalParamDecNames() []string {
	t := reflect.TypeOf(types.RepOperationalParams{})
	out := make([]string, 0, 8)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type.String() != "math.LegacyDec" {
			continue
		}
		if name := jsonTagName(f); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func jsonTagName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" || tag == "-" {
		return ""
	}
	return strings.SplitN(tag, ",", 2)[0]
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate repo root (no go.mod found walking up)")
	return ""
}
