package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/ayitas/shardrive/apps/backend/internal/domain"
)

// NewObjectID returns an opaque RFC 4122 UUIDv4 without introducing a UUID
// library into the provider boundary.
func NewObjectID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate object ID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}

func NormalizeObjectID(value string) (string, error) {
	normalized, err := domain.NormalizeUUID(value)
	if err != nil {
		return "", fmt.Errorf("%w: object ID: %v", ErrInvalidRequest, err)
	}
	return normalized, nil
}
