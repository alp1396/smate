# Go stack. Build smate/base:latest first.
FROM smate/base:latest

# TARGETARCH is filled in by BuildKit, so the same file works on amd64 and arm64.
ARG GO_VERSION=1.24.13
ARG TARGETARCH

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz

ENV PATH="/usr/local/go/bin:${PATH}"
