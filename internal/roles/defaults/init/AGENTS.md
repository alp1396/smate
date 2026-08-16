# Role: init

## Order of reading

1. The note for this run, if the prompt named one. It narrows what to look at
   and wins over anything else here.
2. `smate.project.md` in the repository root, if it is already there — a
   previous init wrote it, and possibly a human edited it since.
3. The repository itself: its README, its build and dependency files, its
   entry points, its CI configuration.

What you are standing in is a snapshot: the tracked files of one commit, minus
anything the project listed under `secrets`. There is no git history, no
remotes, no `.env`, no untracked scratch files. Say what you saw, not what you
assume is normally there.

## What to do

Work out the top-level shape of the project and nothing below it. Somebody who
has never seen this repository should be able to read what you leave and know
where to start:

- what the project is, and what it is for;
- its main modules or subsystems, and how they depend on each other;
- how the application or system is started;
- how it is tested, and with what command;
- the stack: languages, runtimes and their versions, the package manager, the
  notable frameworks and services it needs.

Prefer what the repository states over what you infer. When something is only a
guess, mark it as one — a plausible wrong command is worse than an admitted gap.

## What to leave behind

**`smate.project.md`** in the repository root. This is the durable result: it is
committed and every other role reads it before starting. One section per
question above. If the file was already there, update it rather than replacing
it wholesale — keep what still holds and note what changed.

**`smate.Dockerfile`** in the repository root — an image the project can be run
and tested on. Before writing it, decide whether the project can be containerised
at all, and let the answer be no when it is no:

- a desktop or mobile application, or anything needing a display or hardware;
- a toolchain that is not available on Linux — Xcode and iOS, .NET Framework,
  Windows-only SDKs;
- anything requiring the host kernel, privileged devices, USB, or systemd;
- a licensed toolchain that cannot be installed unattended.

When one of these applies, **write no Dockerfile at all** and say in
`smate.project.md` which of them it was. A file that cannot work is worse than
its absence: somebody will try to build it.

When you do write it, base it on `smate/base:latest` if it is meant to be run as
a smate task image — that is where tmux and the agent CLIs come from, and an
image without tmux cannot host a detached run. Keep the build unattended and
architecture-neutral (`ARG TARGETARCH` where a download needs it). Open the file
with a comment saying it was written by init and never built: there is no Docker
inside this sandbox, so **you cannot verify it**. Name the build command in that
comment, so the first reader knows what to try:

```
docker build -f smate.Dockerfile -t smate/<project>:latest .
```

**`.smate/init.result.md`** last, when the rest is written:

- what you wrote, and what you deliberately did not — above all, whether there
  is a Dockerfile and why;
- what you could not determine from the snapshot alone;
- what a reader should verify first, the unbuilt image included.

The file being there is what marks the run as having produced a result. Do not
create it up front and fill it in later.
