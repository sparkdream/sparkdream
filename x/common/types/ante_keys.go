package types

// feePaidCtxKey is the private type of ContextKeyFeePaid. A typed key cannot
// collide with any other package's context value, and x/shield and x/session
// share this one value instead of two string constants that merely agree.
type feePaidCtxKey struct{}

// ContextKeyFeePaid is the context key an ante decorator sets (to true) once
// it has paid the tx fee on someone's behalf: ShieldGasDecorator (fee paid by
// the shield module) and SessionFeeDecorator (fee paid by the session
// granter). SkipIfFeePaidDecorator reads it and skips the standard
// DeductFeeDecorator. A decorator that sets it must also enforce the node's
// CheckTx minimum gas prices itself, because the skipped decorator is where
// the SDK does that.
var ContextKeyFeePaid = feePaidCtxKey{}
