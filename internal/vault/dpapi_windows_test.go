package vault

import (
	"bytes"
	"errors"
	"testing"
)

func TestDPAPIRoundTripAndTamper(t *testing.T) {
	v := dpapi{}
	plain := []byte(`{"refreshToken":"rt"}`)
	sealed, err := v.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !Sealed(sealed) || sealed[len(Magic)] != schemeDPAPI || bytes.Contains(sealed, []byte(`"rt"`)) {
		t.Fatal("bad frame")
	}
	got, err := v.Open(sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open %q %v", got, err)
	}
	sealed[len(sealed)-5] ^= 0xff
	if _, err := v.Open(sealed); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("tamper: %v", err)
	}
	if got, err := v.Open([]byte("{}")); err != nil || string(got) != "{}" {
		t.Fatalf("legacy %q %v", got, err)
	}
	if _, err := v.Open(frame(schemeKeyring, []byte("xxxxxxxxxxxxxxxxxxxx"))); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("foreign scheme: %v", err)
	}
}
