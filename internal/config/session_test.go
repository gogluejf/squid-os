package config

import "testing"

func TestNewSessionDocKeepsInitialConfigIndependent(t *testing.T) {
	cfg := SessionConfig{
		Tools:  []string{"read"},
		Skills: []string{"review"},
		Agents: []string{"researcher"},
	}
	doc := NewSessionDoc(cfg)

	doc.Config.Tools[0] = "bash"
	doc.Config.Skills[0] = "plan"
	doc.Config.Agents[0] = "coder"

	if doc.Initial.Tools[0] != "read" || doc.Initial.Skills[0] != "review" || doc.Initial.Agents[0] != "researcher" {
		t.Fatalf("initial config mutated with current config: %+v", doc.Initial)
	}
}

func TestParseAuthorizationMode(t *testing.T) {
	for _, value := range []AuthorizationMode{AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll, AuthorizationEndOnWrite, AuthorizationEndOnAll} {
		got, err := ParseAuthorizationMode(string(value))
		if err != nil || got != value {
			t.Fatalf("value=%q got=%q err=%v", value, got, err)
		}
	}
	if _, err := ParseAuthorizationMode("wrong"); err == nil {
		t.Fatal("expected invalid mode error")
	}
}
