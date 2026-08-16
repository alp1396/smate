# Written by the init role and never built: there is no Docker inside the
# sandbox, so this file is unverified. Build it before trusting it:
#
#   docker build -f smate.Dockerfile -t smate/smate:latest .
#
# It is the bundled `go` stack plus make, because the project drives its build
# and tests through the Makefile. Based on smate/base:latest, so it keeps tmux
# and the agent CLIs and can host a detached run — build that image first
# (`smate build base`).
FROM smate/base:latest

# TARGETARCH is filled in by BuildKit, so the same file works on amd64 and arm64.
ARG GO_VERSION=1.24.13
ARG TARGETARCH

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz

ENV PATH="/usr/local/go/bin:${PATH}"

RUN apt-get update \
 && apt-get install -y --no-install-recommends make \
 && rm -rf /var/lib/apt/lists/*
