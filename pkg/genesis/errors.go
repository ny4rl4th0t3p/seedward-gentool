package genesis

import "errors"

// Sentinel errors for the genesis-construction failure modes a caller may want
// to branch on. Throw sites wrap these with %w and add context, so callers can
// match them with errors.Is while still getting a descriptive message.
var (
	// ErrInvalidVesting indicates malformed vesting parameters for an account —
	// e.g. neither a delayed nor continuous schedule is derivable from the
	// start/end times, or the vesting amount exceeds the account's balance.
	ErrInvalidVesting = errors.New("invalid vesting parameters")

	// ErrSupplyMismatch indicates the configured total supply does not equal the
	// sum of every balance actually created at genesis.
	ErrSupplyMismatch = errors.New("total supply mismatch")

	// ErrDelegationBelowReserve indicates a delegating account's amount does not
	// exceed the required non-staked liquid reserve, so it cannot delegate while
	// keeping a spendable balance.
	ErrDelegationBelowReserve = errors.New("delegating amount must exceed the non-staked reserve")
)
