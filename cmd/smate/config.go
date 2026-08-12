package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"

	"smate/internal/store"
)

func cmdConfig(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "edit":
			return cmdConfigEdit(args[1:])
		case "key":
			return cmdConfigKey(args[1:])
		}
	}
	if _, _, err := parse(args, nil, nil); err != nil {
		return err
	}
	return showConfig()
}

// showConfig prints how each harness authenticates. Key values are masked: this
// output ends up in screenshots.
func showConfig() error {
	s, err := store.New()
	if err != nil {
		return err
	}
	cfg, err := s.LoadGlobal()
	if err != nil {
		return err
	}
	values, err := s.LoadEnv()
	if err != nil {
		return err
	}

	names := make([]string, 0, len(cfg.Harness))
	for name := range cfg.Harness {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("config:", s.ConfigPath())
	fmt.Println("keys:  ", s.EnvPath())
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "HARNESS\tAUTH\tDETAIL")
	for _, name := range names {
		h := cfg.Harness[name]
		if h.State != "" && h.Mount != "" {
			fmt.Fprintf(w, "%s\tstate\t%s → %s\n", name, s.HarnessDir(h.State), h.Mount)
		}
		for _, key := range h.Env {
			fmt.Fprintf(w, "%s\tenv\t%s = %s\n", name, key, mask(values[key]))
		}
		set := make([]string, 0, len(h.Set))
		for key := range h.Set {
			set = append(set, key)
		}
		sort.Strings(set)
		for _, key := range set {
			fmt.Fprintf(w, "%s\tset\t%s = %s\n", name, key, h.Set[key])
		}
		if h.State == "" && len(h.Env) == 0 && len(h.Set) == 0 {
			fmt.Fprintf(w, "%s\t-\tnothing configured\n", name)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if len(cfg.Cache) == 0 {
		return nil
	}
	cacheNames := make([]string, 0, len(cfg.Cache))
	for name := range cfg.Cache {
		cacheNames = append(cacheNames, name)
	}
	sort.Strings(cacheNames)

	fmt.Println()
	cw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(cw, "CACHE\tHOST\tMOUNT")
	for _, name := range cacheNames {
		c := cfg.Cache[name]
		host := c.Host
		if host == "" {
			host = s.CacheDir(name)
		}
		fmt.Fprintf(cw, "%s\t%s\t%s\n", name, host, c.Mount)
	}
	return cw.Flush()
}

func mask(v string) string {
	switch {
	case v == "":
		return "(not set)"
	case len(v) <= 8:
		return "••••"
	default:
		return "••••" + v[len(v)-4:]
	}
}

func cmdConfigEdit(args []string) error {
	if _, _, err := parse(args, nil, nil); err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	if _, err := s.LoadGlobal(); err != nil { // writes the defaults if missing
		return err
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nano"
	}
	cmd := exec.Command(editor, s.ConfigPath())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", editor, s.ConfigPath(), err)
	}
	_, err = s.LoadGlobal() // fail loudly on a broken file instead of at start
	return err
}

// cmdConfigKey stores one key, read from stdin rather than taken as an argument so
// it does not end up in the shell history.
func cmdConfigKey(args []string) error {
	name, _, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("usage: smate config key <NAME>")
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	values, err := s.LoadEnv()
	if err != nil {
		return err
	}

	fmt.Printf("value for %s (visible while typing): ", name)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("read value: %w", err)
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return fmt.Errorf("empty value")
	}

	values[name] = value
	if err := s.SaveEnv(values); err != nil {
		return err
	}
	fmt.Printf("%s stored in %s\n", name, s.EnvPath())
	return nil
}
