// Command smate orchestrates isolated development tasks.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"smate/internal/core"
	"smate/internal/gitx"
	"smate/internal/store"
	"smate/internal/tui"
)

const usage = `smate — an isolated sandbox for development tasks

Usage (from the working repository):
  smate                            open the task screen: actions, logs, diff, artefacts
  smate --help                     this list

  smate start <id> [--image IMG]   create the task sandbox
  smate shell [<id>]               enter the task container
  smate open-ide [<id>]            open the task workspace in your editor
  smate apply [<id>]               take the changes and import them
  smate list                       list tasks and their runs
  smate clean [<id>] [--purge]     stop the container and free space

  smate run <id> --role <name>     start a role, detached
       [-m "..."] [--force]        a note for this run; skip the inputs check
  smate attach [<id>]              enter the live run and leave it running
  smate logs [<id>] [-f]           the run's current screen
  smate stop [<id>]                kill the run, leave the task alone

  smate images                     list the image library
  smate images reset <name>|--all  restore bundled defaults
  smate build <name>               build a library image

  smate roles                      list the role library
  smate roles reset <name>|--all   restore bundled defaults

  smate config                     show harness configuration
  smate config edit                edit ~/.smate/config.yml
  smate config key <NAME>          store an API key
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "smate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Bare `smate` opens the screen rather than printing the manual; the flag says
	// so.
	if len(args) == 0 {
		return cmdTUI()
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(usage)
		return nil
	case "start":
		return cmdStart(args[1:])
	case "shell":
		return cmdShell(args[1:])
	case "open-ide":
		return cmdOpenIDE(args[1:])
	case "apply":
		return cmdApply(args[1:])
	case "list":
		return cmdList()
	case "clean":
		return cmdClean(args[1:])
	case "images":
		return cmdImages(args[1:])
	case "build":
		return cmdBuild(args[1:])
	case "roles":
		return cmdRoles(args[1:])
	case "run":
		return cmdRun(args[1:])
	case "attach":
		return cmdAttach(args[1:])
	case "logs":
		return cmdLogs(args[1:])
	case "stop":
		return cmdStop(args[1:])
	case "config":
		return cmdConfig(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

// parse reads a subcommand's arguments: at most one positional <id> plus the known
// flags, in any order. The flag package does not fit: it stops at the first
// positional argument, and `smate start 123 --image X` is how one writes it.
func parse(args []string, valued, boolean []string) (id string, flags map[string]string, err error) {
	flags = map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := strings.TrimLeft(a, "-")
		switch {
		case !strings.HasPrefix(a, "-"):
			if id != "" {
				return "", nil, fmt.Errorf("unexpected argument: %s", a)
			}
			id = a
		case contains(boolean, name):
			flags[name] = "true"
		case contains(valued, name):
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("flag --%s needs a value", name)
			}
			i++
			flags[name] = args[i]
		default:
			if k, v, ok := strings.Cut(name, "="); ok && contains(valued, k) {
				flags[k] = v
				continue
			}
			return "", nil, fmt.Errorf("unknown flag: %s", a)
		}
	}
	return id, flags, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func cmdStart(args []string) error {
	id, flags, err := parse(args, []string{"image"}, nil)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("usage: smate start <id> [--image IMG]")
	}

	repo, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve current directory: %w", err)
	}
	image, err := resolveImage(repo, flags["image"])
	if err != nil {
		return err
	}

	s, err := store.New()
	if err != nil {
		return err
	}
	t, warnings, err := core.Start(s, repo, id, image)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		return err
	}
	fmt.Printf("task %s: %s @ %.8s → container %s\n", t.ID, t.Branch, t.BaseSHA, t.Container())
	fmt.Printf("enter with: smate shell %s\n", t.ID)
	return nil
}

// resolveImage picks the environment image: --image, then .smate.yml, then ask.
func resolveImage(repo, flagImage string) (string, error) {
	if flagImage != "" {
		return flagImage, nil
	}
	root, err := gitx.Root(repo)
	if err != nil {
		return "", err
	}
	cfg, _, err := store.LoadConfig(root)
	if err != nil {
		return "", err
	}
	if cfg.Image != "" {
		return cfg.Image, nil
	}
	// The prompt lists the library: asking in silence is how one types "go:latest"
	// and gets an unrelated image from the hub.
	s, err := store.New()
	if err != nil {
		return "", err
	}
	if names, err := core.ImageNames(s); err == nil && len(names) > 0 {
		fmt.Println("image library:", strings.Join(names, ", "))
	}
	fmt.Println("or any docker reference, e.g. ubuntu:24.04")
	fmt.Print("environment image for this project: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read image name: %w", err)
	}
	image := strings.TrimSpace(line)
	if image == "" {
		return "", fmt.Errorf("no image given")
	}
	return image, nil
}

func cmdShell(args []string) error {
	id, _, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	return core.Shell(s, id)
}

func cmdOpenIDE(args []string) error {
	id, _, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	cmd, warnings, err := core.OpenIDECmd(s, id)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	// Attached: a GUI editor hands the terminal back at once, a terminal editor
	// gets a real tty. One behaviour covers both, so there is no flag to pick.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func cmdApply(args []string) error {
	id, _, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	t, rep, err := core.Apply(s, id)
	if errors.Is(err, core.ErrNothingToApply) {
		fmt.Println("no changes — nothing to import")
		printCut(rep.Cut)
		return nil
	}
	if err != nil {
		return err
	}
	for _, w := range rep.Warnings {
		fmt.Println("warning:", w)
	}
	fmt.Printf("branch %s in %s, files: %d\n", t.ID, t.Repo, len(rep.Files))
	for _, f := range rep.Files {
		fmt.Println("  ", f)
	}
	printCut(rep.Cut)
	return nil
}

// printCut reports paths that were cut from the snapshot and so did not travel
// back, even though the agent changed them.
func printCut(cut []string) {
	if len(cut) == 0 {
		return
	}
	fmt.Printf("\ncut as secrets (not imported): %d\n", len(cut))
	for _, f := range cut {
		fmt.Println("  -", f)
	}
}

func cmdClean(args []string) error {
	id, flags, err := parse(args, nil, []string{"purge"})
	if err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	purge := flags["purge"] == "true"
	done, err := core.Clean(s, id, purge)
	if err != nil {
		return err
	}
	if len(done) == 0 {
		fmt.Println("nothing to clean")
		return nil
	}
	for _, c := range done {
		if c.Purged {
			fmt.Printf("%s: container and task removed\n", c.ID)
		} else {
			fmt.Printf("%s: container removed, workspace cleared\n", c.ID)
		}
	}
	return nil
}

func cmdImages(args []string) error {
	if len(args) > 0 && args[0] == "reset" {
		return cmdImagesReset(args[1:])
	}
	if _, _, err := parse(args, nil, nil); err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	list, err := core.Images(s)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("the image library is empty")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTAG\tSTATE\tSIZE\tBUILT")
	for _, img := range list {
		state, size, built := "not built", "-", "-"
		if img.Built {
			state = "built"
			size = fmt.Sprintf("%.0f MB", float64(img.Info.Size)/(1024*1024))
			built = img.Info.Created.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", img.Name, img.Tag, state, size, built)
	}
	return w.Flush()
}

func cmdImagesReset(args []string) error {
	name, flags, err := parse(args, nil, []string{"all"})
	if err != nil {
		return err
	}
	all := flags["all"] == "true"
	switch {
	case name == "" && !all:
		return fmt.Errorf("usage: smate images reset <name> | smate images reset --all")
	case name != "" && all:
		return fmt.Errorf("pass either a name or --all, not both")
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	done, err := core.ResetImages(s, name)
	if err != nil {
		return err
	}
	for _, n := range done {
		fmt.Printf("%s: restored to the bundled default\n", n)
	}
	return nil
}

func cmdRoles(args []string) error {
	if len(args) > 0 && args[0] == "reset" {
		return cmdRolesReset(args[1:])
	}
	if _, _, err := parse(args, nil, nil); err != nil {
		return err
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	list, err := core.Roles(s)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("the role library is empty")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tORDER\tHARNESS\tMODEL\tEFFORT\tINPUTS\tOUTPUTS")
	for _, r := range list {
		inputs := strings.Join(r.Inputs, " ")
		if inputs == "" {
			inputs = "-"
		}
		// A dash rather than a blank: the harness's own default is a choice too.
		model, effort := r.Model, r.Effort
		if model == "" {
			model = "-"
		}
		if effort == "" {
			effort = "-"
		}
		// The rows are already in role order; the column says which number to edit.
		order := "-"
		if r.Order != 0 {
			order = strconv.Itoa(r.Order)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, order, r.Harness, model, effort, inputs, strings.Join(r.Outputs, " "))
	}
	return w.Flush()
}

func cmdRolesReset(args []string) error {
	name, flags, err := parse(args, nil, []string{"all"})
	if err != nil {
		return err
	}
	all := flags["all"] == "true"
	switch {
	case name == "" && !all:
		return fmt.Errorf("usage: smate roles reset <name> | smate roles reset --all")
	case name != "" && all:
		return fmt.Errorf("pass either a name or --all, not both")
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	done, err := core.ResetRoles(s, name)
	if err != nil {
		return err
	}
	for _, n := range done {
		fmt.Printf("%s: restored to the bundled default\n", n)
	}
	return nil
}

func cmdBuild(args []string) error {
	name, _, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("usage: smate build <name>")
	}
	s, err := store.New()
	if err != nil {
		return err
	}
	if err := core.Build(s, name); err != nil {
		return err
	}
	fmt.Printf("built %s\n", name)
	return nil
}

func cmdTUI() error {
	s, err := store.New()
	if err != nil {
		return err
	}
	return tui.Run(s)
}

func cmdList() error {
	s, err := store.New()
	if err != nil {
		return err
	}
	views, err := core.ListRuns(s)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		fmt.Println("no tasks")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tBRANCH\tBASE\tRUN\tROLE\tRESULT\tREPO")
	for _, v := range views {
		t := v.Task
		base := t.BaseSHA
		if len(base) > 8 {
			base = base[:8]
		}
		run, role, result := string(v.Run.State), "-", "-"
		if v.HasRun {
			role = v.Run.Meta.Role
			run = fmt.Sprintf("%d %s", v.Run.Meta.N, v.Run.State)
			if v.Run.HasResult {
				result = strings.Join(v.Run.Meta.Outputs, " ")
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Status, t.Branch, base, run, role, result, t.Repo)
	}
	return w.Flush()
}
