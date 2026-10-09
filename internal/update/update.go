package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/fsx"
)

const maxSize = 200 << 20

var (
	ErrChecksum = errors.New("checksum mismatch")
	ErrNoTag    = errors.New("no release found")
)

type Source struct {
	Base   string
	Repo   string
	Client *http.Client
}

func (s Source) base() string {
	if s.Base != "" {
		return strings.TrimSuffix(s.Base, "/")
	}
	return "https://github.com"
}

func (s Source) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}

func (s Source) Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base()+"/"+s.Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	c := *s.client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("%w: %s", ErrNoTag, resp.Status)
	}
	loc, err := resp.Location()
	if err != nil {
		return "", err
	}
	dir, tag := path.Split(loc.Path)
	if !strings.HasSuffix(dir, "/releases/tag/") || tag == "" {
		return "", fmt.Errorf("%w: %s", ErrNoTag, loc)
	}
	return tag, nil
}

func (s Source) Download(ctx context.Context, tag, name string) ([]byte, error) {
	u := s.base() + "/" + s.Repo + "/releases/download/" + url.PathEscape(tag) + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", name, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxSize {
		return nil, fmt.Errorf("download %s: too large", name)
	}
	return b, nil
}

type version struct {
	core [3]int
	pre  []string
}

func parse(v string) (version, bool) {
	var out version
	s, ok := strings.CutPrefix(v, "v")
	if !ok {
		return out, false
	}
	s, build, hasBuild := strings.Cut(s, "+")
	if hasBuild && !idents(build) {
		return out, false
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return out, false
		}
		out.core[i] = n
	}
	if hasPre {
		if !idents(pre) {
			return out, false
		}
		out.pre = strings.Split(pre, ".")
	}
	return out, true
}

func idents(s string) bool {
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		for _, c := range id {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
				return false
			}
		}
	}
	return true
}

func Valid(v string) bool {
	_, ok := parse(v)
	return ok
}

func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		na, ea := strconv.Atoi(a[i])
		nb, eb := strconv.Atoi(b[i])
		switch {
		case ea == nil && eb == nil:
			if na != nb {
				return cmpInt(na, nb)
			}
		case ea == nil:
			return -1
		case eb == nil:
			return 1
		default:
			if c := strings.Compare(a[i], b[i]); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(a), len(b))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func Newer(latest, current string) bool {
	l, ok := parse(latest)
	if !ok {
		return false
	}
	c, ok := parse(current)
	if !ok {
		return false
	}
	for i := range l.core {
		if l.core[i] != c.core[i] {
			return l.core[i] > c.core[i]
		}
	}
	return comparePre(l.pre, c.pre) > 0
}

func Asset(goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "agentswap_" + goos + "_" + goarch + ext
}

func Verify(sums []byte, name string, data []byte) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if strings.EqualFold(f[0], got) {
				return nil
			}
			return fmt.Errorf("%w: %s", ErrChecksum, name)
		}
	}
	return fmt.Errorf("%w: %s not listed", ErrChecksum, name)
}

func Extract(archive []byte, goos string) ([]byte, error) {
	if goos == "windows" {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.Name != "agentswap.exe" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return readAll(rc)
		}
		return nil, errors.New("agentswap.exe not found in archive")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("agentswap not found in archive")
		}
		if err != nil {
			return nil, err
		}
		if h.Name == "agentswap" && h.Typeflag == tar.TypeReg {
			return readAll(tr)
		}
	}
}

func readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err == nil && len(b) > maxSize {
		err = errors.New("binary too large")
	}
	return b, err
}

func rename(from, to string) error {
	for i := 0; ; i++ {
		err := os.Rename(from, to)
		if err == nil || i >= 20 {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func Install(exe string, bin []byte, check func() error) error {
	tmp, old := exe+".new", exe+".old"
	os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, err = f.Write(bin)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(old)
	if err := rename(exe, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := rename(tmp, exe); err != nil {
		rename(old, exe)
		os.Remove(tmp)
		return err
	}
	if check == nil {
		return nil
	}
	if err := check(); err != nil {
		if rerr := rename(old, exe); rerr != nil {
			return fmt.Errorf("%w; restoring %s failed: %v", err, filepath.Base(old), rerr)
		}
		return err
	}
	return nil
}

func Homebrew(exe string) bool {
	return strings.Contains(filepath.ToSlash(exe), "/Cellar/")
}

type Cache struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest,omitempty"`
}

func LoadCache(path string) Cache {
	var c Cache
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

func SaveCache(path string, c Cache) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(path, b, 0o600)
}
