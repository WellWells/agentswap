package execx

import (
	"bytes"
	"errors"
	"os/exec"
)

type Runner func(stdin []byte, name string, args ...string) ([]byte, int, error)

func Run(stdin []byte, name string, args ...string) ([]byte, int, error) {
	cmd := exec.Command(name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), ee.ExitCode(), nil
	}
	if err != nil {
		return nil, -1, err
	}
	return out.Bytes(), 0, nil
}
