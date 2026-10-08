package ui

import "testing"

func TestSystemLanguagesWindows(t *testing.T) {
	langs := SystemLanguages()
	if len(langs) == 0 || langs[0] == "" {
		t.Fatalf("got %q", langs)
	}
}
