package images

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledIncludesTheStacks(t *testing.T) {
	names, err := Bundled()
	if err != nil {
		t.Fatalf("Bundled: %v", err)
	}
	want := map[string]bool{"base": true, "php": true, "node": true, "go": true}
	for _, n := range names {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("missing bundled images: %v (got %v)", want, names)
	}
}

func TestSeedWritesDefaultsOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "images")

	if err := Seed(dir); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if !Exists(dir, "php") {
		t.Fatal("php was not seeded")
	}

	// An existing library is left alone, including a deliberate removal.
	if err := os.RemoveAll(filepath.Join(dir, "php")); err != nil {
		t.Fatal(err)
	}
	if err := Seed(dir); err != nil {
		t.Fatalf("second Seed: %v", err)
	}
	if Exists(dir, "php") {
		t.Error("Seed must not resurrect a removed image")
	}
}

func TestResetRestoresLocalEdits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "images")
	if err := Seed(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "php", "Dockerfile")
	if err := os.WriteFile(path, []byte("FROM broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Reset(dir, "php"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "broken") {
		t.Error("the local edit survived the reset")
	}
	if !strings.Contains(string(data), "smate/base") {
		t.Errorf("the default does not look restored:\n%s", data)
	}
}

func TestResetRejectsUnknownImage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "images")
	if err := Seed(dir); err != nil {
		t.Fatal(err)
	}
	if err := Reset(dir, "cobol"); err == nil {
		t.Error("resetting an image that is not bundled should be an error")
	}
}

func TestListAndExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "images")
	if err := Seed(dir); err != nil {
		t.Fatal(err)
	}
	// A locally added image is part of the library too.
	local := filepath.Join(dir, "rust")
	if err := os.MkdirAll(local, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "Dockerfile"), []byte("FROM rust\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	names, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 5 || names[0] != "base" {
		t.Errorf("library: got %v", names)
	}
	if !Exists(dir, "rust") {
		t.Error("a locally added image should be visible")
	}
	if Exists(dir, "cobol") || Exists(dir, "") {
		t.Error("Exists must not report images that are absent")
	}
}

func TestBaseOf(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "images")
	if err := Seed(dir); err != nil {
		t.Fatal(err)
	}
	if base, ok := BaseOf(dir, "php"); !ok || base != "base" {
		t.Errorf("php: got %q, %v; want base, true", base, ok)
	}
	if _, ok := BaseOf(dir, "base"); ok {
		t.Error("base builds on ubuntu, not on a library image")
	}
	if _, ok := BaseOf(dir, "cobol"); ok {
		t.Error("a missing image has no base")
	}
}

func TestTag(t *testing.T) {
	if got := Tag("php"); got != "smate/php:latest" {
		t.Errorf("got %q, want smate/php:latest", got)
	}
}
