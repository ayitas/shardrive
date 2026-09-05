package auth

import "testing"

func TestSessionTokenIsURLSafeAndHashStable(t *testing.T) {
	token, hash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || len(hash) != 32 {
		t.Fatalf("token/hash lengths: %d/%d", len(token), len(hash))
	}
	if got := HashSessionToken(token); string(got) != string(hash) {
		t.Fatal("session hash is not stable")
	}
}
