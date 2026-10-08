package vault

import (
	"bytes"
	"errors"
	"testing"
)

func TestWithKeyRoundTrip(t *testing.T) {
	v := WithKey(bytes.Repeat([]byte{1}, 32))
	plain := []byte(`{"tokens":{"refresh_token":"rt"}}`)
	sealed, err := v.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !Sealed(sealed) || bytes.Contains(sealed, []byte("refresh_token")) {
		t.Fatalf("not sealed: %q", sealed)
	}
	got, err := v.Open(sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open = %q, %v", got, err)
	}
}

func TestOpenPassesLegacyPlaintext(t *testing.T) {
	v := WithKey(bytes.Repeat([]byte{1}, 32))
	got, err := v.Open([]byte(`{"a":1}`))
	if err != nil || string(got) != `{"a":1}` {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestOpenRejectsTamperedOrForeignKey(t *testing.T) {
	v := WithKey(bytes.Repeat([]byte{1}, 32))
	sealed, _ := v.Seal([]byte("secret"))
	bad := append([]byte(nil), sealed...)
	bad[len(bad)-1] ^= 0xff
	if _, err := v.Open(bad); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("tampered: %v", err)
	}
	other := WithKey(bytes.Repeat([]byte{2}, 32))
	if _, err := other.Open(sealed); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("other key: %v", err)
	}
}
