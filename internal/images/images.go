// Package images owns the image library: the Dockerfiles kept in
// ~/.smate/images and the defaults shipped inside the binary.
package images

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed defaults
var defaults embed.FS

const defaultsRoot = "defaults"

const tagPrefix = "smate/"

func Tag(name string) string { return tagPrefix + name + ":latest" }

func Bundled() ([]string, error) {
	entries, err := defaults.ReadDir(defaultsRoot)
	if err != nil {
		return nil, fmt.Errorf("read bundled images: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func Seed(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check image library: %w", err)
	}
	names, err := Bundled()
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := writeDefault(dir, name); err != nil {
			return err
		}
	}
	return nil
}

func Reset(dir, name string) error {
	names, err := Bundled()
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == name {
			return writeDefault(dir, name)
		}
	}
	return fmt.Errorf("%s is not a bundled image (bundled: %s)", name, strings.Join(names, ", "))
}

func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read image library: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func Exists(dir, name string) bool {
	if name == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, name, "Dockerfile"))
	return err == nil && !info.IsDir()
}

// BaseOf returns the library image this one builds on, read from its first FROM
// line — so we can say what to build first instead of letting docker fail.
func BaseOf(dir, name string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, name, "Dockerfile"))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		ref := fields[1]
		if !strings.HasPrefix(ref, tagPrefix) {
			return "", false
		}
		return strings.TrimSuffix(strings.TrimPrefix(ref, tagPrefix), ":latest"), true
	}
	return "", false
}

func writeDefault(dir, name string) error {
	target := filepath.Join(dir, name)
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("clear %s: %w", target, err)
	}
	src := defaultsRoot + "/" + name
	return fs.WalkDir(defaults, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, src), "/")
		dst := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		data, err := defaults.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o600)
	})
}
