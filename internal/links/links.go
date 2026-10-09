package links

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var useSymlink = runtime.GOOS != "windows"

func path(exe, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), name)
}

func same(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
}

func current(exe, dst string) bool {
	if !useSymlink {
		return same(exe, dst)
	}
	target, err := os.Readlink(dst)
	return err == nil && target == filepath.Base(exe)
}

func Link(exe string, names []string) error {
	for _, n := range names {
		dst := path(exe, n)
		if current(exe, dst) {
			continue
		}
		if fi, err := os.Lstat(dst); err == nil && fi.IsDir() {
			return &os.PathError{Op: "link", Path: dst, Err: errors.New("is a directory")}
		}
		tmp := dst + ".new"
		os.Remove(tmp)
		var err error
		if useSymlink {
			err = os.Symlink(filepath.Base(exe), tmp)
		} else {
			err = os.Link(exe, tmp)
		}
		if err == nil {
			err = os.Rename(tmp, dst)
		}
		if err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return nil
}

func Target(exe string) (string, bool) {
	dst := path(exe, "agentswap")
	if strings.EqualFold(filepath.Clean(dst), filepath.Clean(exe)) || !same(exe, dst) {
		return "", false
	}
	return dst, true
}

func Unlink(exe string, names []string) ([]string, error) {
	var removed []string
	for _, n := range names {
		dst := path(exe, n)
		if !same(exe, dst) {
			continue
		}
		if err := os.Remove(dst); err != nil {
			return removed, err
		}
		removed = append(removed, n)
	}
	return removed, nil
}
