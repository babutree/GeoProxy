package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
)

func VerifyPasswordHash(base string, password string, expectedBase string, expectedHash string) bool {
	baseMatch := subtle.ConstantTimeCompare([]byte(base), []byte(expectedBase)) == 1
	passwordHash := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))
	passwordMatch := subtle.ConstantTimeCompare([]byte(passwordHash), []byte(expectedHash)) == 1
	return baseMatch && passwordMatch
}

func VerifyPlainPassword(base string, password string, expectedBase string, expectedPassword string) bool {
	baseMatch := subtle.ConstantTimeCompare([]byte(base), []byte(expectedBase)) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) == 1
	return baseMatch && passwordMatch
}
