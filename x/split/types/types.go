package types

// MaxShareWeight bounds individual share weights so no single recipient can be
// configured with a runaway weight that would crowd out all other shares.
const MaxShareWeight uint64 = 10000
