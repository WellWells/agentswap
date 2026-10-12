package ui

import (
	"io"
	"strings"
	"testing"
)

func TestReadLine(t *testing.T) {
	r := strings.NewReader("first pass\r\nsecond\nlast")
	for _, want := range []string{"first pass", "second", "last"} {
		got, err := readLine(r)
		if err != nil || got != want {
			t.Fatalf("readLine = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := readLine(r); err != io.EOF {
		t.Fatalf("after input: err = %v", err)
	}
}
