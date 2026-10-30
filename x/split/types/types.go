package types

// MaxShareWeight bounds individual share weights so no single recipient can be
// configured with a runaway weight that would crowd out all other shares.
const MaxShareWeight uint64 = 10000

// MaxTotalShareWeight bounds the sum of every registered share. Weights are
// relative, so a bound on each share alone would still let one new share
// outweigh the genesis pillars (about 1000 in total) many times over.
const MaxTotalShareWeight uint64 = 10000
