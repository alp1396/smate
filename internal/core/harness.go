package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"smate/internal/roles"
	"smate/internal/runtime"
	"smate/internal/store"
)

// HarnessInfo is one configured agent CLI as something to open by hand.
type HarnessInfo struct {
	Name    string
	Cmd     string   // the command line that starts it
	Missing []string // keys config.yml asks for that env.yml does not have
}

func Harnesses(s *store.Store) ([]HarnessInfo, error) {
	cfg, err := s.LoadGlobal()
	if err != nil {
		return nil, err
	}
	values, err := s.LoadEnv()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(cfg.Harness))
	for name := range cfg.Harness {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]HarnessInfo, 0, len(names))
	for _, name := range names {
		h := cfg.Harness[name]
		info := HarnessInfo{Name: name, Cmd: harnessCmdLine(name, h)}
		for _, key := range h.Env {
			if values[key] == "" {
				info.Missing = append(info.Missing, key)
			}
		}
		out = append(out, info)
	}
	return out, nil
}

func HarnessCmd(s *store.Store, id, name string) (*exec.Cmd, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return nil, err
	}
	if !runtime.Running(t.Container()) {
		return nil, fmt.Errorf("container of task %s is not running", t.ID)
	}
	cfg, err := s.LoadGlobal()
	if err != nil {
		return nil, err
	}
	h, ok := cfg.Harness[name]
	if !ok {
		return nil, fmt.Errorf("harness %q is not in %s", name, s.ConfigPath())
	}
	return runtime.ExecCmd(t.Container(), harnessCmdLine(name, h)), nil
}

func harnessCmdLine(name string, h store.Harness) string {
	if strings.TrimSpace(h.Cmd) != "" {
		return h.Cmd
	}
	return name
}

// flagPlaceholder is where the value goes in model_flag and effort_flag.
const flagPlaceholder = "{}"

// roleCmdLine builds the command a role is run through: the harness's own
// command line, then a fragment per value the role named. This is the only place
// the two halves meet — the role knows which model it wants, the harness knows
// how to spell it.
//
// A value the harness cannot express is a warning rather than a refusal: the run
// is still the right role in the right container, only on the CLI's default
// model, and the misconfiguration is in config.yml, not at the boundary.
func roleCmdLine(name string, h store.Harness, r roles.Role) (string, []string) {
	parts := []string{harnessCmdLine(name, h)}
	var warnings []string
	for _, f := range []struct{ field, value, tmpl string }{
		{"model", r.Model, h.ModelFlag},
		{"effort", r.Effort, h.EffortFlag},
	} {
		switch {
		case f.value == "":
		case strings.TrimSpace(f.tmpl) == "":
			warnings = append(warnings, fmt.Sprintf(
				"role %s asks for %s %s, but harness %s has no %s_flag — running on the default",
				r.Name, f.field, f.value, name, f.field))
		case !strings.Contains(f.tmpl, flagPlaceholder):
			warnings = append(warnings, fmt.Sprintf(
				"harness %s: %s_flag %q has no %s to put the value in — %s %s is dropped",
				name, f.field, f.tmpl, flagPlaceholder, f.field, f.value))
		default:
			parts = append(parts, strings.ReplaceAll(f.tmpl, flagPlaceholder, runtime.ShellQuote(f.value)))
		}
	}
	return strings.Join(parts, " "), warnings
}

type injection struct {
	Env      map[string]string
	Mounts   []runtime.Mount
	Limits   runtime.Limits
	Warnings []string
}

// harnessInjection collects environment, mounts and limits from config.yml.
// Everything configured goes into every container: with one user and one set of
// keys, handing them out per project would be a ritual rather than a boundary.
// A missing key warns rather than fails, and Set is applied before env.yml so a
// key can never be shadowed by a fixed setting.
func harnessInjection(s *store.Store) (injection, error) {
	inj := injection{Env: map[string]string{}}

	cfg, err := s.LoadGlobal()
	if err != nil {
		return inj, err
	}
	values, err := s.LoadEnv()
	if err != nil {
		return inj, err
	}
	inj.Limits = runtime.Limits{
		CPUs:   cfg.Limits.CPUs,
		Memory: cfg.Limits.Memory,
		PIDs:   cfg.Limits.PIDs,
	}

	names := make([]string, 0, len(cfg.Harness))
	for name := range cfg.Harness {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		h := cfg.Harness[name]
		for key, value := range h.Set {
			inj.Env[key] = value
		}
		for _, key := range h.Env {
			if v := values[key]; v != "" {
				inj.Env[key] = v
			} else {
				inj.Warnings = append(inj.Warnings, fmt.Sprintf(
					"harness %s: %s is not set (smate config key %s)", name, key, key))
			}
		}
		switch {
		case h.State == "":
		case h.Mount == "":
			inj.Warnings = append(inj.Warnings, fmt.Sprintf(
				"harness %s: state is set but mount is empty — nothing to mount", name))
		default:
			dir := s.HarnessDir(h.State)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return inj, fmt.Errorf("create harness state %s: %w", dir, err)
			}
			// Written once and then the user's; silence is the normal course
			// here, warnings are for what is configured wrongly.
			if _, err := s.SeedHarnessState(name, dir); err != nil {
				return inj, err
			}
			inj.Mounts = append(inj.Mounts, runtime.Mount{Host: dir, Container: h.Mount})
		}
	}

	if err := cacheInjection(s, cfg, &inj); err != nil {
		return inj, err
	}
	return inj, nil
}

func cacheInjection(s *store.Store, cfg store.Global, inj *injection) error {
	names := make([]string, 0, len(cfg.Cache))
	for name := range cfg.Cache {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		c := cfg.Cache[name]
		if c.Mount == "" {
			inj.Warnings = append(inj.Warnings, fmt.Sprintf(
				"cache %s: mount is empty — nothing to mount", name))
			continue
		}
		if !filepath.IsAbs(c.Mount) {
			inj.Warnings = append(inj.Warnings, fmt.Sprintf(
				"cache %s: mount must be absolute: %s", name, c.Mount))
			continue
		}
		// /workspace is the snapshot's own bind mount, and virtiofs refuses a
		// second one nested inside it. `mounts` works around that by copying; a
		// cache has nothing to copy into, so it is refused.
		if clean := filepath.Clean(c.Mount); clean == "/workspace" || strings.HasPrefix(clean, "/workspace/") {
			inj.Warnings = append(inj.Warnings, fmt.Sprintf(
				"cache %s: mount is under /workspace — that is the snapshot, not a place for a cache", name))
			continue
		}
		host := c.Host
		if host == "" {
			host = s.CacheDir(name)
		} else if !filepath.IsAbs(host) {
			inj.Warnings = append(inj.Warnings, fmt.Sprintf(
				"cache %s: host must be absolute: %s", name, host))
			continue
		}
		if err := os.MkdirAll(host, 0o700); err != nil {
			return fmt.Errorf("create cache dir %s: %w", host, err)
		}
		inj.Mounts = append(inj.Mounts, runtime.Mount{Host: host, Container: c.Mount})
	}
	return nil
}
