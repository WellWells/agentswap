package jsonx

import (
	"strings"
	"testing"
)

const sample = "{\n  \"numStartups\": 3,\n  \"oauthAccount\": {\n    \"emailAddress\": \"a@x\"\n  },\n  \"projects\": {\"C:/x\": {\"allowedTools\": []}}\n}\n"

func TestSetTopPreservesBytes(t *testing.T) {
	out, err := Set([]byte(sample), "oauthAccount", []byte(`{"emailAddress":"b@x"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sample, "{\n    \"emailAddress\": \"a@x\"\n  }", `{"emailAddress":"b@x"}`, 1)
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

func TestSetTopInsertsMissingKey(t *testing.T) {
	out, err := Set([]byte("{\n  \"a\": 1\n}\n"), "oauthAccount", []byte(`{"x":1}`))
	if err != nil || string(out) != "{\n  \"a\": 1,\n  \"oauthAccount\": {\"x\":1}\n}\n" {
		t.Fatalf("%q %v", out, err)
	}
	out, err = Set([]byte("{}"), "k", []byte("1"))
	if err != nil || string(out) != `{"k":1}` {
		t.Fatalf("%q %v", out, err)
	}
}

func TestGetTopAndMembers(t *testing.T) {
	v, ok, err := Get([]byte(sample), "oauthAccount")
	if err != nil || !ok || !strings.Contains(string(v), "a@x") {
		t.Fatalf("%s %v %v", v, ok, err)
	}
	if _, ok, _ := Get([]byte(sample), "nope"); ok {
		t.Fatal("found missing key")
	}
	if _, err := Members([]byte("[1]")); err == nil {
		t.Fatal("array accepted")
	}
	if _, err := Members([]byte(`{"a":1`)); err == nil {
		t.Fatal("truncated accepted")
	}
	ms, _ := Members([]byte(`{"a" : "x" , "b":[1, 2], "c": 12}`))
	if string(ms[0].Val) != `"x"` || string(ms[1].Val) != "[1, 2]" || string(ms[2].Val) != "12" {
		t.Fatalf("%q %q %q", ms[0].Val, ms[1].Val, ms[2].Val)
	}
}

func TestEncodeObject(t *testing.T) {
	ms, _ := Members([]byte(`{"a":1,"b":{"c":2}}`))
	if got := string(Encode(ms, false)); got != `{"a":1,"b":{"c":2}}` {
		t.Fatal(got)
	}
	if got := string(Encode(ms, true)); got != "{\n  \"a\": 1,\n  \"b\": {\"c\":2}\n}" {
		t.Fatal(got)
	}
	if got := string(Encode(nil, true)); got != "{}" {
		t.Fatal(got)
	}
}
