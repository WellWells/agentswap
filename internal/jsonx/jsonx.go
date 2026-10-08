package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
)

var errNotObject = errors.New("not a JSON object")

type Member struct {
	Key        string
	Val        json.RawMessage
	Start, End int
}

func Members(doc []byte) ([]Member, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errNotObject
	}
	var out []Member
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errNotObject
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		end := int(dec.InputOffset())
		out = append(out, Member{Key: key, Val: raw, Start: end - len(raw), End: end})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return out, nil
}

func Get(doc []byte, key string) (json.RawMessage, bool, error) {
	ms, err := Members(doc)
	if err != nil {
		return nil, false, err
	}
	for _, m := range ms {
		if m.Key == key {
			return m.Val, true, nil
		}
	}
	return nil, false, nil
}

func Set(doc []byte, key string, val []byte) ([]byte, error) {
	ms, err := Members(doc)
	if err != nil {
		return nil, err
	}
	splice := func(at, until int, ins []byte) []byte {
		out := make([]byte, 0, len(doc)+len(ins))
		out = append(out, doc[:at]...)
		out = append(out, ins...)
		return append(out, doc[until:]...)
	}
	for _, m := range ms {
		if m.Key == key {
			return splice(m.Start, m.End, val), nil
		}
	}
	k, _ := json.Marshal(key)
	pretty := bytes.Contains(doc, []byte("\n"))
	var ins []byte
	at := bytes.IndexByte(doc, '{') + 1
	if len(ms) > 0 {
		at = ms[len(ms)-1].End
		ins = append(ins, ',')
	}
	if pretty {
		ins = append(ins, "\n  "...)
	}
	ins = append(ins, k...)
	ins = append(ins, ':')
	if pretty {
		ins = append(ins, ' ')
	}
	ins = append(ins, val...)
	if pretty && len(ms) == 0 {
		ins = append(ins, '\n')
	}
	return splice(at, at, ins), nil
}

func Encode(ms []Member, pretty bool) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		if pretty {
			b.WriteString("\n  ")
		}
		k, _ := json.Marshal(m.Key)
		b.Write(k)
		b.WriteByte(':')
		if pretty {
			b.WriteByte(' ')
		}
		b.Write(m.Val)
	}
	if pretty && len(ms) > 0 {
		b.WriteByte('\n')
	}
	b.WriteByte('}')
	return b.Bytes()
}
