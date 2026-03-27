package main

import "testing"

func TestIsRepoSpec(t *testing.T) {
	cases := []struct {
		spec string
		want bool
	}{
		{spec: "file:///tmp/repo.git", want: true},
		{spec: "https://host/org/repo.git", want: true},
		{spec: "ssh://git@host/org/repo.git", want: true},
		{spec: "git@host:org/repo.git", want: true},
		{spec: "/tmp/repo", want: false},
		{spec: "relative/repo", want: false},
		{spec: "", want: false},
	}
	for _, tc := range cases {
		if got := isRepoSpec(tc.spec); got != tc.want {
			t.Fatalf("isRepoSpec(%q) = %v, want %v", tc.spec, got, tc.want)
		}
	}
}
