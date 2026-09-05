package auth

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword("correct horse battery staple", hash); err != nil || !ok {
		t.Fatalf("valid password: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword("wrong", hash); err != nil || ok {
		t.Fatalf("wrong password: ok=%v err=%v", ok, err)
	}
	if hash == "" || hash[:10] != "$argon2id$" {
		t.Fatalf("hash format = %q", hash)
	}
}
