# Base environment image every smate stack inherits from: shell, git, editor,
# tmux and the three agent CLIs. Node is here because all three ship as npm
# packages. tmux is what makes an unattended run re-enterable.
FROM ubuntu:24.04

# tmux picks its UTF-8 mode from the locale, and a bare ubuntu sets no LANG: it
# would take the pane for ASCII-only and write an underscore for every glyph an
# agent draws, corrupting attach and capture-pane alike.
ENV LANG=C.UTF-8

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      git ca-certificates curl nano tmux \
 && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && rm -rf /var/lib/apt/lists/*

RUN npm install -g \
      @anthropic-ai/claude-code \
      @openai/codex \
      opencode-ai \
 && npm cache clean --force

# A task runs as the host user (chosen at `docker run --user`), and that uid has
# no entry in /etc/passwd here — hence a world-writable home to write into.
RUN mkdir -p /home/smate \
 && chmod 777 /home/smate
