package swap

type Identity struct {
	Key   string
	Email string
	Plan  string
	Mode  string
}

type SameLogin interface {
	SameLogin(a, b []byte) bool
}

type LoginVerifier interface {
	VerifyOwner([]byte) error
	CheckReplacement(saved, incoming []byte, freshLogin bool) error
}

type SnapshotFilter interface {
	AccountSnapshot([]byte) ([]byte, error)
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
