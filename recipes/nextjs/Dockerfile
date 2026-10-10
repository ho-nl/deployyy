# syntax=docker/dockerfile:1.7
# PLATFORM BUILD RECIPE — Next.js and GraphCommerce. Consumed by the reusable
# nextjs-build workflow (graphcommerce-build calls the same workflow), which
# overlays it into a project checkout UNLESS the project ships its own
# Dockerfile (a repo-local Dockerfile wins wholesale; repo-local .platform/*
# files win per file).
#
# The image contract the deployyy GraphCommerceApp platform runs
# (deployyy-operator internal/platform/graphcommerce):
#   - target runtime: a Next.js standalone server, `node server.js` on :3000,
#     uid/gid 1001, readiness on GET /api/health, the shared ISR cache
#     volume at CACHE_DIR=/cache (set by the operator, absent = pod-local);
#   - target cache-seed: busybox + /seed/cache-seed.tar.gz, pushed as
#     `<app tag>-cache-seed`. The operator's per-build seed Job extracts it
#     onto the cache volume as uid 1001 (seedjob.go), so a release starts warm.
#
# Both images run as a non-root numeric user with no setuid helpers, so they
# run under Pod Security Standard `restricted` (docs/PSS-RESTRICTED.md).
#
# Build-args (the workflow resolves them): NODE_VERSION from package.json
# `engines.node` or .nvmrc (20, 22 or 24); DEPLOYYY_BUILD_ID, the commit sha,
# which becomes the Next build id unless the project sets generateBuildId.
# Build-time environment
# (NEXT_PUBLIC_* / GC_* repository variables and GC_* / NEXT_PUBLIC_*
# secrets) arrives as the BuildKit secret `build-env`: it is sourced for the
# build and never stored in a layer.
ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-bookworm-slim AS builder
ENV NEXT_TELEMETRY_DISABLED=1 COREPACK_ENABLE_DOWNLOAD_PROMPT=0 CI=1
WORKDIR /app
RUN corepack enable
# The whole tree first: workspaces, patch-package patches, .yarn/ releases
# and postinstall code generation (GraphCommerce) all need more than the
# lockfile, and a partial copy fails in ways that are hard to see.
COPY . .
# The platform cache handler and the Next config wrapper (docs in each file).
RUN cp .platform/cache-handler.mjs deployyy-cache-handler.mjs \
    && node .platform/inject-next-config.mjs
# The commit sha as the Next build id (inject-next-config.mjs).
ARG DEPLOYYY_BUILD_ID=""
RUN --mount=type=secret,id=build-env,required=false \
    set -eu; \
    if [ -f /run/secrets/build-env ]; then set -a; . /run/secrets/build-env; set +a; \
      echo "Build variables: $(sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' /run/secrets/build-env | tr '\n' ' ')"; fi; \
    if [ -f pnpm-lock.yaml ]; then pm=pnpm; pnpm install --frozen-lockfile; \
    elif [ -f yarn.lock ]; then pm=yarn; \
      if [ "$(yarn --version | cut -d. -f1)" = "1" ]; then yarn install --frozen-lockfile; \
      else yarn install --immutable; fi; \
    elif [ -f package-lock.json ]; then pm=npm; npm ci; \
    else pm=npm; npm install; fi; \
    if node -e 'process.exit(require("./package.json").scripts?.build ? 0 : 1)'; then \
      "$pm" run build; else npx --no-install next build; fi; \
    test -f .next/standalone/server.js || { \
      echo "FATAL: next build produced no .next/standalone/server.js."; \
      echo "With outputFileTracingRoot set, server.js is elsewhere. Add a Dockerfile for that layout to the application directory."; \
      exit 1; }
# Readiness is GET /api/health by default (operator compose.go; the path and
# port are project settings there). An app without the route it is probed on
# builds, but its pods never become ready.
RUN if [ -e .next/server/pages/api/health.js ] || [ -e .next/server/app/api/health/route.js ]; then \
      echo "Health route: /api/health found"; \
    else \
      echo "WARNING: no /api/health route found. A release serves traffic only after GET /api/health returns HTTP 200."; \
      echo "WARNING: add pages/api/health.(js|ts) or app/api/health/route.(js|ts), or set another health path in the project settings."; \
    fi
# The prerendered routes, in the cache handler's format, for the seed image.
RUN node .platform/seed-cache.mjs && mkdir -p public

# The cache seed: extracted onto the shared cache volume by the operator's
# per-build Job, which runs `sh`, `tar`, `sed` and `head` as uid 1001.
FROM busybox:1.37 AS cache-seed
COPY --from=builder --chown=1001:1001 /app/cache-seed.tar.gz /seed/cache-seed.tar.gz
LABEL app.deployyy.pod-security="restricted"
USER 1001:1001

FROM node:${NODE_VERSION}-bookworm-slim AS runtime
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
WORKDIR /app
# The server needs node only: npm, npx and corepack go (fewer packages to
# patch, fewer findings in the image scan).
RUN groupadd --system --gid 1001 nextjs \
    && useradd --system --uid 1001 --gid nextjs --no-create-home --shell /usr/sbin/nologin nextjs \
    && rm -rf /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/corepack \
              /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack
COPY --from=builder --chown=1001:1001 /app/.next/standalone ./
COPY --from=builder --chown=1001:1001 /app/.next/static ./.next/static
COPY --from=builder --chown=1001:1001 /app/public ./public
# Numeric, so the kubelet can verify runAsNonRoot without a securityContext.
LABEL app.deployyy.pod-security="restricted"
USER 1001:1001
EXPOSE 3000
CMD ["node", "server.js"]
