# Node.js stack. Node 22 itself comes from smate/base (the agent CLIs need it),
# so this layer only adds the package managers.
FROM smate/base:latest

RUN corepack enable \
 && corepack prepare yarn@stable pnpm@latest --activate
