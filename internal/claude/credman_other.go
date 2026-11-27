//go:build !windows

package claude

import "errors"

var errNoCredman = errors.New("Windows Credential Manager is only available on Windows")

type noCredStore struct{}

func systemCredStore() CredStore { return noCredStore{} }

func (noCredStore) Get(string, string) ([]byte, error) { return nil, errNoCredman }

func (noCredStore) Set(string, string, []byte) error { return errNoCredman }

func (noCredStore) Delete(string, string) error { return errNoCredman }
