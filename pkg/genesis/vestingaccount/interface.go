package vestingaccount

type VestingAccount interface {
	Address() string
	Amount() int64
	DelegateTo() string
}
