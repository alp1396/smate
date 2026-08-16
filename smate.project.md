# smate

Written by the `init` role from a snapshot of one commit (tracked files only, no
git history, no remotes, no untracked files). First version of this file — there
was no `smate.project.md` in the snapshot.

## What it is

`smate` is a CLI tool that runs a development task inside a disposable Docker
container and lets **only a validated patch series** out of it. A person or an
agent CLI (Claude Code, Codex, OpenCode) works inside the sandbox; the working
`.git`, its history, its remotes and the user's keys are not reachable from
there.

The cycle, as `README.md` states it:

```
git archive HEAD  →  /workspace in container smate-<id>  →  work  →
git format-patch  →  checked on the host  →  git am onto branch <id>
```

What goes in is a flat `git archive HEAD` snapshot minus anything the project
lists under `secrets`; what comes back is a patch series, validated on the host
and landed on a separate branch. Task statuses are `ACTIVE` → `DONE` |
`REJECTED` → `CLEANED`.

Version `0.4.0` (`VERSION`, embedded by `version.go` rather than passed with
`-ldflags`). License: see `LICENSE`.

## Modules and how they depend on each other

Dependencies run one way: `cmd` → `internal/core` → leaf packages. Leaf packages
do not import `core`; composition of user-level operations lives only in `core`.

```
cmd/smate/          CLI entry point: usage text, hand-written argument parsing,
                    dispatch, output (main.go, config.go, run.go)
internal/core/      control plane — the only place store + gitx + runtime +
                    patch are combined: start, apply, clean, shell, run, run
                    states, harnesses, roles, images, mounts, secrets
internal/store/     layout of ~/.smate on disk: tasks and meta.json, config.yml,
                    env.yml (0600), harness state, caches, image/role libraries
internal/task/      domain model: Status, Task, container name smate-<id>
internal/gitx/      wrapper over the git CLI: archive, sandbox init with a
                    `baseline` commit, format-patch, am, branch operations
internal/runtime/   wrappers over the docker CLI (run, exec, build, inspect,
                    limits, mounts) and over tmux (detached run session,
                    pipe-pane, attach, kill)
internal/patch/     parses and validates a patch series as untrusted input, and
                    scans it for literal key values
internal/secrets/   the single interpreter of the `secrets` denylist
internal/images/    image library: bundled Dockerfiles (base, php, node, go),
                    seed, reset, list, tag smate/<name>:latest
internal/roles/     role library: role.yml + AGENTS.md, bundled planner, coder,
                    reviewer, init; inputs/outputs contract
internal/artifacts/ layout of the .smate/ directory inside the workspace, the
                    channel roles exchange files through
internal/tui/       bubbletea task screen: actions / logs / diff / artefacts tabs
```

Bundled defaults are embedded from `internal/images/defaults/`,
`internal/roles/defaults/` and `internal/store/defaults/claude/settings.json`.

## How it is started

Build and install:

```sh
make install        # runs tests, builds, then `go install ./cmd/smate`
                    # → $(go env GOPATH)/bin, which must be on PATH
make build          # go build -o bin/smate ./cmd/smate  → ./bin/smate
```

Then, from a working repository (`README.md`, "Usage"):

```sh
smate                            # the task screen — the usual way in
smate start <id> [--image IMG]   # snapshot the current branch → smate-<id>
smate shell [<id>]               # enter the container
smate apply [<id>]               # validate the changes and import as branch <id>
smate list
smate clean [<id>] [--purge]

smate run <id> --role <name> [-m "..."] [--force]
smate attach|logs|stop [<id>]
smate images | smate build <name> | smate images reset <name>|--all
smate roles  | smate roles reset <name>|--all
smate config | smate config edit | smate config key <NAME>
```

There is no daemon: every command is one shot, and state lives on disk under
`~/.smate` (`tasks/<id>/meta.json`, `config.yml`, `env.yml`). Before first use,
`smate build base` builds the base environment image; the stacks build on top of
it.

Note: `CLAUDE.md` lists a `smate open-ide` command, but no such case exists in
`cmd/smate/main.go` and the README's usage does not mention it — treat `CLAUDE.md`
as stale on that point.

## How it is tested

```sh
make test           # go vet ./... && go test ./...
```

Pointwise: `go test ./internal/core/...`, `go test -run TestApply ./internal/core`.
`make test` is required before calling work finished (`CLAUDE.md`).

Conventions: standard `testing` only, no frameworks and no mocks; tests that need
`git` or `docker` run the real binaries and `t.Skip` when `exec.LookPath` does not
find them; helpers are declared at the top of the test file with `t.Helper()`.

Verified in this snapshot: `go vet ./... && go test ./...` passes — every package
`ok`, `internal/task` has no test files. Docker was absent here, so the
docker-dependent tests skipped themselves and their coverage is unproven from
inside the sandbox.

There is no CI configuration in the snapshot (no `.github/`, no other CI file).

## Stack

- **Language / runtime**: Go, module `smate`, `go 1.24.0` in `go.mod`; README
  requires Go 1.24+. Verified toolchain in this sandbox: go1.24.13 linux/arm64.
- **Package manager**: Go modules (`go.mod` / `go.sum`, vendored directory absent).
- **Build tool**: `make` (a four-target Makefile: build, test, install, clean).
- **Dependencies**, deliberately few: the charmbracelet stack —
  `bubbletea v1.3.10`, `bubbles v0.21.0`, `lipgloss v1.1.0` — and
  `gopkg.in/yaml.v3 v3.0.1`. Everything else in `go.mod` is indirect. Argument
  parsing is hand-written on purpose (the stdlib `flag` stops at the first
  positional argument); Cobra and urfave are not to be introduced.
- **External tools it shells out to**: `git`, `docker`, `tmux`. It implements no
  git or docker protocol of its own.
- **Runtime requirements**: Go 1.24+, `git`, and a running Docker; container
  images derive from `ubuntu:24.04` with Node 22 and the three agent CLIs
  (`@anthropic-ai/claude-code`, `@openai/codex`, `opencode-ai`).
- **Services / keys**: no database and no server. The agent CLIs reach their own
  providers; keys are stored in `~/.smate/env.yml` (0600) and passed as
  environment variables.

## Container image

`smate.Dockerfile` in the repository root — the bundled `go` stack plus `make`,
on `smate/base:latest`. It has never been built (no Docker in the sandbox); the
build command is in a comment at the top of the file.
