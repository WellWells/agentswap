package ui

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"
)

func ReadSecret(f *os.File) (string, error) {
	restore, err := noEcho(f)
	if err != nil {
		return "", err
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-sig:
			restore()
			os.Stderr.WriteString("\n")
			os.Exit(130)
		case <-done:
		}
	}()
	defer func() {
		signal.Stop(sig)
		close(done)
		restore()
	}()
	line, err := readLine(f)
	if errors.Is(err, io.EOF) {
		time.Sleep(300 * time.Millisecond)
	}
	return line, err
}

func readLine(r io.Reader) (string, error) {
	var line []byte
	var b [1]byte
	for {
		n, err := r.Read(b[:])
		if n > 0 {
			if b[0] == '\n' {
				break
			}
			line = append(line, b[0])
			continue
		}
		if errors.Is(err, io.EOF) && len(line) > 0 {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return strings.TrimRight(string(line), "\r"), nil
}
