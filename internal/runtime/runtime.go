// Package runtime is a thin wrapper around the docker CLI.
package runtime

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Mount struct {
	Host      string
	Container string
}

type Limits struct {
	CPUs   string
	Memory string
	PIDs   int
}

// Args renders the limits as docker flags. Disk is absent on purpose:
// --storage-opt size= needs a driver with quota support and silently does nothing
// on an ordinary install.
func (l Limits) Args() []string {
	var args []string
	if l.CPUs != "" {
		args = append(args, "--cpus", l.CPUs)
	}
	if l.Memory != "" {
		args = append(args, "--memory", l.Memory)
	}
	if l.PIDs > 0 {
		args = append(args, "--pids-limit", strconv.Itoa(l.PIDs))
	}
	return args
}

// ContainerHome is the home of a task process inside the container. The image
// makes it writable by any uid, because ours comes from the host and usually has
// no entry in /etc/passwd there.
const ContainerHome = "/home/smate"

func HostUser() string {
	uid, gid := os.Getuid(), os.Getgid()
	if uid < 0 || gid < 0 {
		return ""
	}
	return strconv.Itoa(uid) + ":" + strconv.Itoa(gid)
}

// Run starts the task container. Besides the workspace only what the harnesses
// need is mounted — no host home, no sockets, no host network. It stays up for the
// whole task, hence sleep infinity.
func Run(name, image, workspace string, env map[string]string, mounts []Mount, limits Limits) error {
	args := []string{"run", "-d",
		"--name", name,
		"-v", workspace + ":/workspace",
		"-w", "/workspace",
	}
	if user := HostUser(); user != "" {
		args = append(args, "--user", user, "-e", "HOME="+ContainerHome)
	}
	args = append(args, limits.Args()...)
	for _, m := range mounts {
		args = append(args, "-v", m.Host+":"+m.Container)
	}
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	sort.Strings(names) // stable command line, easier to compare runs
	for _, k := range names {
		args = append(args, "-e", k+"="+env[k])
	}
	args = append(args, image, "sleep", "infinity")

	cmd := exec.Command("docker", args...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker run %s: %s", name, strings.TrimSpace(errb.String()))
	}
	return nil
}

func ShellCmd(name string) *exec.Cmd {
	return exec.Command("docker", "exec", "-it", name, "bash")
}

func ExecCmd(name, cmdline string) *exec.Cmd {
	return exec.Command("docker", "exec", "-it", name, "sh", "-c", cmdline)
}

func RunAttached(cmd *exec.Cmd) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

type ImageInfo struct {
	Size    int64
	Created time.Time
}

func InspectImage(tag string) (info ImageInfo, ok bool) {
	out, err := exec.Command("docker", "image", "inspect",
		"-f", "{{.Size}} {{.Created}}", tag).Output()
	if err != nil {
		return ImageInfo{}, false
	}
	size, created, found := strings.Cut(strings.TrimSpace(string(out)), " ")
	if !found {
		return ImageInfo{}, false
	}
	info.Size, _ = strconv.ParseInt(size, 10, 64)
	info.Created, _ = time.Parse(time.RFC3339Nano, created)
	return info, true
}

func Build(tag, dir string) error {
	cmd := exec.Command("docker", "build", "-t", tag, dir)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build %s: %w", tag, err)
	}
	return nil
}

func Running(name string) bool {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name).Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// Exists reports whether the task container exists in any state.
func Exists(name string) bool {
	return exec.Command("docker", "inspect", "-f", "{{.Id}}", name).Run() == nil
}

// Remove stops and deletes the container with its writable layer and anonymous
// volumes. Images are left alone. A missing container is not an error.
func Remove(name string) error {
	if !Exists(name) {
		return nil
	}
	cmd := exec.Command("docker", "rm", "-f", "-v", name)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker rm %s: %s", name, strings.TrimSpace(errb.String()))
	}
	return nil
}
