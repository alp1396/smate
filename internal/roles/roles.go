// Package roles owns the role library: the definitions in ~/.smate/roles and the
// defaults shipped inside the binary. A role is a described performer — which
// harness runs it, with what command, what it reads and what it must leave
// behind.
package roles

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed defaults
var defaults embed.FS

const defaultsRoot = "defaults"

// ConfigName is the role definition inside a role's directory.
const ConfigName = "role.yml"

// AgentsName is the instruction file copied into the sandbox on every run.
const AgentsName = "AGENTS.md"

type Role struct {
	Name string `yaml:"-"`
	// Order is the order of the work, not of the alphabet. The bundled roles
	// are 10, 20, 30, leaving room to slot one of your own between them.
	Order   int    `yaml:"order,omitempty"`
	Harness string `yaml:"harness"`
	// Model and Effort are values, not flags: how they reach the CLI is the
	// harness's business (model_flag, effort_flag in config.yml). Both are
	// optional — left out, the harness runs on whatever it defaults to.
	Model   string   `yaml:"model,omitempty"`
	Effort  string   `yaml:"effort,omitempty"`
	Inputs  []string `yaml:"inputs,omitempty"`
	Outputs []string `yaml:"outputs"`
	// Cmd is the whole command line roles used to carry. It is still read so a
	// file left from then can be told where its model went, and is never run.
	Cmd string `yaml:"cmd,omitempty"`
}

var ErrNotFound = errors.New("role not found")

func Bundled() ([]string, error) {
	entries, err := defaults.ReadDir(defaultsRoot)
	if err != nil {
		return nil, fmt.Errorf("read bundled roles: %w", err)
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
		return fmt.Errorf("check role library: %w", err)
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
	return fmt.Errorf("%s is not a bundled role (bundled: %s)", name, strings.Join(names, ", "))
}

func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read role library: %w", err)
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

func AgentsPath(dir, name string) string { return filepath.Join(dir, name, AgentsName) }

func Load(dir, name string) (Role, error) {
	if name == "" {
		return Role{}, fmt.Errorf("no role name given")
	}
	path := filepath.Join(dir, name, ConfigName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Role{}, fmt.Errorf("%s: %w (%s)", name, ErrNotFound, dir)
	}
	if err != nil {
		return Role{}, fmt.Errorf("read %s: %w", path, err)
	}
	var r Role
	if err := yaml.Unmarshal(data, &r); err != nil {
		return Role{}, fmt.Errorf("parse %s: %w", path, err)
	}
	r.Name = name
	if err := r.validate(); err != nil {
		return Role{}, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

func LoadAll(dir string) ([]Role, error) {
	names, err := List(dir)
	if err != nil {
		return nil, err
	}
	var out []Role
	for _, name := range names {
		r, err := Load(dir, name)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return before(out[i], out[j]) })
	return out, nil
}

// before is the one place role order is decided. An unnumbered role goes last:
// zero means nobody said where it belongs, and putting it first would push the
// roles somebody did arrange down the list.
func before(a, b Role) bool {
	switch {
	case a.Order != 0 && b.Order != 0:
		if a.Order != b.Order {
			return a.Order < b.Order
		}
	case a.Order != 0:
		return true
	case b.Order != 0:
		return false
	}
	return a.Name < b.Name
}

func (r Role) validate() error {
	if strings.TrimSpace(r.Harness) == "" {
		return fmt.Errorf("harness is empty — set it to a name from ~/.smate/config.yml")
	}
	if strings.TrimSpace(r.Cmd) != "" {
		return fmt.Errorf("cmd is no longer read — the harness builds the command line; move the model into model: and the reasoning effort into effort:, and drop cmd")
	}
	// A singular `output:` key lands here too: it is left unread, so the list
	// comes out empty and the message has to name the key it wants.
	if len(r.Outputs) == 0 {
		return fmt.Errorf("outputs is empty — list the artefacts the role must leave behind, e.g. outputs: [result.md]")
	}
	for _, out := range r.Outputs {
		if err := validArtefact("outputs", out); err != nil {
			return err
		}
	}
	for _, in := range r.Inputs {
		if err := validArtefact("inputs", in); err != nil {
			return err
		}
		// The outputs are deleted just before the harness starts, so a role that
		// waits for one of its own outputs would never be runnable — and the
		// reason would be invisible.
		for _, out := range r.Outputs {
			if in == out {
				return fmt.Errorf("output %s is also listed in inputs, and it is deleted at the start of every run", out)
			}
		}
	}
	return nil
}

func validArtefact(field, name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("%s: empty artefact name", field)
	case name != filepath.Base(name) || strings.ContainsAny(name, `/\`):
		return fmt.Errorf("%s: %q must be a file name inside .smate/, without a path", field, name)
	case name == "." || name == "..":
		return fmt.Errorf("%s: %q is not a file name", field, name)
	}
	return nil
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
