package git

import (
	"context"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want Status
	}{
		{
			name: "clean tracking branch",
			out: "# branch.oid 1a2b3c4d5e6f\n" +
				"# branch.head main\n" +
				"# branch.upstream origin/main\n" +
				"# branch.ab +0 -0\n",
			want: Status{Branch: "main", OID: "1a2b3c4d5e6f", HasUpstream: true},
		},
		{
			name: "dirty and diverged",
			out: "# branch.oid 1a2b3c4d5e6f\n" +
				"# branch.head feat/x\n" +
				"# branch.upstream origin/feat/x\n" +
				"# branch.ab +1 -2\n" +
				"1 .M N... 100644 100644 100644 aaa bbb internal/render/render.go\n",
			want: Status{Branch: "feat/x", OID: "1a2b3c4d5e6f", HasUpstream: true, Ahead: 1, Behind: 2, Unstaged: 1},
		},
		{
			name: "counts by XY",
			out: "# branch.oid 1a2b3c4d5e6f\n# branch.head main\n" +
				"1 M. N... 100644 100644 100644 aaa bbb staged.go\n" +
				"1 .M N... 100644 100644 100644 aaa bbb unstaged.go\n" +
				"1 MM N... 100644 100644 100644 aaa bbb both.go\n" +
				"2 R. N... 100644 100644 100644 aaa bbb R100 new.go\told.go\n" +
				"u UU N... 100644 100644 100644 100644 aaa bbb ccc conflict.go\n",
			// both.go는 staged·unstaged 양쪽에 잡히고, conflict는 XY를 보지 않는다.
			want: Status{Branch: "main", OID: "1a2b3c4d5e6f", Staged: 3, Unstaged: 2, Conflicted: 1},
		},
		{
			// 업스트림 없는 새 브랜치: branch.upstream/branch.ab 줄이 아예 없다.
			name: "no upstream",
			out:  "# branch.oid 1a2b3c4d5e6f\n# branch.head main\n",
			want: Status{Branch: "main", OID: "1a2b3c4d5e6f"},
		},
		{
			name: "detached",
			out:  "# branch.oid 1a2b3c4d5e6f\n# branch.head (detached)\n",
			want: Status{Branch: "(detached)", Detached: true, OID: "1a2b3c4d5e6f"},
		},
		{
			name: "initial commit",
			out:  "# branch.oid (initial)\n# branch.head main\n",
			want: Status{Branch: "main"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Parse(c.out); got != c.want {
				t.Errorf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestReadNotARepo(t *testing.T) {
	if _, ok := Read(context.Background(), t.TempDir()); ok {
		t.Error("expected ok=false outside a repo")
	}
	if _, ok := Read(context.Background(), ""); ok {
		t.Error("expected ok=false for an empty dir")
	}
}
