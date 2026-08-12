package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	repo := t.TempDir()
	want := Config{Image: "smate-dev:latest", Secrets: []string{"secret.txt", "creds"}, Mounts: []string{"secret.env:/run/secret.env"}}

	if err := SaveConfig(repo, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, ok, err := LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !ok {
		t.Fatal("the config should have been reported as existing")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip differs:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	got, ok, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("a missing config must not be an error: %v", err)
	}
	if ok {
		t.Error("a missing config must be reported as absent")
	}
	if got.Image != "" || got.Secrets != nil || got.Mounts != nil {
		t.Errorf("expected an empty config, got %+v", got)
	}
}

func TestLoadConfigBroken(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ConfigName), []byte("image: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadConfig(repo); err == nil {
		t.Error("a broken config must be an error rather than an empty one")
	}
}
