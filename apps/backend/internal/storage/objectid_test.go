package storage

import "testing"

func TestNewObjectIDProducesUniqueCanonicalUUIDv4(t *testing.T) {
	seen := make(map[string]struct{})
	for range 100 {
		id, err := NewObjectID()
		if err != nil {
			t.Fatalf("NewObjectID(): %v", err)
		}
		if normalized, err := NormalizeObjectID(id); err != nil || normalized != id {
			t.Fatalf("NormalizeObjectID(%q) = %q, %v", id, normalized, err)
		}
		if id[14] != '4' {
			t.Errorf("UUID %q is not version 4", id)
		}
		if id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b' {
			t.Errorf("UUID %q has invalid RFC 4122 variant", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate object ID %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNormalizeObjectIDRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{"", "../object", "/absolute", "not-a-uuid", "01234567-89ab-cdef-0123-456789abcdeg"} {
		if _, err := NormalizeObjectID(value); err == nil {
			t.Errorf("NormalizeObjectID(%q) error = nil", value)
		}
	}
}
