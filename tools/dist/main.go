package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const allTargets = "linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64"

var aliases = []string{"cxswap", "codexswap", "ccswap", "claudeswap"}

type target struct{ OS, Arch string }

func (t target) archive() string {
	ext := ".tar.gz"
	if t.OS == "windows" {
		ext = ".zip"
	}
	return "agentswap_" + t.OS + "_" + t.Arch + ext
}

type file struct {
	Name string
	Data []byte
}

func parseTargets(s string) ([]target, error) {
	var out []target
	for _, part := range strings.Split(s, ",") {
		goos, arch, ok := strings.Cut(strings.TrimSpace(part), "/")
		if !ok || (goos != "linux" && goos != "darwin" && goos != "windows") || (arch != "amd64" && arch != "arm64") {
			return nil, fmt.Errorf("unsupported target %q", part)
		}
		out = append(out, target{goos, arch})
	}
	return out, nil
}

func writeTarGz(w io.Writer, bin []byte, extras []file, links []string, mtime time.Time) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "agentswap", Mode: 0o755, Size: int64(len(bin)), ModTime: mtime, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := tw.Write(bin); err != nil {
		return err
	}
	for _, l := range links {
		if err := tw.WriteHeader(&tar.Header{Name: l, Linkname: "agentswap", Mode: 0o755, ModTime: mtime, Typeflag: tar.TypeLink}); err != nil {
			return err
		}
	}
	for _, f := range extras {
		if err := tw.WriteHeader(&tar.Header{Name: f.Name, Mode: 0o644, Size: int64(len(f.Data)), ModTime: mtime, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(f.Data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeZip(w io.Writer, binName string, bin []byte, extras []file, mtime time.Time) error {
	zw := zip.NewWriter(w)
	add := func(name string, data []byte, mode os.FileMode) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: mtime}
		h.SetMode(mode)
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = fw.Write(data)
		return err
	}
	if err := add(binName, bin, 0o755); err != nil {
		return err
	}
	for _, f := range extras {
		if err := add(f.Name, f.Data, 0o644); err != nil {
			return err
		}
	}
	return zw.Close()
}

func writeChecksums(dir string, names []string) error {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var b strings.Builder
	for _, n := range sorted {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), n)
	}
	return os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(b.String()), 0o644)
}

func buildTime() time.Time {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Now().UTC()
}

func build(t target, version, outFile string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version="+version, "-o", outFile, "./cmd/agentswap")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.OS, "GOARCH="+t.Arch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func run() error {
	version := flag.String("version", "dev", "version embedded in the binary")
	out := flag.String("out", "dist", "output directory")
	targetList := flag.String("targets", allTargets, "comma-separated GOOS/GOARCH list")
	flag.Parse()

	if _, err := os.Stat("go.mod"); err != nil {
		return errors.New("run from the repository root")
	}
	targets, err := parseTargets(*targetList)
	if err != nil {
		return err
	}
	var extras []file
	for _, name := range []string{"README.md", "LICENSE", "NOTICE"} {
		if data, err := os.ReadFile(name); err == nil {
			extras = append(extras, file{name, data})
		}
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "agentswap-dist-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	mtime := buildTime()
	var produced []string
	for _, t := range targets {
		binName := "agentswap"
		if t.OS == "windows" {
			binName += ".exe"
		}
		binPath := filepath.Join(tmp, t.OS+"_"+t.Arch, binName)
		if err := build(t, *version, binPath); err != nil {
			return fmt.Errorf("build %s/%s: %w", t.OS, t.Arch, err)
		}
		bin, err := os.ReadFile(binPath)
		if err != nil {
			return err
		}
		f, err := os.Create(filepath.Join(*out, t.archive()))
		if err != nil {
			return err
		}
		if t.OS == "windows" {
			err = writeZip(f, binName, bin, extras, mtime)
		} else {
			err = writeTarGz(f, bin, extras, aliases, mtime)
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		produced = append(produced, t.archive())
		fmt.Printf("%-36s %6.1f MB\n", t.archive(), float64(len(bin))/1e6)
	}
	for _, name := range []string{"install.sh", "install.ps1"} {
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(*out, name), data, 0o644); err != nil {
			return err
		}
		produced = append(produced, name)
	}
	return writeChecksums(*out, produced)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "dist:", err)
		os.Exit(1)
	}
}
