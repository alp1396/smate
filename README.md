# SMATE - Docker based sandbox for running AI agents isolated

An isolated sandbox for development tasks. A person, or their agent, works
inside a Docker container, and the only thing that leaves it is a validated
patch series — no access to the working `.git`, its history, remotes or the
user's keys.

What goes in is a flat `git archive HEAD` snapshot: tracked files and nothing
else. What comes back is a patch series, checked on the host and landed on a
separate branch of the working repository.

## Install

```sh
make install       # smate → $(go env GOPATH)/bin, which must be on your PATH
smate build base   # the base environment image
smate build php    # a stack on top of it
```

Requires Go 1.24+, git and a running Docker.

## Image library

Environment images live in `~/.smate/images/<name>/Dockerfile`, shared across
projects. The library is filled with the bundled defaults — `base`, `php`,
`node`, `go` — the first time it is used; the stacks build on top of `base`.

```sh
smate images                     # what is in the library and what is built
smate build <name>               # docker build -t smate/<name>:latest
smate images reset <name>        # restore a default, discarding local edits
smate images reset --all
```

Edit the Dockerfiles freely: they are yours, and `reset` brings a default back
if an experiment goes wrong.

`base` carries the three agent CLIs — Claude Code, Codex and OpenCode — so every
stack has them.

## Agent CLIs and keys

How each CLI authenticates is described once, in `~/.smate/config.yml`:

```yaml
harness:
  claude:
    state: claude              # ~/.smate/harness/claude, kept between tasks
    mount: /home/smate/.claude  # mounted here inside the container
    set:                        # literal values, as opposed to keys
      CLAUDE_CONFIG_DIR: /home/smate/.claude
    model_flag: "--model {}"    # how a role's model: reaches this CLI
  codex:
    state: codex
    mount: /home/smate/.codex
    env: [OPENAI_API_KEY]
    model_flag: "-m {}"
    effort_flag: "-c model_reasoning_effort={}"
  opencode:
    state: opencode
    mount: /home/smate/.local/share/opencode
    env: [OPENROUTER_API_KEY]
    model_flag: "--model {}"
```

Each harness is also something to open by hand: the task screen lists them under
the shell, and picking one runs it in the container with the terminal handed
over. What it runs is the harness's own name unless an optional `cmd:` says
otherwise — `cmd: opencode run --tui`, say.

`model_flag` and `effort_flag` are how a role's `model:` and `effort:` turn into
a command line: the fragment is appended to the harness's command with the value
in place of `{}`, and left out entirely when the role names nothing. The
spelling lives here, with the harness, because it is a property of the CLI —
`--model X` for one, `-c model_reasoning_effort=X` for another — while the role
only knows which model it wants. Claude Code has no reasoning-effort flag at
all, so `effort_flag` is simply absent for it; a role that asks for effort
anyway is run on the default and told so in a warning.

`state` is what makes a login survive a new container. It only works when the
CLI keeps everything in one directory — Claude Code splits its state between
`~/.claude` and `~/.claude.json` unless `CLAUDE_CONFIG_DIR` points it at a
single place, which is what `set` is for.

Mounts sit under `/home/smate` rather than `/root` because a task does not run as
root — see below.

Key values live apart, in `~/.smate/env.yml` (mode 600), and never in a project
file — `.smate.yml` is committed.

```sh
smate config                       # what is configured; values are masked
smate config edit                  # $EDITOR on config.yml
smate config key OPENAI_API_KEY    # read a value from stdin into env.yml
```

Everything configured is injected into every task container: one user, one set
of keys, so splitting them per project would be a ritual rather than a boundary.
A harness whose key is missing produces a warning, not an error.

Because the container now holds keys, `apply` refuses any patch containing one
of their values. That catches the accident — a key copied into a config file.
It does not catch intent: base64 or a value written out in pieces gets through.
Against that, only a credential broker helps, and there isn't one yet.

## Caches

A fresh snapshot means a fresh container, which means a build tool that keeps a
local cache — Go's module cache, npm's, pip's — starts from nothing every
task and downloads the same dependencies again. `cache` in `~/.smate/config.yml`
mounts a host directory into every container to keep one warm across tasks and
branches, the same way a harness keeps its login:

```yaml
cache:
  go-mod:
    mount: /home/smate/go/pkg/mod   # host defaults to ~/.smate/cache/go-mod
  go-build:
    host: /Users/me/Library/Caches/go-build  # or point at a cache that already exists
    mount: /home/smate/.cache/go-build
```

`host` is optional: left out, smate keeps the cache itself under
`~/.smate/cache/<name>`, created empty on first use and filled in over time.
Set it to reuse a cache that already exists on the machine instead — whatever
was downloaded outside of smate is available on the first task, too.

Unlike `mounts` in `.smate.yml`, a cache entry's `mount` may not land under
`/workspace`: there is no snapshot counterpart for it to copy into, and
`/workspace` is itself a bind mount already — the same nested-mount failure
described under **Project config** below, with no copy fallback to catch it. A
bad entry produces a warning at `start` rather than stopping the task, the same
as a misconfigured harness.

Several tasks running at once share the same cache directory. Most build
tools lock their cache against concurrent writers on their own — Go has since
1.14 — but a lock implemented on top of a host filesystem shared into several
containers is not something to build critical correctness on.

## Usage

Commands run from the working repository.

```sh
smate                           # the task screen — this is the usual way in
smate --help                    # everything below, as a list

smate start 123 [--image IMG]   # snapshot the current branch → container smate-123
smate shell [<id>]              # enter the container and work
smate apply [<id>]              # validate the changes and import them as branch <id>
smate list                      # tasks, newest first, with their statuses and runs
smate clean [<id>] [--purge]    # stop the container and free space
```

Bare `smate` opens the task list; enter on a task opens it on its **actions**
tab: apply, a shell, every harness from `config.yml`, every role in the library
with what it is doing right now, and clean. Enter on a harness opens that CLI in
the container; enter on a role gives it connect, run, run-with-a-note, attach and
stop; attach and stop are inert unless that role is the one running, and run is
inert while an input it declares is not in `.smate/` — the row names the artefact
it is waiting for instead of offering a keypress that ends in the same refusal. Apply, clean and
stop ask before they act — they are the three that cannot be taken back. The
other tabs (logs, diff, artefacts) are read-only views of the same task.

Without an `<id>` the commands act on the single active task. Several tasks can
be in flight at once: each has its own container and snapshot, and each imports
onto the commit it was taken from, in any order.

A typical round by hand:

```sh
smate start 123        # the image is asked once and remembered in .smate.yml
smate shell            # edit, commit into the sandbox's own git, exit
smate apply            # → branch 123 in the repository; push and MR stay manual
smate clean            # drop the container and the snapshot
```

## Roles

A role is a described performer: which harness runs it, on which model, which
artefacts it needs and which one it must leave behind. Roles live in
`~/.smate/roles/<name>/` — a `role.yml` and an `AGENTS.md` — and are shared
between projects. `init`, `planner`, `coder` and `reviewer` ship with the
binary.

```yaml
# ~/.smate/roles/reviewer/role.yml
order: 30                      # where it sits in every list smate prints
harness: claude                # where env, mounts and state come from
model: claude-sonnet-5         # the value; the flag comes from the harness
                               # effort: high — likewise, where the CLI has one
inputs: [coder.result.md]      # must be in .smate/, or the run refuses
outputs: [reviewer.result.md]  # what the run is judged by
```

`order` is the order of the work rather than of the alphabet — planner, coder,
reviewer is what one wants to read, and `coder, planner, reviewer` is what
sorting by name gives for free. The bundled ones are numbered 5, 10, 20 and 30,
so a role of your own fits between them without renumbering anything. A role with
no `order` sorts after every numbered one, among its unnumbered peers by name.

`harness`, `model` and `effort` are three separate values because only the first
is a choice about the role. The role says which model it wants; the harness in
`config.yml` says how that CLI is told about it (`model_flag`, `effort_flag`), so
pointing a role at another harness is one line rather than a rewritten command.
Both `model` and `effort` are optional — left out, the CLI runs on its own
default. A role that carries the old `cmd:` is refused with a message saying
where its model has moved, rather than quietly running the wrong one.

The bundled `planner` reads nothing but the note
for the run and leaves `.smate/task.md` and `.smate/plan.md`; the bundled
`coder` requires the task and follows the plan when one is there, and leaves
`.smate/coder.result.md`; the bundled `reviewer` reads that report and the diff
against `baseline`, and leaves `.smate/reviewer.result.md`. The bundled `init`
is described below.

### init, and results that belong in the repository

`init` is run once on a repository nobody has described yet. It reads the
snapshot and writes `smate.project.md` in the repository root: what the project
is, its modules, how it is started, how it is tested, what stack it stands on.
Alongside it, when the project can be containerised at all, it writes
`smate.Dockerfile` — an image to run and test the repository on. The other three
roles read `smate.project.md` before starting, when it is there.

Both files sit in the repository, not in `.smate/`, and that is the point: they
are meant to be committed, so they come back through the patch series like any
other change and are reviewed the same way. What `outputs:` lists is
`init.result.md` — the report of the run. This is the same split the coder works
under, where the product is code and the artefact is a report about it, and it is
why `outputs:` needs no notion of a repository path: an artefact is cleared
before every run, and clearing a file of the project is not something a role
should be able to ask for.

Two things `init` cannot do, and says so rather than pretending otherwise. It
cannot build the Dockerfile it proposes — there is no Docker inside the sandbox —
so the file is a proposal carrying the build command in its opening comment.
And it refuses to write one at all for a project that does not belong in a
container: a desktop or mobile application, a toolchain absent on Linux, anything
wanting the host kernel or a display. A Dockerfile that cannot work is worse than
none, because somebody will try to build it.

Nothing in smate reads `smate.Dockerfile` on its own: `image:` in `.smate.yml`
still takes a name from the image library or a docker reference. Building it and
deciding what to do with it is yours, after `apply`.

`outputs` is a list, and a run has produced a result only when every one of them
is written and not empty — half a plan is not something the next role should be
allowed to start from. They are all deleted at the start of the run, so a run
that dies halfway cannot leave one fresh file next to one from yesterday.

A missing input stops the run — that contract is what will make a chain of roles
deterministic. Two things wave it through, both of them deliberate: `--force`, and
`-m "..."`. The note is itself a statement of the task: it reaches the agent as
the first thing it reads, so `smate run 300 --role planner -m "add index.html"`
works on an empty task, with a warning about what was not there.

```sh
smate roles                     # the library
smate roles reset <name>|--all  # restore bundled defaults

smate run 123 --role coder      # start it detached; the terminal comes back
smate logs 123 [-f]             # the run's current screen
smate attach 123                # step into the live run; Ctrl-B D to leave it running
smate stop 123                  # kill the run, leave the task alone
```

A run lives in a tmux session inside the container, so it survives your leaving
and takes you back: `attach` puts you at the agent's own terminal, where you can
answer its question and detach without killing it.

**Connect** — the first action on a role's screen in the task UI — is the same
role with you in the room. It prepares exactly what a run prepares: the same
`AGENTS.md` copied in, the same note consumed, the same session in the same
container, counted as the same kind of run. What differs is the last line of the
prompt: instead of "write your result, nobody is watching the terminal", the
agent is told to read its role, say so in a line and wait for you. Then the
terminal is handed to that session, so you steer the role by hand — with its
instructions and its artefact contract already in place, rather than an unbriefed
`Open claude`. Because it starts detached and only then attaches, closing the
terminal leaves the agent alive; `attach` comes back to it, `stop` ends it.

Two things a run is strict about are relaxed there, since somebody is present: a
missing input is a warning rather than a refusal, and the role's outputs are left
alone instead of being cleared — connecting is also how one goes in to read the
last report and ask about it. The price is that a result written during a
connected session cannot be told from the one that was already there. A connected
session holds the task's one run slot, so `run` will ask you to stop it first.

For a run to get anywhere on its own, the harness must stop asking permission for
every read and every command. On first use smate writes defaults into the
harness's own state — `~/.smate/harness/claude/settings.json`, mounted into the
container — which allow work to proceed and deny the commands that install
software. Edit that file to taste; it is only ever written when missing.

The deny list guards against habit, not intent: it matches command prefixes, so
`sh -c 'apt-get ...'` walks past it. What actually holds is the container.

Artefacts live in `.smate/` inside the workspace: the request you write, the
per-run note (`-m "..."`), the reports roles leave behind. That directory never
reaches a commit — it is excluded when the patch is produced and refused if it
shows up in one anyway. A project that already tracks a `.smate/` of its own is
refused at `start`: our files would land among the project's and travel back in
the patch as edits to them.

Run states, computed on demand from the session and the recorded exit code:

```
NOT RUN   the task has never started a role
WORKING   the session is alive and printing
SLEEP     alive but silent for over a minute, nobody attached — go and look
CUT OFF   the session is gone without an exit code (OOM, kill, docker rm)
FAILED    exited non-zero
FINISHED  exited zero — which does not mean it wrote anything
```

`FINISHED` is about the process, the result is about the artefact: `smate list`
reports them separately, because an agent that exits cleanly having written
nothing is the usual outcome of a weak prompt.

An interactive harness does not exit when it is done — it writes its report and
waits for the next line, so it stays `WORKING`, then `SLEEP`. The next `run` puts
such a session down by itself, provided the result is written, the session has been
silent for over a minute and nobody is attached; otherwise it refuses and leaves the
choice to you (`smate attach`, `smate stop`). A role whose command ends by itself —
`cmd: claude -p` — reaches a real `FINISHED` instead.

## Project config

`<repo>/.smate.yml`, committed along with the project:

```yaml
image: php            # a library name, or any docker reference
secrets:              # paths cut from the snapshot before the container starts
  - secret.txt
  - creds
mounts:               # host paths made visible inside the container, alongside it
  - fixtures/prod.dump:/workspace/fixtures/prod.dump
  - /home/me/.config/thing/token:/home/smate/.config/thing/token
```

A name from the image library resolves to `smate/<name>:latest`; anything else
is passed to docker as written, so `ubuntu:24.04` keeps working.

Paths under `secrets` never reach the container and cannot come back from it:
their changes are excluded from the patch, and `apply` lists them separately.
The files in the working repository are left untouched.

Entries are literal paths and directories. A mask (`*.env`) and a path that is
not in the snapshot are both refused at `start` — a denylist that matches nothing
protects nothing, while its author is sure of the opposite.

`mounts` is the way in for a file `.gitignore` keeps out of the snapshot
entirely — `git archive` never sees it, so `secrets` has nothing to cut and
nothing to report. Each entry is `host:container`, docker's own shape. A
relative host path is read from the repository root, not the snapshot, so it
still resolves to a file the snapshot doesn't have; the container side must be
absolute and cannot be `/workspace` itself. A missing host path is refused at
`start`, the same as a bad `secrets` entry.

An entry lands on the container one of two ways, decided by the container
side of the colon. Outside `/workspace` it is bind mounted, live, the same as
a harness's own state directory. Under `/workspace` it is copied into the
snapshot instead, once, before the container starts: `/workspace` is itself a
bind mount (the snapshot), and a second bind mount nested inside it is what
Docker Desktop's virtiofs backend refuses to create — the same entry works on
Linux, but fails there with a `mountpoint is outside of rootfs` error. A copy
sidesteps the nested mount and, unlike a live mount, does not track further
host-side edits made after `start`.

Whatever lands under a mount, bind or copy, is invisible to `apply` — it sits
outside the sandbox's own git, so it is neither cut nor reported, and it does
not travel back. A bind mount manages that by sitting outside the sandbox's
working tree entirely; a copy manages it by being written to the sandbox's
`.git/info/exclude` at `start`, the same way the task's own artefact directory
is kept out of `git add -A`. If you'd rather it went through the same
cut-and-report bookkeeping `secrets` gives you, put the path under `secrets`
too once it's in the snapshot some other way; `mounts` itself is one-way, in
only.

## What is checked before an import

Whatever the agent left uncommitted in the sandbox is committed there first, so
work does not have to survive somebody remembering to commit it. The series is
then produced against the baseline recorded at `start`, not against whatever
history the container ended up with.

The whole patch is refused if it escapes the repository root, touches anything
inside `.git/` or `.smate/`, contains a symlink, or covers a path listed in
`secrets`. An added `+x` bit is reported as a warning.

Beyond the content, the working copy has to be clean — an import must not mix
with unfinished work. The branch itself starts at the commit the snapshot was
taken from, wherever the repository stands now. Otherwise nothing is imported and
the task becomes `REJECTED` — you can go back into `shell`, fix it and apply
again.

Applying the same task twice is that same loop, and it does not ask you to delete
the branch first: several rounds of work in one sandbox end up under one branch
name, so `apply` replaces its own previous import. Only its own — smate remembers
where it left the branch, and a branch that has moved since, or one it never
created, stops the import instead. Those commits exist nowhere else, while
everything smate imported can be produced from the sandbox again. If what is on
such a branch is spent, delete it yourself and apply again.

Note that the branch is rebuilt rather than added to: a branch already pushed
needs a force-push after the next `apply`.

A successful import leaves the repository standing on that branch — the next
thing to do with it is usually to look at it, and pushing is manual anyway. A
refused import puts you back where you were.

## Task statuses

```
start  → ACTIVE
apply  → DONE      (success)
       → REJECTED  (a check or a guard failed)
clean  → CLEANED
```

`ACTIVE` and `REJECTED` are protected from bulk `clean`: the former are still in
progress, the latter can still be finished. Without an `<id>` only `DONE` tasks
are touched — and with `--purge`, `CLEANED` ones as well, which have neither
container nor workspace left to protect. An explicit `<id>` overrides all of
that and cleans any task, `ACTIVE` included.

Plain `clean` drops the container and the snapshot but keeps the record of the
task; `--purge` removes the record too, and with it the memory of where the
import left the branch.

## Limitations

`secrets` understands exact paths only, no globs.

A task container runs as your own uid and gid (`--user`), not as root. That is
what keeps the snapshot writable by the agent and readable by you afterwards — and
agent CLIs refuse to work unattended as root anyway. Consequences worth knowing:
your uid usually has no entry in the image's `/etc/passwd`, so `HOME` is set
explicitly to `/home/smate` and `whoami` inside will complain; and if you run
smate itself as root, the container is root again and the refusal comes back.

The rest of the hardening is not done: the container still has full capabilities
and a writable root filesystem, and its outbound network is docker's default —
open. A harness needs to reach its provider, and telling that traffic from
everything else needs an allowlist and a proxy, which is not built.

Containers are capped at 1 CPU, 512 MB and 512 processes (`limits` in
`~/.smate/config.yml`). 512 MB is tight for a dependency build: it ends as
`CUT OFF`, killed by the kernel, which is worth remembering before looking for a
bug. Disk is not capped — that needs a storage driver with quota support — so
space is freed only by `clean`.

`SLEEP` catches a run that hangs or waits in silence, not every run that waits: a
harness that spins a progress indicator keeps printing, and the mark never
appears. Telling "waiting for an answer" from "thinking" needs per-harness
screen rules or lifecycle hooks, and neither is worth its price — `attach` and
looking is.

Inside `attach` the keys go to the agent: `Ctrl-B D` detaches, `Ctrl-C` kills it.

Keys are passed as environment variables, so they show up in `docker inspect`.
Harness state under `~/.smate/harness/` holds long-lived credentials in formats
smate cannot recognise, so the leak scan does not cover them; the directory is
mounted outside `/workspace`, which keeps it out of a patch by accident.
