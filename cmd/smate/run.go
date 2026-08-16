package main

import (
	"fmt"
	"os"
	"time"

	"smate/internal/core"
	"smate/internal/store"
)

func cmdRun(args []string) error {
	id, flags, err := parse(args, []string{"role", "m"}, []string{"force"})
	if err != nil {
		return err
	}
	if id == "" || flags["role"] == "" {
		return fmt.Errorf(`usage: smate run <id> --role <name> [-m "..."] [--force]`)
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	meta, warnings, err := core.Run(s, id, flags["role"], flags["m"], flags["force"] == "true")
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		return err
	}
	fmt.Printf("run %d: role %s started in the background\n", meta.N, meta.Role)
	fmt.Printf("watch it: smate logs %s -f    step in: smate attach %s\n", id, id)
	return nil
}

func cmdAttach(args []string) error {
	id, _, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	// Said before handing the terminal over: inside, the keys go to the agent and
	// Ctrl-C is the one that ends the run.
	fmt.Println("detach with Ctrl-B then D — Ctrl-C would kill the agent")
	return core.Attach(s, id)
}

func cmdStop(args []string) error {
	id, _, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	r, err := core.Stop(s, id)
	if err != nil {
		return err
	}
	fmt.Printf("run %d (%s) killed; the task is untouched\n", r.Meta.N, r.Meta.Role)
	return nil
}

// pollEvery is how often `logs -f` re-reads the screen. This is a screen, not a
// stream, so it is polled rather than followed.
const pollEvery = 2 * time.Second

func cmdLogs(args []string) error {
	id, flags, err := parse(args, nil, []string{"f"})
	if err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	if flags["f"] != "true" {
		screen, r, err := core.Logs(s, id)
		if err != nil {
			return err
		}
		fmt.Println(screen)
		fmt.Println(runLine(r))
		return nil
	}
	for {
		screen, r, err := core.Logs(s, id)
		if err != nil {
			return err
		}
		fmt.Print("\033[H\033[2J") // home, clear: the screen is redrawn, not appended
		fmt.Println(screen)
		fmt.Println(runLine(r))
		if r.State != core.StateWorking && r.State != core.StateSleep {
			return nil
		}
		time.Sleep(pollEvery)
	}
}

func runLine(r core.RunInfo) string {
	line := fmt.Sprintf("— run %d (%s): %s", r.Meta.N, r.Meta.Role, r.State)
	switch r.State {
	case core.StateSleep:
		line += fmt.Sprintf(", silent for %s — smate attach", r.Silent.Round(time.Second))
	case core.StateFailed:
		line += fmt.Sprintf(", exit %d", r.Exit)
		if r.OutOfMemory() {
			line += " — " + core.OOMHint
		}
	}
	if r.HasResult {
		line += ", result written"
	} else if r.State == core.StateDone {
		line += ", no result written"
	}
	return line
}
