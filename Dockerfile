# syntax=docker/dockerfile:1.7
#
# icon-sets-sync image: the Go binary, plus the tools it shells out to.
#
# Every pack's sync-upstream.yml builds this from a checkout of this repository
# and runs, unchanged since the Python image:
#
#   docker run --rm -v "$PWD/..:/work" -v "<toolkit>/config:/app/config" \
#     <image> sync --pack="$PACK_SLUG" [--force]
#
# so three things are load-bearing: the ENTRYPOINT takes `sync ...` as its
# arguments, the pack configs are read from /app/config, and /work holds the
# pack checkouts (each config's pack_root is /work/<pack>).
#
# Build:    docker build -t icon-sets-sync .
# Run:      docker run --rm -it -v "$PWD/..:/work" icon-sets-sync menu
# Compose:  see docker-compose.yml + Makefile.

# ---------------------------------------------------------------- build stage
FROM golang:1.26-trixie AS build

WORKDIR /src

# Dependencies first, so a source-only change reuses the module layer.
COPY src/go.mod src/go.sum ./
RUN go mod download && go mod verify

COPY src/ ./

ARG VERSION=dev
# CGO off: the binary is static, so the runtime stage needs no Go and no libc
# match. -buildvcs=false because .git is excluded from the build context.
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X github.com/ichava/icon-sets-sync/src/internal/version.version=${VERSION}" \
        -o /out/icon-sets-sync ./cmd/icon-sets-sync

# -------------------------------------------------------------- runtime stage
FROM debian:trixie-slim AS runtime

# What the binary shells out to:
#   git        -- branch, commit and push in the pack repos
#   gh         -- open the pull request
#   nodejs/npm -- `npm pack <pkg>@<version>`, the npm source strategy
#   ca-certs   -- TLS to the registries, GitHub and unicode.org
#   curl, gpg  -- only to add GitHub's apt repository for gh
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
        git ca-certificates curl gpg \
        nodejs npm \
 && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
 && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
 && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends gh \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/icon-sets-sync /usr/local/bin/icon-sets-sync

# imt is the short alias. ichava-maintainer-toolkit was the Python console
# script; it stays as a symlink so a script calling it by name keeps working,
# and the binary prints a deprecation notice when invoked under that name.
RUN ln -s icon-sets-sync /usr/local/bin/imt \
 && ln -s icon-sets-sync /usr/local/bin/ichava-maintainer-toolkit

# The config bundle ships in the image; the pack workflows bind-mount their
# own checkout's config/ over it. ICHAVA_CONFIG_DIR is the config_dir setting.
COPY config /app/config
ENV ICHAVA_CONFIG_DIR=/app/config

# /work is where the pack checkouts are mounted.
WORKDIR /work

ENTRYPOINT ["icon-sets-sync"]
CMD ["menu"]
