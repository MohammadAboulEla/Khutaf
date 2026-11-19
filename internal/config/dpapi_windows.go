//go:build windows

package config

import (
	"encoding/base64"
	"errors"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const dpapiPrefix = "dpapi:"

func protectSecret(plain string) (string, error) {
	if plain == "" || strings.HasPrefix(plain, dpapiPrefix) {
		return plain, nil
	}
	inBytes := []byte(plain)
	inBlob := windows.DataBlob{
		Size: uint32(len(inBytes)),
		Data: &inBytes[0],
	}
	desc, err := windows.UTF16PtrFromString("Khutaf LLM API Key")
	if err != nil {
		return "", err
	}
	var outBlob windows.DataBlob
	err = windows.CryptProtectData(&inBlob, desc, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &outBlob)
	runtime.KeepAlive(inBytes)
	runtime.KeepAlive(desc)
	if err != nil {
		return "", err
	}
	if outBlob.Data == nil || outBlob.Size == 0 {
		return "", errors.New("CryptProtectData returned empty output")
	}
	defer func() {
		_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))
	}()

	cipher := make([]byte, outBlob.Size)
	copy(cipher, unsafe.Slice(outBlob.Data, outBlob.Size))
	return dpapiPrefix + base64.StdEncoding.EncodeToString(cipher), nil
}

func unprotectSecret(stored string) (string, error) {
	if !strings.HasPrefix(stored, dpapiPrefix) {
		return stored, nil
	}
	encoded := strings.TrimPrefix(stored, dpapiPrefix)
	if encoded == "" {
		return "", errors.New("empty DPAPI secret")
	}
	cipher, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(cipher) == 0 {
		return "", errors.New("empty DPAPI ciphertext")
	}
	inBlob := windows.DataBlob{
		Size: uint32(len(cipher)),
		Data: &cipher[0],
	}
	var outBlob windows.DataBlob
	err = windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &outBlob)
	runtime.KeepAlive(cipher)
	if err != nil {
		return "", err
	}
	defer func() {
		if outBlob.Data != nil {
			_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))
		}
	}()
	if outBlob.Data == nil || outBlob.Size == 0 {
		return "", nil
	}
	plain := make([]byte, outBlob.Size)
	copy(plain, unsafe.Slice(outBlob.Data, outBlob.Size))
	return string(plain), nil
}
