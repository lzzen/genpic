package jobstore_test

import (
	"testing"

	"genpic/internal/jobstore"
)

func TestOwnerScope_ListContains(t *testing.T) {
	jUser := &jobstore.Job{UserID: "u1", SessionID: ""}
	jAnon := &jobstore.Job{UserID: "", SessionID: "s1"}
	jLegacy := &jobstore.Job{UserID: "", SessionID: ""}

	tests := []struct {
		name  string
		scope jobstore.OwnerScope
		job   *jobstore.Job
		want  bool
	}{
		{"user matches", jobstore.OwnerScope{UserID: "u1"}, jUser, true},
		{"user mismatch", jobstore.OwnerScope{UserID: "u2"}, jUser, false},
		{"session matches anon", jobstore.OwnerScope{SessionID: "s1"}, jAnon, true},
		{"session mismatch", jobstore.OwnerScope{SessionID: "s2"}, jAnon, false},
		{"session does not list user job", jobstore.OwnerScope{SessionID: "s1"}, jUser, false},
		{"legacy list empty scope", jobstore.OwnerScope{}, jLegacy, true},
		{"legacy hidden for session", jobstore.OwnerScope{SessionID: "s1"}, jLegacy, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.ListContains(tt.job); got != tt.want {
				t.Fatalf("ListContains = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOwnerScope_CanViewJob(t *testing.T) {
	jUser := &jobstore.Job{UserID: "u1", SessionID: ""}
	jAnon := &jobstore.Job{UserID: "", SessionID: "s1"}
	jLegacy := &jobstore.Job{UserID: "", SessionID: ""}

	var empty jobstore.OwnerScope
	if !empty.CanViewJob(jLegacy) {
		t.Fatal("empty scope should view legacy job")
	}
	if empty.CanViewJob(jAnon) {
		t.Fatal("empty scope must not view session job")
	}
	if !(jobstore.OwnerScope{SessionID: "s1"}).CanViewJob(jAnon) {
		t.Fatal("session scope should view own anon job")
	}
	if (jobstore.OwnerScope{UserID: "u1"}).CanViewJob(jAnon) {
		t.Fatal("user scope must not view anon-only job")
	}
	if !(jobstore.OwnerScope{UserID: "u1"}).CanViewJob(jUser) {
		t.Fatal("user scope should view own user job")
	}
}
