package official

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/WellWells/agentswap/internal/execx"
	"github.com/WellWells/agentswap/internal/fsx"
)

const (
	CodexVersion  = "0.160.1"
	ClaudeVersion = "2.1.294"
	ClaudeAxios   = "axios/1.15.2"
	AgyVersion    = "1.3.1"
	AgyBuild      = "994719654"
	CodexOrigin   = "codex_cli_rs"
)

var versionRe = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+[0-9A-Za-z.+-]*`)

type Detector struct {
	Cache  string
	Getenv func(string) string
	GOOS   string
	GOARCH string
	Run    execx.Runner
	Look   func(string) (string, error)

	mu    sync.Mutex
	found map[string]tool
	osv   string
}

type tool struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"`
	Version string `json:"version"`
	Build   string `json:"build,omitempty"`
}

func (d *Detector) run(name string, args ...string) ([]byte, int, error) {
	r := d.Run
	if r == nil {
		r = execx.Run
	}
	return r(nil, name, args...)
}

func (d *Detector) getenv(k string) string {
	if d.Getenv == nil {
		return os.Getenv(k)
	}
	return d.Getenv(k)
}

func (d *Detector) locate(name string) string {
	look := d.Look
	if look == nil {
		look = exec.LookPath
	}
	if p, err := look(name); err == nil {
		return p
	}
	if name == "agy" && d.GOOS == "windows" {
		if base := d.getenv("LOCALAPPDATA"); base != "" {
			p := filepath.Join(base, "agy", "bin", "agy.exe")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func (d *Detector) loadCache() map[string]tool {
	m := map[string]tool{}
	if d.Cache == "" {
		return m
	}
	if b, err := os.ReadFile(d.Cache); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func (d *Detector) detect(name string, build bool) (tool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.found[name]; ok {
		return t, t.Version != ""
	}
	if d.found == nil {
		d.found = map[string]tool{}
	}
	t := d.lookup(name, build)
	d.found[name] = t
	return t, t.Version != ""
}

func (d *Detector) lookup(name string, build bool) tool {
	path := d.locate(name)
	if path == "" {
		return tool{}
	}
	st, err := os.Stat(path)
	if err != nil {
		return tool{}
	}
	t := tool{Path: path, Size: st.Size(), ModTime: st.ModTime().UnixNano()}
	cache := d.loadCache()
	if c, ok := cache[name]; ok && c.Path == t.Path && c.Size == t.Size && c.ModTime == t.ModTime && c.Version != "" && (!build || c.Build != "") {
		return c
	}
	out, code, err := d.run(path, "--version")
	if err != nil || code != 0 {
		return tool{}
	}
	t.Version = versionRe.FindString(string(out))
	if t.Version == "" {
		return tool{}
	}
	if build {
		if t.Build = scanBuild(path); t.Build == "" {
			return tool{}
		}
	}
	if d.Cache != "" {
		cache[name] = t
		if b, err := json.MarshalIndent(cache, "", "  "); err == nil {
			os.MkdirAll(filepath.Dir(d.Cache), 0o700)
			fsx.WriteAtomic(d.Cache, b, 0o600)
		}
	}
	return t
}

func scanBuild(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	return findBuild(f)
}

func findBuild(r io.Reader) string {
	marker := []byte("google_src/files/")
	buf := make([]byte, 1<<20)
	var carry []byte
	for {
		n, err := r.Read(buf)
		data := append(carry, buf[:n]...)
		for off := 0; ; {
			i := bytes.Index(data[off:], marker)
			if i < 0 {
				break
			}
			start := off + i + len(marker)
			j := start
			for j < len(data) && data[j] >= '0' && data[j] <= '9' {
				j++
			}
			if j == len(data) {
				break
			}
			if j-start >= 6 && data[j] == '/' {
				return string(data[start:j])
			}
			off = start
		}
		if err != nil {
			return ""
		}
		keep := len(marker) + 32
		if len(data) > keep {
			data = data[len(data)-keep:]
		}
		carry = append([]byte(nil), data...)
	}
}

func (d *Detector) Codex() string {
	v := CodexVersion
	if t, ok := d.detect("codex", false); ok {
		v = t.Version
	}
	return fmt.Sprintf("%s/%s (%s; %s) %s", CodexOrigin, v, d.osInfo(), d.arch(), Terminal(d.getenv))
}

func (d *Detector) Claude() string {
	v := ClaudeVersion
	if t, ok := d.detect("claude", false); ok {
		v = t.Version
	}
	return "claude-code/" + v
}

func (d *Detector) Agy(authMethod string) string {
	v, cl := AgyVersion, AgyBuild
	if t, ok := d.detect("agy", true); ok {
		v, cl = t.Version, t.Build
	}
	if authMethod == "" {
		authMethod = "consumer"
	}
	return fmt.Sprintf("antigravity/cli/%s (aidev_client; os_type=%s; arch=%s; cl=%s; auth_method=%s)", v, d.GOOS, d.GOARCH, cl, authMethod)
}

func (d *Detector) arch() string {
	switch d.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		if d.GOOS == "linux" {
			return "aarch64"
		}
		return "arm64"
	case "386":
		return "x86"
	}
	return d.GOARCH
}

func (d *Detector) osInfo() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.osv == "" {
		d.osv = d.describeOS()
	}
	return d.osv
}

func (d *Detector) describeOS() string {
	switch d.GOOS {
	case "windows":
		if v := windowsVersion(); v != "" {
			return "Windows " + v
		}
		return "Windows"
	case "darwin":
		if out, code, err := d.run("/usr/bin/sw_vers", "-productVersion"); err == nil && code == 0 {
			if v := strings.TrimSpace(string(out)); v != "" {
				return "Mac OS " + v
			}
		}
		return "Mac OS"
	case "linux":
		b, _ := os.ReadFile("/etc/os-release")
		return LinuxName(string(b))
	}
	return d.GOOS
}

var linuxNames = map[string]string{
	"ubuntu": "Ubuntu", "debian": "Debian", "fedora": "Fedora", "arch": "Arch Linux", "alpine": "Alpine Linux",
	"centos": "CentOS", "rhel": "Red Hat Enterprise Linux", "opensuse-leap": "openSUSE", "opensuse-tumbleweed": "openSUSE",
	"linuxmint": "Linux Mint", "manjaro": "Manjaro", "nixos": "NixOS", "amzn": "Amazon Linux", "rocky": "Rocky Linux",
	"almalinux": "AlmaLinux", "gentoo": "Gentoo Linux", "pop": "Pop!_OS",
}

func LinuxName(osRelease string) string {
	vals := map[string]string{}
	for _, line := range strings.Split(osRelease, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			vals[k] = strings.Trim(v, `"'`)
		}
	}
	name, ok := linuxNames[vals["ID"]]
	if !ok {
		name = "Linux"
	}
	if v := vals["VERSION_ID"]; v != "" {
		return name + " " + v
	}
	if vals["ID"] == "arch" || vals["BUILD_ID"] == "rolling" {
		return name + " Rolling Release"
	}
	return name
}

func Terminal(getenv func(string) string) string {
	t := "unknown"
	switch {
	case strings.TrimSpace(getenv("TERM_PROGRAM")) != "":
		t = getenv("TERM_PROGRAM")
		if v := strings.TrimSpace(getenv("TERM_PROGRAM_VERSION")); v != "" {
			t += "/" + v
		}
	case getenv("WEZTERM_VERSION") != "":
		t = "WezTerm/" + getenv("WEZTERM_VERSION")
	case getenv("KITTY_WINDOW_ID") != "" || strings.Contains(getenv("TERM"), "kitty"):
		t = "kitty"
	case getenv("ALACRITTY_SOCKET") != "" || getenv("TERM") == "alacritty":
		t = "Alacritty"
	case getenv("KONSOLE_VERSION") != "":
		t = "Konsole/" + getenv("KONSOLE_VERSION")
	case getenv("GNOME_TERMINAL_SCREEN") != "":
		t = "gnome-terminal"
	case getenv("VTE_VERSION") != "":
		t = "VTE/" + getenv("VTE_VERSION")
	case getenv("WT_SESSION") != "":
		t = "WindowsTerminal"
	case strings.TrimSpace(getenv("TERM")) != "":
		t = getenv("TERM")
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./", r) {
			return r
		}
		return '_'
	}, strings.TrimSpace(t))
}
