package codex

import (
	"fmt"
	"strconv"
	"strings"
)

type tomlError int

func (e tomlError) Error() string {
	return fmt.Sprintf("config.toml: cannot parse near byte %d", int(e))
}

func topLevel(config []byte, key string) (string, error) {
	s := string(config)
	i := 0
	for {
		i = skipBlank(s, i)
		if i >= len(s) || s[i] == '[' {
			return "", nil
		}
		path, j, err := tomlKey(s, i)
		if err != nil {
			return "", err
		}
		j = skipSpace(s, j)
		if j >= len(s) || s[j] != '=' {
			return "", tomlError(j)
		}
		val, k, err := tomlValue(s, skipSpace(s, j+1))
		if err != nil {
			return "", err
		}
		k = skipSpace(s, k)
		if k < len(s) && s[k] == '#' {
			k = lineEnd(s, k)
		}
		if k < len(s) && s[k] != '\n' && !strings.HasPrefix(s[k:], "\r\n") {
			return "", tomlError(k)
		}
		if len(path) == 1 && path[0] == key {
			return val, nil
		}
		i = k
	}
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func lineEnd(s string, i int) int {
	if n := strings.IndexByte(s[i:], '\n'); n >= 0 {
		return i + n
	}
	return len(s)
}

func skipBlank(s string, i int) int {
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '#':
			i = lineEnd(s, i)
		default:
			return i
		}
	}
	return i
}

func bareKeyByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

func tomlKey(s string, i int) ([]string, int, error) {
	var path []string
	for {
		i = skipSpace(s, i)
		var part string
		var err error
		switch {
		case i < len(s) && (s[i] == '"' || s[i] == '\''):
			part, i, err = tomlString(s, i)
			if err != nil {
				return nil, i, err
			}
		default:
			j := i
			for j < len(s) && bareKeyByte(s[j]) {
				j++
			}
			if j == i {
				return nil, i, tomlError(i)
			}
			part, i = s[i:j], j
		}
		path = append(path, part)
		i = skipSpace(s, i)
		if i >= len(s) || s[i] != '.' {
			return path, i, nil
		}
		i++
	}
}

func tomlString(s string, i int) (string, int, error) {
	q := s[i]
	if strings.HasPrefix(s[i:], strings.Repeat(string(q), 3)) {
		j := i + 3
		for j < len(s) {
			if q == '"' && s[j] == '\\' {
				j += 2
				continue
			}
			if strings.HasPrefix(s[j:], strings.Repeat(string(q), 3)) {
				end := j + 3
				for n := 0; n < 2 && end < len(s) && s[end] == q; n++ {
					end++
				}
				body := strings.TrimPrefix(strings.TrimPrefix(s[i+3:end-3], "\r"), "\n")
				return body, end, nil
			}
			j++
		}
		return "", j, tomlError(i)
	}
	j := i + 1
	for j < len(s) && s[j] != q && s[j] != '\n' {
		if q == '"' && s[j] == '\\' {
			j++
		}
		j++
	}
	if j >= len(s) || s[j] != q {
		return "", j, tomlError(i)
	}
	body := s[i+1 : j]
	if q == '"' {
		if v, err := strconv.Unquote(`"` + body + `"`); err == nil {
			body = v
		}
	}
	return body, j + 1, nil
}

func tomlValue(s string, i int) (string, int, error) {
	if i >= len(s) {
		return "", i, tomlError(i)
	}
	switch s[i] {
	case '"', '\'':
		return tomlString(s, i)
	case '[', '{':
		start, depth := i, 0
		for i < len(s) {
			switch s[i] {
			case '"', '\'':
				_, j, err := tomlString(s, i)
				if err != nil {
					return "", j, err
				}
				i = j
				continue
			case '#':
				i = lineEnd(s, i)
				continue
			case '[', '{':
				depth++
			case ']', '}':
				depth--
				if depth == 0 {
					return s[start : i+1], i + 1, nil
				}
			}
			i++
		}
		return "", i, tomlError(start)
	}
	j := i
	for j < len(s) && !strings.ContainsRune(" \t\r\n#", rune(s[j])) {
		j++
	}
	if j-i == 10 && s[i+4] == '-' && s[i+7] == '-' && j+1 < len(s) && s[j] == ' ' && s[j+1] >= '0' && s[j+1] <= '9' {
		for j++; j < len(s) && !strings.ContainsRune(" \t\r\n#", rune(s[j])); j++ {
		}
	}
	if j == i {
		return "", i, tomlError(i)
	}
	return s[i:j], j, nil
}
