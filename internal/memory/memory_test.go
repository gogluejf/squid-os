package memory

import (
	"path/filepath"
	"testing"
)

func TestResolvePaths(t *testing.T) {
	p := Paths{GlobalMemoryDir: "/global", AgentsDir: "/agents"}
	cases := []struct {
		ns              Namespace
		wd, agent, want string
	}{{NamespaceWorkspace, "/work", "", filepath.Join("/work", "memory")}, {NamespaceGlobal, "", "", "/global"}, {NamespaceAgent, "", "review", filepath.Join("/agents", "review", "memory")}}
	for _, c := range cases {
		got, err := ResolvePath(c.ns, c.wd, p, c.agent)
		if err != nil || got != c.want {
			t.Fatalf("got %q, %v want %q", got, err, c.want)
		}
	}
}
