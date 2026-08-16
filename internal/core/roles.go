package core

import (
	"fmt"
	"sort"
	"strings"

	"smate/internal/roles"
	"smate/internal/store"
)

// Roles returns the role library, seeding the bundled defaults on first use.
func Roles(s *store.Store) ([]roles.Role, error) {
	dir := s.RolesDir()
	if err := roles.Seed(dir); err != nil {
		return nil, err
	}
	return roles.LoadAll(dir)
}

// LoadRole reads one role and checks what only the control plane can: that its
// harness is configured. An unknown one means no keys and no state mount. The
// harness comes back with it because that is where the command line is built
// from — the role names a model, the harness knows the flag.
func LoadRole(s *store.Store, name string) (roles.Role, store.Harness, error) {
	dir := s.RolesDir()
	if err := roles.Seed(dir); err != nil {
		return roles.Role{}, store.Harness{}, err
	}
	r, err := roles.Load(dir, name)
	if err != nil {
		return roles.Role{}, store.Harness{}, err
	}
	cfg, err := s.LoadGlobal()
	if err != nil {
		return roles.Role{}, store.Harness{}, err
	}
	h, ok := cfg.Harness[r.Harness]
	if !ok {
		known := make([]string, 0, len(cfg.Harness))
		for n := range cfg.Harness {
			known = append(known, n)
		}
		sort.Strings(known)
		return roles.Role{}, store.Harness{}, fmt.Errorf("role %s: harness %q is not in %s (configured: %s)",
			r.Name, r.Harness, s.ConfigPath(), strings.Join(known, ", "))
	}
	return r, h, nil
}

func ResetRoles(s *store.Store, name string) ([]string, error) {
	dir := s.RolesDir()
	if err := roles.Seed(dir); err != nil {
		return nil, err
	}
	if name != "" {
		if err := roles.Reset(dir, name); err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	names, err := roles.Bundled()
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if err := roles.Reset(dir, n); err != nil {
			return nil, err
		}
	}
	return names, nil
}
