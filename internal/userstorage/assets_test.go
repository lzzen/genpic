package userstorage

import "testing"

func TestThumbKeyForOutput(t *testing.T) {
	k := "users/u1/jobs/j1/0.png"
	want := "users/u1/jobs/j1/0_thumb.jpg"
	if got := ThumbKeyForOutput(k); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if ThumbKeyForOutput("users/u1/refs/x.png") != "" {
		t.Fatal("refs should not get thumb key")
	}
}
