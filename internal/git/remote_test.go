package git

import (
	"slices"
	"testing"
)

func TestPushSetsUpstreamWhenMissing(t *testing.T) {
	cases := []struct {
		name     string
		branch   string
		upstream string
		remotes  []string
		want     []string
		wantErr  bool
	}{
		{"tracking", "main", "origin/main", []string{"origin"}, []string{"push"}, false},
		{"no upstream prefers origin", "feat/x", "", []string{"fork", "origin"}, []string{"push", "-u", "origin", "feat/x"}, false},
		{"no upstream, other remote", "feat/x", "", []string{"fork"}, []string{"push", "-u", "fork", "feat/x"}, false},
		{"no remote", "feat/x", "", nil, nil, true},
		{"detached", "(detached)", "", []string{"origin"}, nil, true},
		{"unknown branch", "", "", []string{"origin"}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := pushArgs(c.branch, c.upstream, c.remotes)
			if (err != nil) != c.wantErr {
				t.Fatalf("pushArgs err = %v, wantErr %v", err, c.wantErr)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("pushArgs = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRemoteArgs(t *testing.T) {
	if got := fetchArgs(); !slices.Equal(got, []string{"fetch", "--prune"}) {
		t.Errorf("fetchArgs = %q", got)
	}
	if got := pullArgs(); !slices.Equal(got, []string{"pull", "--ff-only"}) {
		t.Errorf("pullArgs = %q", got)
	}
}

func TestSwitchNotCheckout(t *testing.T) {
	if got := switchArgs("feat/x"); !slices.Equal(got, []string{"switch", "feat/x"}) {
		t.Errorf("switchArgs = %q", got)
	}
	if got := createBranchArgs("feat/y"); !slices.Equal(got, []string{"switch", "-c", "feat/y"}) {
		t.Errorf("createBranchArgs = %q", got)
	}
	if got := deleteBranchArgs("feat/y"); !slices.Equal(got, []string{"branch", "-d", "feat/y"}) {
		t.Errorf("deleteBranchArgs = %q", got)
	}
}
