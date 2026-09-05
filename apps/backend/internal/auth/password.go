package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory     = 64 * 1024
	argonTime       = 3
	argonThreads    = 2
	argonKeyLength  = 32
	argonSaltLength = 16
)

// HashPassword returns a PHC-style Argon2id hash suitable for storage.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLength)
	encode := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, encode.EncodeToString(salt), encode.EncodeToString(hash)), nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("invalid password hash format")
	}
	var memory, time, threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil || memory == 0 || time == 0 || threads == 0 {
		return false, errors.New("invalid password hash parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false, errors.New("invalid password salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return false, errors.New("invalid password digest")
	}
	actual := argon2.IDKey([]byte(password), salt, time, memory, uint8(threads), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
