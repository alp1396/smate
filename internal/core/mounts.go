package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"smate/internal/runtime"
)

// containerWorkspace is the container-side root a mounts entry can land under.
// A path under it cannot be bind mounted: /workspace is itself a bind mount, and
// Docker Desktop's virtiofs backend refuses a second one nested inside it
// ("mountpoint is outside of rootfs"), though the same entry works on Linux.
// Such entries are copied into the snapshot instead.
const containerWorkspace = "/workspace/"

// Resolved is the `mounts` entries of .smate.yml, split by how they reach the
// container: Bind passes straight to docker, Copy is applied to the snapshot on
// disk before the container exists.
type Resolved struct {
	Bind []runtime.Mount
	Copy []Copy
}

type Copy struct {
	Host string
	Rel  string
}

// resolveMounts turns the `mounts` entries of .smate.yml into container mounts —
// how a file .gitignore keeps out of the snapshot still reaches the container.
//
// Entries are host:container, docker's own shape. A relative host path is taken
// from the repository root, not the snapshot: the whole point is a file the
// snapshot does not have. The container path must be absolute and must not be
// /workspace, which a mount would hide rather than add to. Checked at start,
// loudly: a mount that fails silently is a file the agent expected and did not get.
func resolveMounts(entries []string, root string) (Resolved, error) {
	var out Resolved
	for _, raw := range entries {
		host, container, ok := strings.Cut(raw, ":")
		host, container = strings.TrimSpace(host), strings.TrimSpace(container)
		if !ok || host == "" || container == "" {
			return Resolved{}, fmt.Errorf("mounts: entry must be host:container: %s", raw)
		}
		if !filepath.IsAbs(container) {
			return Resolved{}, fmt.Errorf("mounts: container path must be absolute: %s", raw)
		}
		clean := filepath.Clean(container)
		if clean == "/workspace" {
			return Resolved{}, fmt.Errorf("mounts: /workspace is the snapshot itself: %s", raw)
		}
		hostPath := host
		if !filepath.IsAbs(hostPath) {
			hostPath = filepath.Join(root, filepath.FromSlash(hostPath))
		}
		if _, err := os.Lstat(hostPath); err != nil {
			if os.IsNotExist(err) {
				return Resolved{}, fmt.Errorf("mounts: %s does not exist", raw)
			}
			return Resolved{}, fmt.Errorf("mounts: check %s: %w", raw, err)
		}
		if rel, ok := strings.CutPrefix(clean, containerWorkspace); ok {
			out.Copy = append(out.Copy, Copy{Host: hostPath, Rel: rel})
		} else {
			out.Bind = append(out.Bind, runtime.Mount{Host: hostPath, Container: container})
		}
	}
	return out, nil
}

func applyCopies(ws string, copies []Copy) ([]string, error) {
	exclude := make([]string, 0, len(copies))
	for _, c := range copies {
		dst := filepath.Join(ws, filepath.FromSlash(c.Rel))
		if err := copyPath(c.Host, dst); err != nil {
			return nil, fmt.Errorf("mounts: copy %s: %w", c.Rel, err)
		}
		exclude = append(exclude, "/"+c.Rel)
	}
	return exclude, nil
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst, info.Mode())
	}
	return copyFile(src, dst, info.Mode())
}

func copyDir(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(dst, mode.Perm()|0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
