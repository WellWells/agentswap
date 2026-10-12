package transfer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
)

const (
	Format        = "agentswap-export"
	Version       = 1
	MinPassphrase = 8

	kdfName  = "pbkdf2-sha256"
	maxIter  = 10_000_000
	keyLen   = 32
	saltLen  = 16
	nonceLen = 12
)

var iterations = 600_000

var (
	ErrFormat     = errors.New("not an agentswap export file")
	ErrVersion    = errors.New("export file was written by a newer agentswap")
	ErrPassphrase = errors.New("wrong passphrase or damaged file")
)

type Entry struct {
	Alias    string `json:"alias,omitempty"`
	Snapshot []byte `json:"snapshot"`
}

type Bundle map[string][]Entry

type KDF struct {
	Name       string `json:"name"`
	Iterations int    `json:"iterations"`
	Salt       []byte `json:"salt"`
}

type header struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	KDF     *KDF   `json:"kdf,omitempty"`
	Nonce   []byte `json:"nonce,omitempty"`
}

type envelope struct {
	header
	Data     []byte `json:"data,omitempty"`
	Accounts Bundle `json:"accounts,omitempty"`
}

type File struct {
	env envelope
}

func Seal(b Bundle, passphrase []byte) ([]byte, error) {
	plain, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, saltLen)
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	h := header{Format: Format, Version: Version, KDF: &KDF{Name: kdfName, Iterations: iterations, Salt: salt}, Nonce: nonce}
	aead, aad, err := h.aead(passphrase)
	if err != nil {
		return nil, err
	}
	return encode(envelope{header: h, Data: aead.Seal(nil, nonce, plain, aad)})
}

func Plain(b Bundle) ([]byte, error) {
	return encode(envelope{header: header{Format: Format, Version: Version}, Accounts: b})
}

func encode(env envelope) ([]byte, error) {
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func Parse(data []byte) (*File, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Format != Format || env.Version < 1 {
		return nil, ErrFormat
	}
	if env.Version > Version {
		return nil, ErrVersion
	}
	if env.KDF == nil {
		if env.Nonce != nil || env.Data != nil || env.Accounts == nil {
			return nil, ErrFormat
		}
		return &File{env}, nil
	}
	k := env.KDF
	if k.Name != kdfName || k.Iterations < 1 || k.Iterations > maxIter || len(k.Salt) < 8 || len(env.Nonce) != nonceLen || env.Data == nil || env.Accounts != nil {
		return nil, ErrFormat
	}
	return &File{env}, nil
}

func (f *File) Encrypted() bool { return f.env.KDF != nil }

func (f *File) Open(passphrase []byte) (Bundle, error) {
	if !f.Encrypted() {
		return f.env.Accounts, nil
	}
	aead, aad, err := f.env.header.aead(passphrase)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, f.env.Nonce, f.env.Data, aad)
	if err != nil {
		return nil, ErrPassphrase
	}
	var b Bundle
	if err := json.Unmarshal(plain, &b); err != nil {
		return nil, ErrFormat
	}
	return b, nil
}

func (h header) aead(passphrase []byte) (cipher.AEAD, []byte, error) {
	aad, err := json.Marshal(h)
	if err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(pbkdf2(passphrase, h.KDF.Salt, h.KDF.Iterations, keyLen))
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	return aead, aad, nil
}
