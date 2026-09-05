package domain

import (
	"encoding/hex"
	"fmt"
	"strings"
)

func NormalizeUUID(value string) (string, error) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", fmt.Errorf("%w: value must be a canonical UUID", ErrInvalid)
	}
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return "", fmt.Errorf("%w: value must be a canonical UUID", ErrInvalid)
	}
	if _, err := hex.DecodeString(compact); err != nil {
		return "", fmt.Errorf("%w: malformed UUID", ErrInvalid)
	}
	return strings.ToLower(value), nil
}
