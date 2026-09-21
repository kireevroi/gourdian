//go:build windows

package secrets

import (
	"syscall"
	"unsafe"
)

var (
	crypt32             = syscall.NewLazyDLL("crypt32.dll")
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	pLocalFree          = kernel32.NewProc("LocalFree")
)

const cryptprotectUIForbidden = 0x1

type dataBlob struct {
	size uint32
	data *byte
}

func blob(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{size: uint32(len(b)), data: &b[0]}
}

func (d dataBlob) bytes() []byte {
	out := make([]byte, d.size)
	copy(out, unsafe.Slice(d.data, d.size))
	return out
}

func protect(plain []byte) ([]byte, error) {
	in, out := blob(plain), dataBlob{}
	if r, _, err := pCryptProtectData.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out))); r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	return out.bytes(), nil
}

func unprotect(sealed []byte) ([]byte, error) {
	in, out := blob(sealed), dataBlob{}
	if r, _, err := pCryptUnprotectData.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out))); r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	return out.bytes(), nil
}
