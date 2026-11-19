//go:build !windows

package config

import (
	"errors"
	"strings"
)

const dpapiPrefix = "dpapi:"

func protectSecret(plain string) (string, error) {
	return plain, nil
}

func unprotectSecret(stored string) (string, error) {
	if strings.HasPrefix(stored, dpapiPrefix) {
		return "", errors.New("DPAPI secret decryption requires Windows")
	}
	return stored, nil
}
