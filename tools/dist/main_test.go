package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var mtime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestParseTargets(t *testing.T) {
	got, err := parseTargets("linux/amd64, windows/arm64")
	if err != nil || len(got) != 2 || got[1] != (target{"windows", "arm64"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got[0].archive() != "agentswap_linux_amd64.tar.gz" || got[1].archive() != "agentswap_windows_arm64.zip" {
		t.Fatalf("names %s %s", got[0].archive(), got[1].archive())
	}
	for _, bad := range []string{"linux", "plan9/amd64", "linux/mips"} {
		if _, err := parseTargets(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTarGzHasBinaryAndExtras(t *testing.T) {
	var buf bytes.Buffer
	extras := []file{{"LICENSE", []byte("MIT")}}
	if err := writeTarGz(&buf, []byte("BIN"), extras, mtime); err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
		if h.Name == "agentswap" {
			b, _ := io.ReadAll(tr)
			if string(b) != "BIN" || h.Mode != 0o755 {
				t.Fatalf("binary %q mode %o", b, h.Mode)
			}
		}
	}
	if strings.Join(names, ",") != "agentswap,LICENSE" {
		t.Fatalf("entries %v", names)
	}
}

func TestZipHasExeAndExtras(t *testing.T) {
	var buf bytes.Buffer
	if err := writeZip(&buf, "agentswap.exe", []byte("BIN"), []file{{"README.md", []byte("hi")}}, mtime); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "agentswap.exe,README.md" {
		t.Fatalf("entries %v", names)
	}
}

func TestFormula(t *testing.T) {
	sums := map[target]string{
		{"darwin", "arm64"}: "da",
		{"darwin", "amd64"}: "di",
		{"linux", "arm64"}:  "la",
	}
	var buf bytes.Buffer
	if err := writeFormula(&buf, "v1.2.3", sums); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"class Agentswap < Formula\n",
		"  version \"1.2.3\"\n",
		"  on_macos do\n    on_arm do\n      url \"https://github.com/WellWells/agentswap/releases/download/v1.2.3/agentswap_darwin_arm64.tar.gz\"\n      sha256 \"da\"\n    end\n\n    on_intel do\n",
		"      sha256 \"di\"\n",
		"  on_linux do\n    on_arm do\n",
		"    system bin/\"agentswap\", \"link\"\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Count(got, "on_intel") != 1 || strings.Count(got, " do\n")+2 != strings.Count(got, "end\n") {
		t.Errorf("unbalanced blocks:\n%s", got)
	}
	for _, v := range []string{"dev", "1.2.3", "vnext"} {
		if err := writeFormula(io.Discard, v, sums); err == nil {
			t.Errorf("%q accepted", v)
		}
	}
	if err := writeFormula(io.Discard, "v1.0.0", map[target]string{{"windows", "amd64"}: "w"}); err == nil {
		t.Error("windows-only accepted")
	}
}

func TestChecksumsFormat(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.zip"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.tar.gz"), []byte("a"), 0o644)
	if err := writeChecksums(dir, []string{"b.zip", "a.tar.gz"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	want := "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb  a.tar.gz\n" +
		"3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d  b.zip\n"
	if string(got) != want {
		t.Fatalf("got:\n%s", got)
	}
}
