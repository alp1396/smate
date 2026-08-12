package store

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Harness is how one agent CLI authenticates and where it keeps its state.
//
// State is a directory kept between tasks and mounted at Mount inside the
// container — that is how a CLI that logs in interactively stays logged in. Env
// names the variables it needs from env.yml, Set holds literal values, Cmd is
// how it is started by hand (default: the harness's own name). A harness may use
// all of them at once.
type Harness struct {
	State string            `yaml:"state,omitempty"`
	Mount string            `yaml:"mount,omitempty"`
	Env   []string          `yaml:"env,omitempty"`
	Set   map[string]string `yaml:"set,omitempty"`
	Cmd   string            `yaml:"cmd,omitempty"`
}

// Cache is a host directory mounted into every task container so a build cache
// survives the fresh snapshot. Host defaults to ~/.smate/cache/<name>; set it to
// reuse a cache the machine already has, e.g. the real ~/go/pkg/mod.
type Cache struct {
	Host  string `yaml:"host,omitempty"`
	Mount string `yaml:"mount,omitempty"`
}

// Limits are the resource caps put on every task container, in docker's own
// syntax and passed through untouched. They live here rather than in .smate.yml
// because capacity is a property of the machine, not of a project.
type Limits struct {
	CPUs   string `yaml:"cpus,omitempty"`
	Memory string `yaml:"memory,omitempty"`
	PIDs   int    `yaml:"pids,omitempty"`
}

type Global struct {
	Harness map[string]Harness `yaml:"harness"`
	Cache   map[string]Cache   `yaml:"cache,omitempty"`
	Limits  Limits             `yaml:"limits"`
}

// defaultLimits are tight on purpose: a dependency build hits the memory cap and
// comes out as CUT OFF rather than swapping the host to death.
var defaultLimits = Limits{CPUs: "1", Memory: "512m", PIDs: 512}

func (l Limits) WithDefaults() Limits {
	if l.CPUs == "" {
		l.CPUs = defaultLimits.CPUs
	}
	if l.Memory == "" {
		l.Memory = defaultLimits.Memory
	}
	if l.PIDs == 0 {
		l.PIDs = defaultLimits.PIDs
	}
	return l
}

// defaultGlobal is written on first use so there is something to edit.
//
// A single state mount only covers a CLI that can be told to keep everything in
// one directory: Claude Code splits state between ~/.claude and ~/.claude.json
// unless CLAUDE_CONFIG_DIR says otherwise. Mounts sit under the task user's home
// rather than /root, which is 0700 and not ours.
var defaultGlobal = Global{
	Harness: map[string]Harness{
		"claude": {
			State: "claude",
			Mount: containerHome + "/.claude",
			Set:   map[string]string{"CLAUDE_CONFIG_DIR": containerHome + "/.claude"},
		},
		"codex": {
			State: "codex",
			Mount: containerHome + "/.codex",
			Env:   []string{"OPENAI_API_KEY"},
		},
		"opencode": {
			State: "opencode",
			Mount: containerHome + "/.local/share/opencode",
			Env:   []string{"OPENROUTER_API_KEY"},
		},
	},
	Limits: defaultLimits,
}

const containerHome = "/home/smate"

func (s *Store) ConfigPath() string { return filepath.Join(s.root, "config.yml") }

// EnvPath is ~/.smate/env.yml, the file holding the actual key values.
func (s *Store) EnvPath() string { return filepath.Join(s.root, "env.yml") }

func (s *Store) HarnessDir(name string) string { return filepath.Join(s.root, "harness", name) }

func (s *Store) CacheDir(name string) string { return filepath.Join(s.root, "cache", name) }

//go:embed defaults
var harnessDefaults embed.FS

// SeedHarnessState writes the settings a harness needs to work unattended and
// returns what it wrote. Without them an agent CLI stops at the first permission
// prompt with nobody there to answer.
//
// Only missing files are written, so edits survive; to refuse a default, leave an
// empty file in its place rather than deleting it. The deny list inside guards
// against habit, not intent — it matches command prefixes. The boundary is the
// container.
func (s *Store) SeedHarnessState(name, dir string) ([]string, error) {
	entries, err := harnessDefaults.ReadDir("defaults/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // nothing bundled for this harness
	}
	if err != nil {
		return nil, fmt.Errorf("read bundled settings of harness %s: %w", name, err)
	}
	var written []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		target := filepath.Join(dir, e.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("check %s: %w", target, err)
		}
		data, err := harnessDefaults.ReadFile("defaults/" + name + "/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read bundled %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", target, err)
		}
		written = append(written, target)
	}
	return written, nil
}

// LoadGlobal reads config.yml, writing the defaults first if it is missing.
func (s *Store) LoadGlobal() (Global, error) {
	data, err := os.ReadFile(s.ConfigPath())
	if errors.Is(err, fs.ErrNotExist) {
		if err := s.SaveGlobal(defaultGlobal); err != nil {
			return Global{}, err
		}
		return defaultGlobal, nil
	}
	if err != nil {
		return Global{}, fmt.Errorf("read config.yml: %w", err)
	}
	var g Global
	if err := yaml.Unmarshal(data, &g); err != nil {
		return Global{}, fmt.Errorf("parse config.yml: %w", err)
	}
	g.Limits = g.Limits.WithDefaults()
	return g, nil
}

func (s *Store) SaveGlobal(g Global) error {
	data, err := yaml.Marshal(g)
	if err != nil {
		return fmt.Errorf("encode config.yml: %w", err)
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", s.root, err)
	}
	if err := os.WriteFile(s.ConfigPath(), data, 0o600); err != nil {
		return fmt.Errorf("write config.yml: %w", err)
	}
	return nil
}

// LoadEnv reads env.yml. A missing file is an empty set, not an error.
func (s *Store) LoadEnv() (map[string]string, error) {
	data, err := os.ReadFile(s.EnvPath())
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read env.yml: %w", err)
	}
	values := map[string]string{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("parse env.yml: %w", err)
	}
	return values, nil
}

// SaveEnv writes env.yml with owner-only permissions. This file holds the keys.
func (s *Store) SaveEnv(values map[string]string) error {
	data, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode env.yml: %w", err)
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", s.root, err)
	}
	if err := os.WriteFile(s.EnvPath(), data, 0o600); err != nil {
		return fmt.Errorf("write env.yml: %w", err)
	}
	return nil
}
