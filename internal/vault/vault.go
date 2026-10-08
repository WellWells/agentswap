package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
)

const Magic = "AGSV1"

const (
	schemeDPAPI   byte = 1
	schemeKeyring byte = 2
	schemeFile    byte = 3
)

var (
	ErrKey         = errors.New("vault key unavailable")
	ErrCorrupt     = errors.New("encrypted data cannot be decrypted on this machine")
	errUnavailable = errors.New("key source unavailable")
)

type Vault interface {
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}

func Sealed(b []byte) bool {
	return len(b) > len(Magic) && string(b[:len(Magic)]) == Magic
}

func frame(scheme byte, body []byte) []byte {
	out := make([]byte, 0, len(Magic)+1+len(body))
	out = append(out, Magic...)
	out = append(out, scheme)
	return append(out, body...)
}

func unframe(b []byte) (byte, []byte) { return b[len(Magic)], b[len(Magic)+1:] }

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealGCM(key, plain []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, []byte(Magic)), nil
}

func openGCM(key, body []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	n := gcm.NonceSize()
	if len(body) < n {
		return nil, ErrCorrupt
	}
	out, err := gcm.Open(nil, body[:n], body[n:], []byte(Magic))
	if err != nil {
		return nil, ErrCorrupt
	}
	return out, nil
}

type keySource interface {
	scheme() byte
	key(create bool) ([]byte, error)
}

type keyed struct {
	sources []keySource
	mu      sync.Mutex
	cache   map[byte][]byte
}

func newKeyed(sources ...keySource) *keyed {
	return &keyed{sources: sources, cache: map[byte][]byte{}}
}

func (k *keyed) keyFor(s keySource, create bool) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if key, ok := k.cache[s.scheme()]; ok {
		return key, nil
	}
	key, err := s.key(create)
	if err != nil {
		return nil, err
	}
	k.cache[s.scheme()] = key
	return key, nil
}

func (k *keyed) Seal(plain []byte) ([]byte, error) {
	for _, s := range k.sources {
		key, err := k.keyFor(s, true)
		if errors.Is(err, errUnavailable) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrKey, err)
		}
		body, err := sealGCM(key, plain)
		if err != nil {
			return nil, err
		}
		return frame(s.scheme(), body), nil
	}
	return nil, ErrKey
}

func (k *keyed) Open(b []byte) ([]byte, error) {
	if !Sealed(b) {
		return b, nil
	}
	scheme, body := unframe(b)
	for _, s := range k.sources {
		if s.scheme() != scheme {
			continue
		}
		key, err := k.keyFor(s, false)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrKey, err)
		}
		return openGCM(key, body)
	}
	return nil, ErrCorrupt
}

type staticKey []byte

func (s staticKey) scheme() byte             { return schemeKeyring }
func (s staticKey) key(bool) ([]byte, error) { return s, nil }

func WithKey(key []byte) Vault { return newKeyed(staticKey(key)) }
