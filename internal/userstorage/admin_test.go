package userstorage

import "testing"

func TestRemainingBytes(t *testing.T) {
	if RemainingBytes(100, 500) != 400 {
		t.Fatal()
	}
	if RemainingBytes(600, 500) != 0 {
		t.Fatal()
	}
	if RemainingBytes(0, 0) != 0 {
		t.Fatal()
	}
}
