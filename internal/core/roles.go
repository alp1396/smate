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
// harness is configured. An unknown one means no keys and no state mount.
func LoadRole(s *store.Store, name string) (roles.Role, error) {
	dir := s.RolesDir()
	if err := roles.Seed(dir); err != nil {
		return roles.Role{}, err
	}
	r, err := roles.Load(dir, name)
	if err != nil {
		return roles.Role{}, err
	}
	cfg, err := s.LoadGlobal()
	if err != nil {
		return roles.Role{}, err
	}
	if _, ok := cfg.Harness[r.Harness]; !ok {
		known := make([]string, 0, len(cfg.Harness))
		for n := range cfg.Harness {
			known = append(known, n)
		}
		sort.Strings(known)
		return roles.Role{}, fmt.Errorf("role %s: harness %q is not in %s (configured: %s)",
			r.Name, r.Harness, s.ConfigPath(), strings.Join(known, ", "))
	}
	return r, nil
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
