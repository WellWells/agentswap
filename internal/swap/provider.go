package swap

type Identity struct {
	Key   string
	Email string
	Plan  string
	Mode  string
}

type LiveLocker interface {
	LockLive() (func(), error)
}

type Provider interface {
	Name() string
	ReadLive() ([]byte, error)
	WriteLive([]byte) error
	Identify([]byte) (Identity, error)
}
