package claude

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/WellWells/agentswap/internal/fsx"
)

const (
	credmanAccount  = "claude-code-user"
	credmanChunk    = 2400
	credmanMaxParts = 256
)

var (
	ErrKeychainLocked   = errors.New("the macOS keychain holding the Claude Code login is locked; unlock it (for example `security unlock-keychain ~/Library/Keychains/login.keychain-db`) and try again")
	ErrKeychainOverflow = errors.New("Claude Code split this login across several keychain items, which agentswap cannot read yet")
	errKeychainTooLarge = errors.New("credentials too large to write to the keychain safely")
	errCredmanTooLarge  = errors.New("credentials too large for Windows Credential Manager")
	errCredmanBroken    = errors.New("the Claude Code login in Windows Credential Manager is incomplete; run `claude` once or log in again")
	errCredmanReadback  = errors.New("Windows Credential Manager did not return the Claude Code login that was just written")
)

type CredStore interface {
	Get(service, name string) ([]byte, error)
	Set(service, name string, value []byte) error
	Delete(service, name string) error
}

type Credman struct {
	Service string
	Account string
	Store   CredStore
}

type secureStore interface {
	load() ([]byte, bool, error)
	save([]byte) error
	clear() error
}

func secureService(lookup func(string) (string, bool)) string {
	dir, set := lookup("CLAUDE_SECURESTORAGE_CONFIG_DIR")
	if !set {
		dir, _ = lookup("CLAUDE_CONFIG_DIR")
	}
	if dir == "" {
		return "Claude Code-credentials"
	}
	sum := sha256.Sum256([]byte(dir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

func credmanEnabled(getenv func(string) string, home string) bool {
	if getenv("CLAUDE_CODE_FORCE_WINDOWS_CREDMAN") == "1" {
		return true
	}
	legacy := filepath.Join(home, ".claude", ".config.json")
	config := filepath.Join(home, ".claude.json")
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		legacy, config = filepath.Join(dir, ".config.json"), filepath.Join(dir, ".claude.json")
	}
	b, err := os.ReadFile(legacy)
	if err != nil && !absentFile(legacy, err) {
		return false
	}
	if len(b) == 0 {
		if b, err = os.ReadFile(config); err != nil {
			return false
		}
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(b, &top) != nil {
		return false
	}
	features := top["cachedGrowthBookFeatures"]
	if jsonNull(features) {
		features = top["cachedFeatureFlags"]
	}
	var flags map[string]json.RawMessage
	if jsonNull(features) || json.Unmarshal(features, &flags) != nil {
		return false
	}
	return string(bytes.TrimSpace(flags["tengu_windows_credman"])) == "true"
}

func absentFile(path string, err error) bool {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP) {
		return true
	}
	st, serr := os.Stat(path)
	return serr == nil && st.IsDir()
}

func jsonNull(b json.RawMessage) bool {
	t := bytes.TrimSpace(b)
	return len(t) == 0 || string(t) == "null"
}

func usableJSON(b []byte) ([]byte, bool) {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || string(t) == "null" || !json.Valid(t) {
		return nil, false
	}
	return t, true
}

func compactJSON(b []byte) []byte {
	var out bytes.Buffer
	if json.Compact(&out, b) != nil {
		return b
	}
	return out.Bytes()
}

func keychainJSON(b []byte) []byte {
	var out bytes.Buffer
	for _, r := range string(compactJSON(b)) {
		switch {
		case r >= 0x20 && r <= 0x7e:
			out.WriteRune(r)
		case r > 0xffff:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&out, `\u%04x\u%04x`, hi, lo)
		default:
			fmt.Fprintf(&out, `\u%04x`, r)
		}
	}
	return out.Bytes()
}

func (k Keychain) load() ([]byte, bool, error) {
	out, code, err := k.run(nil, "find-generic-password", "-a", k.Account, "-w", "-s", k.Service)
	if err != nil {
		return nil, false, err
	}
	switch code {
	case 0:
	case 44:
		return nil, false, nil
	case 36:
		return nil, false, ErrKeychainLocked
	default:
		return nil, false, fmt.Errorf("security find-generic-password: exit %d", code)
	}
	b, ok := usableJSON(out)
	if !ok {
		return nil, false, nil
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(b, &top) == nil {
		if _, sealed := top["keychainOverflow"]; sealed {
			return nil, false, ErrKeychainOverflow
		}
	}
	return b, true, nil
}

func (k Keychain) save(b []byte) error { return k.Write(keychainJSON(b)) }

func (k Keychain) clear() error { return k.Delete() }

func (c Credman) part(suffix string) string { return c.Account + "#" + suffix }

func (c Credman) get(name string) ([]byte, bool, error) {
	b, err := c.Store.Get(c.Service, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (c Credman) meta() (n, l int, ok bool, err error) {
	raw, found, err := c.get(c.part("m"))
	if err != nil || !found {
		return 0, 0, false, err
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return 0, 0, false, nil
	}
	num := func(key string) (int, bool) {
		var f float64
		if json.Unmarshal(m[key], &f) != nil || f != math.Trunc(f) || f < 1 || f > credmanMaxParts*credmanChunk {
			return 0, false
		}
		return int(f), true
	}
	n, okN := num("n")
	l, okL := num("l")
	if !okN || !okL || n > credmanMaxParts || l > n*credmanChunk {
		return 0, 0, false, nil
	}
	return n, l, true, nil
}

var credmanRetryDelay = 150 * time.Millisecond

func (c Credman) load() ([]byte, bool, error) {
	var (
		b   []byte
		ok  bool
		err error
	)
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(credmanRetryDelay)
		}
		if b, ok, err = c.loadOnce(); err == nil {
			break
		}
	}
	return b, ok, err
}

func (c Credman) loadOnce() ([]byte, bool, error) {
	n, l, chunked, err := c.meta()
	if err != nil {
		return nil, false, err
	}
	if !chunked {
		b, found, err := c.get(c.Account)
		if err != nil || !found {
			return nil, false, err
		}
		b, ok := usableJSON(b)
		return b, ok, nil
	}
	var joined bytes.Buffer
	for i := 0; i < n; i++ {
		b, found, err := c.get(c.part(strconv.Itoa(i)))
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, errCredmanBroken
		}
		joined.Write(b)
	}
	if joined.Len() != l {
		return nil, false, errCredmanBroken
	}
	b, err := base64.StdEncoding.DecodeString(joined.String())
	if err != nil {
		return nil, false, errCredmanBroken
	}
	b, ok := usableJSON(b)
	if !ok {
		return nil, false, errCredmanBroken
	}
	return b, true, nil
}

func (c Credman) set(name string, b []byte) error { return c.Store.Set(c.Service, name, b) }

func (c Credman) del(name string) error { return c.Store.Delete(c.Service, name) }

func (c Credman) write(v []byte) error {
	old, _, _, err := c.meta()
	if err != nil {
		return err
	}
	if len(v) <= credmanChunk {
		if err := c.set(c.Account, v); err != nil {
			return err
		}
		if old > 0 {
			if err := c.set(c.part("p"), []byte(strconv.Itoa(old))); err != nil {
				return err
			}
			errs := []error{c.del(c.part("m"))}
			for i := 0; i < old; i++ {
				errs = append(errs, c.del(c.part(strconv.Itoa(i))))
			}
			if err := errors.Join(errs...); err != nil {
				return err
			}
			if err := c.del(c.part("p")); err != nil {
				return err
			}
		}
	} else {
		s := base64.StdEncoding.EncodeToString(v)
		n := (len(s) + credmanChunk - 1) / credmanChunk
		if n > credmanMaxParts {
			return errCredmanTooLarge
		}
		if err := c.set(c.part("p"), []byte(strconv.Itoa(max(old, n)))); err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			if err := c.set(c.part(strconv.Itoa(i)), []byte(s[i*credmanChunk:min((i+1)*credmanChunk, len(s))])); err != nil {
				return err
			}
		}
		if err := c.set(c.part("m"), []byte(fmt.Sprintf(`{"n":%d,"l":%d}`, n, len(s)))); err != nil {
			return err
		}
		errs := []error{c.del(c.Account)}
		for i := n; i < old; i++ {
			errs = append(errs, c.del(c.part(strconv.Itoa(i))))
		}
		if err := errors.Join(errs...); err != nil {
			return err
		}
		if err := c.del(c.part("p")); err != nil {
			return err
		}
	}
	got, ok, err := c.load()
	if err != nil || !ok || !bytes.Equal(got, v) {
		return errCredmanReadback
	}
	return nil
}

func (c Credman) remove() error {
	n, _, chunked, err := c.meta()
	if err != nil {
		return err
	}
	errs := []error{c.del(c.Account)}
	if chunked {
		for i := 0; i < n; i++ {
			errs = append(errs, c.del(c.part(strconv.Itoa(i))))
		}
		errs = append(errs, c.del(c.part("m")))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	limit := n
	raw, found, err := c.get(c.part("p"))
	if err != nil {
		return err
	}
	if found {
		if p, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			limit = max(limit, min(max(p, 0), credmanMaxParts))
		}
	}
	for limit < credmanMaxParts {
		_, found, err := c.get(c.part(strconv.Itoa(limit)))
		if err != nil {
			return err
		}
		if !found {
			break
		}
		limit++
	}
	errs = []error{c.del(c.part("m")), c.del(c.part("p"))}
	for i := n; i < limit; i++ {
		errs = append(errs, c.del(c.part(strconv.Itoa(i))))
	}
	return errors.Join(errs...)
}

func (c Credman) save(b []byte) error { return c.write(compactJSON(b)) }

func (c Credman) clear() error { return c.remove() }

func (p Provider) secure() secureStore {
	switch {
	case p.Keychain != nil:
		return p.Keychain
	case p.Credman != nil:
		return p.Credman
	}
	return nil
}

func (p Provider) readCreds() ([]byte, error) {
	s := p.secure()
	if s == nil {
		return os.ReadFile(p.credPath())
	}
	b, ok, err := s.load()
	if err != nil {
		return nil, err
	}
	if ok {
		return b, nil
	}
	return os.ReadFile(p.credPath())
}

func (p Provider) readLiveCreds() ([]byte, error) {
	if p.Keychain != nil || p.Credman == nil {
		return p.readCreds()
	}
	if st, err := os.Stat(p.ConfigDir); err == nil && st.IsDir() {
		unlock, err := p.lockStorage()
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	b, ok, err := p.Credman.load()
	switch {
	case err == nil && ok:
		return b, nil
	case err == nil:
		return os.ReadFile(p.credPath())
	case errors.Is(err, errCredmanBroken):
		return nil, err
	}
	plain, perr := os.ReadFile(p.credPath())
	if perr != nil {
		return nil, err
	}
	return plain, nil
}

func (p Provider) writeCreds(b []byte) error {
	s := p.secure()
	if s == nil {
		return fsx.WriteAtomic(p.credPath(), b, 0o600)
	}
	_, had, err := s.load()
	if err != nil {
		return err
	}
	werr := s.save(b)
	if werr == nil {
		if !had {
			if err := os.Remove(p.credPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		} else if _, err := os.Stat(p.credPath()); err == nil {
			now := time.Now()
			os.Chtimes(p.credPath(), now, now)
		}
		return nil
	}
	if errors.Is(werr, ErrKeychainLocked) || errors.Is(werr, errKeychainTooLarge) {
		return werr
	}
	if err := fsx.WriteAtomic(p.credPath(), b, 0o600); err != nil {
		return errors.Join(werr, err)
	}
	if had || p.Credman != nil {
		if err := s.clear(); err != nil {
			return errors.Join(werr, err)
		}
	}
	return nil
}

func (p Provider) DeleteSecure() error {
	if s := p.secure(); s != nil {
		return s.clear()
	}
	return nil
}
