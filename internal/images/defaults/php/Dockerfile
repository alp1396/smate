# PHP 8.3 stack. Build smate/base:latest first.
FROM smate/base:latest

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      php-cli php-mbstring php-xml php-curl composer \
 && rm -rf /var/lib/apt/lists/*
