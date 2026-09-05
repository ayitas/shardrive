package domain

import (
	"errors"
	"testing"
)

func TestNormalizeUUID(t *testing.T) {
	got, err := NormalizeUUID("01234567-89AB-CDEF-0123-456789ABCDEF")
	if err != nil {
		t.Fatalf("NormalizeUUID(): %v", err)
	}
	if got != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Fatalf("normalized UUID = %q", got)
	}
	for _, value := range []string{"", "not-a-uuid", "../unsafe", "01234567-89ab-cdef-0123-456789abcdeg"} {
		if _, err := NormalizeUUID(value); !errors.Is(err, ErrInvalid) {
			t.Errorf("NormalizeUUID(%q) error = %v, want ErrInvalid", value, err)
		}
	}
}
