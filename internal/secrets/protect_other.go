//go:build !windows

package secrets

// Without DPAPI the file's 0600 permissions are the protection.
func protect(plain []byte) ([]byte, error) { return plain, nil }

func unprotect(sealed []byte) ([]byte, error) { return sealed, nil }
