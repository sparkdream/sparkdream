package types_test

import (
	"testing"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	_ "sparkdream/x/forum/types"
)

// TestForumParamsTagsDense pins the dense field numbering of the forum
// params messages (max tag == field count, no gaps) and the removal of the
// dead appeal-fee params: appeals are bonded through x/rep, never charged a
// forum fee.
func TestForumParamsTagsDense(t *testing.T) {
	for _, name := range []string{"sparkdream.forum.v1.Params", "sparkdream.forum.v1.ForumOperationalParams"} {
		d, err := gogoproto.HybridResolver.FindDescriptorByName(protoreflect.FullName(name))
		require.NoError(t, err, name)
		md := d.(protoreflect.MessageDescriptor)
		fields := md.Fields()
		seen := map[protoreflect.FieldNumber]bool{}
		max := protoreflect.FieldNumber(0)
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			require.False(t, seen[f.Number()], "%s: duplicate tag %d", name, f.Number())
			seen[f.Number()] = true
			if f.Number() > max {
				max = f.Number()
			}
			for _, dead := range []string{"appeal_fee_amount", "lock_appeal_fee_amount", "move_appeal_fee_amount"} {
				require.NotEqual(t, dead, string(f.Name()), "%s still has %s", name, dead)
			}
		}
		require.Equal(t, int(max), fields.Len(), "%s: tags must be dense", name)
		require.Zero(t, md.ReservedRanges().Len(), "%s: no reserved tags", name)
	}
}
