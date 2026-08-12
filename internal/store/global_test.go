package store

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLoadGlobalSeedsDefaults(t *testing.T) {
	s := At(t.TempDir())

	cfg, err := s.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	for _, name := range []string{"claude", "codex", "opencode"} {
		if _, ok := cfg.Harness[name]; !ok {
			t.Errorf("default config has no %s", name)
		}
	}
	if cfg.Harness["claude"].Mount != "/home/smate/.claude" {
		t.Errorf("claude mount: %q", cfg.Harness["claude"].Mount)
	}
	if _, err := os.Stat(s.ConfigPath()); err != nil {
		t.Errorf("config.yml was not written: %v", err)
	}
	if cfg.Limits != defaultLimits {
		t.Errorf("default limits = %+v, want %+v", cfg.Limits, defaultLimits)
	}

	// A second read must return what is on disk, not the defaults again.
	cfg.Harness["claude"] = Harness{Env: []string{"ANTHROPIC_API_KEY"}}
	if err := s.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}
	again, err := s.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Harness["claude"].Env, []string{"ANTHROPIC_API_KEY"}) {
		t.Errorf("edits were lost: %+v", again.Harness["claude"])
	}
}

func TestSeedHarnessState(t *testing.T) {
	s := At(t.TempDir())
	dir := s.HarnessDir("claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	written, err := s.SeedHarnessState("claude", dir)
	if err != nil {
		t.Fatalf("SeedHarnessState: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("wrote %v, want one settings file", written)
	}
	data, err := os.ReadFile(written[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"bypassPermissions", "apt-get"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the defaults do not mention %q: %s", want, data)
		}
	}

	// Written once: what the user edits afterwards is theirs.
	if err := os.WriteFile(written[0], []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := s.SeedHarnessState("claude", dir)
	if err != nil || len(again) != 0 {
		t.Errorf("the defaults were rewritten: %v (%v)", again, err)
	}

	// A harness with nothing bundled is not an error.
	if got, err := s.SeedHarnessState("codex", dir); err != nil || got != nil {
		t.Errorf("codex: %v (%v)", got, err)
	}
}

// A config written before limits existed must still get them, and a partial
// section must only fill in what it left out.
func TestLimitsFillInTheMissingFields(t *testing.T) {
	s := At(t.TempDir())
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ConfigPath(), []byte("harness:\n  claude: {}\nlimits:\n  memory: 4g\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.Memory != "4g" {
		t.Errorf("memory = %q, want the configured 4g", cfg.Limits.Memory)
	}
	if cfg.Limits.CPUs != defaultLimits.CPUs || cfg.Limits.PIDs != defaultLimits.PIDs {
		t.Errorf("unset fields were not defaulted: %+v", cfg.Limits)
	}
}

func TestEnvRoundTripAndPermissions(t *testing.T) {
	s := At(t.TempDir())

	values, err := s.LoadEnv()
	if err != nil {
		t.Fatalf("LoadEnv on a missing file: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("a missing env.yml must read as empty, got %v", values)
	}

	values["OPENAI_API_KEY"] = "sk-test-value"
	if err := s.SaveEnv(values); err != nil {
		t.Fatalf("SaveEnv: %v", err)
	}
	got, err := s.LoadEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got["OPENAI_API_KEY"] != "sk-test-value" {
		t.Errorf("round trip: %v", got)
	}

	// The file holds keys, so nobody else may read it.
	info, err := os.Stat(s.EnvPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("env.yml permissions: got %o, want 600", perm)
	}
}
