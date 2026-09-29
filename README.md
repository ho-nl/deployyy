# deployyy

deployyy runs your Magento 2, Next.js / GraphCommerce and Laravel projects on
the [deployyy platform](https://deployyy.app).
Each git branch becomes an environment. Preview environments scale to zero
when idle. Your repository contains only code and a short caller workflow —
the platform derives all other data.

This repository is the public part of the platform. It contains the build
workflows and the Dockerfile recipes.

## Get started

### Step 1 — Link your repository

Install the deployyy GitHub App on your repository:
**[github.com/apps/deployyy-platform](https://github.com/apps/deployyy-platform)**.

The platform then connects your repository and prepares your environments.
Contact us if your organization is not on the platform yet.

### Step 2 — Add the `COMPOSER_AUTH` secret

Add one Actions secret to your repository: `COMPOSER_AUTH`. Its value is
the content of your `auth.json` (your Magento Marketplace or Packagist
keys). Composer reads this variable natively.

The platform does not use organization secrets. Your keys stay in your
repository.

### Step 3 — Add the build workflows

Add two caller workflows to your repository:

```yaml
# .github/workflows/preview-build.yaml — builds every branch
name: Preview build
on:
  push:
    branches-ignore: [main]
concurrency:
  group: preview-build-${{ github.ref_name }}
  cancel-in-progress: true
jobs:
  build:
    uses: ho-nl/deployyy/.github/workflows/magento2-build.yml@main
    secrets: inherit
```

```yaml
# .github/workflows/build.yaml — builds main
name: Build
on:
  push:
    branches: [main]
concurrency:
  group: build-${{ github.ref_name }}
  cancel-in-progress: false
jobs:
  build:
    uses: ho-nl/deployyy/.github/workflows/magento2-build.yml@main
    secrets: inherit
```

### Step 4 — Push a branch

Push a branch. The workflow builds two images and pushes them to
`ghcr.io/<your-repo>` with commit tags (`php-fpm-<sha7>`, `nginx-<sha7>`).
The operator finds the tags and deploys your preview environment at
`https://<branch>.<project>.deployyy.app`. CI does not deploy — the
operator does.

## Configuration

Your repository contains no platform configuration file. The platform
holds your project configuration:

- The release line comes from your `composer.json`. You do not declare it.
- Your service stack (database, search, queue, cache) is part of your
  project configuration on the platform.
- The static-content locales come from the repository variable
  `DEPLOYYY_MAGENTO_LOCALES`. The platform sets this variable from your
  project configuration. The default is `en_US`. Do not edit the variable
  by hand — the platform converges it.

To change your configuration, contact the platform team. A dashboard for
self-service configuration is planned.

### GraphQL POST cache (Magento)

GraphCommerce sends its GraphQL queries as POST requests. The Magento recipe
adds [`GraphCommerce_GraphQlVarnishPostCache`](https://github.com/graphcommerce-org/magento2-graphcommerce_graphqlvarnishpostcache),
so Varnish caches a POST `/graphql` request that has an `X-Document-ID`
header. The module also sets `graphql/session/disable` to `1` by default,
which POST caching needs. The build adds the module to `app/code` and
enables it in `app/etc/config.php`; your `composer.lock` does not change.

- A project that already has the module keeps its own copy.
- A project whose `config.php` disables the module keeps it disabled.
- To leave the module out, set the repository variable
  `DEPLOYYY_GRAPHQL_POST_CACHE` to `0`.

## How the build works

1. The workflow reads `composer.json` and finds your release line.
   Example: `mage-os/product-community-edition: 3.*` gives `mageos-3` — Mage OS
   3.x releases are security releases of one line (same Magento 2.4.9 core and
   service versions), so the match is major-level.
   You do not declare the line.
2. It builds with the platform recipe for that line from
   [`recipes/`](recipes/): a Dockerfile plus `.platform/` support files.
   When we improve a recipe, your project gets the improvement on its
   next build.
3. You can replace the recipe. A `Dockerfile` in your repository replaces
   the full recipe Dockerfile. A file in your `.platform/` directory
   replaces only that one file.
4. Project patches are applied at install time. Put your
   [vaimo/composer-patches](https://github.com/vaimo/composer-patches) files
   in a `patches/` directory at the repository root (and point
   `extra.patches-search` or `extra.patches` sources at it). The recipe
   copies that directory into the build context **before** `composer
   install`, so the plugin can apply the patches. A project without a
   `patches/` directory builds unchanged.

## Recipes

| Line | Dir | PHP | Status |
|---|---|---|---|
| `mageos-3` | [`recipes/mageos-3/`](recipes/mageos-3/) | 8.4 | ✅ validated live (3.2.0, 3.4.0) |
| `magento-249` (Magento Open Source 2.4.9) | [`recipes/magento/`](recipes/magento/) | 8.4 | 🟡 image builds and starts; not validated live |
| `magento-248` (2.4.8) | [`recipes/magento/`](recipes/magento/) | 8.4 | 🟡 image builds and starts; not validated live |
| `magento-247` (2.4.7) | [`recipes/magento/`](recipes/magento/) | 8.3 | 🟡 image builds and starts; not validated live |
| `magento-246` (2.4.6) | [`recipes/magento/`](recipes/magento/) | 8.2 | 🟡 image builds and starts; not validated live |
| Next.js / GraphCommerce | [`recipes/nextjs/`](recipes/nextjs/) | Node 20/22/24 | 🟡 validated locally on Next 14, 15 and 16; not validated live |
| Laravel | [`recipes/laravel/`](recipes/laravel/) | 8.2–8.4 | see below |

The Magento Open Source lines share one recipe. It handles the known traps
of these releases:

- **Cache types** come from the installed modules (every `etc/cache.xml`),
  not from the version. A cache type that is missing from `env.php` is
  disabled without a warning.
- **`queue_poison_pill`** has no primary key in core. MySQL Group
  Replication refuses every write to such a table, and Magento writes to it
  on every `setup:upgrade`. When no module in your project declares a
  primary key for it, the build adds the module
  `Deployyy_QueuePoisonPillPk`, and the next `setup:upgrade` adds the key.
- **composer-patches**: the `patches/` directory is in the build before
  `composer install` (see step 4 above).
- **Minification**: the build records whether it deployed minified JS and
  CSS (`app/etc/static_build.php`), and `env.php` sets the runtime
  minification to match. A database with other minification settings cannot
  make the shop ask for files that are not in the image.

A line that has no recipe stops the build with a clear message that shows
the supported lines. Adobe Commerce has no recipe. Your repository can ship
its own Dockerfile.

## Image scanning, SBOM and provenance

Every build pushes its images **by digest** first and scans each one with
[trivy](https://trivy.dev):

- A **critical** vulnerability that has a fixed version **fails the build**.
- Every other finding is **reported**: the job summary shows the counts and
  the critical and high findings, and the full report is a run artifact
  (`trivy-<image>`).
- To accept a finding, add its ID to a `.trivyignore` file at the root of
  your repository, with a comment that gives the reason.

The build tags the images (the tags the platform deploys) only when every
image of the build passed. Each image carries an SBOM and a provenance
attestation. To read them:

```sh
docker buildx imagetools inspect ghcr.io/<owner>/<repo>:<tag> --format '{{json .SBOM}}'
docker buildx imagetools inspect ghcr.io/<owner>/<repo>:<tag> --format '{{json .Provenance}}'
```

## Unprivileged images

The images from `recipes/magento` and `recipes/nextjs` run as a non-root
user, need no `sudo` and no capabilities, and so can run under the
Kubernetes Pod Security Standard `restricted`. See
[`docs/PSS-RESTRICTED.md`](docs/PSS-RESTRICTED.md) for the details and for
what the platform must change to enforce it.

## Next.js and GraphCommerce

A Next.js or GraphCommerce project on the platform builds with
`nextjs-build.yml` or `graphcommerce-build.yml` (the same build):

```yaml
# .github/workflows/build.yaml — builds every branch
name: Build
on: push
concurrency:
  group: build-${{ github.ref_name }}
  cancel-in-progress: true
jobs:
  build:
    uses: ho-nl/deployyy/.github/workflows/graphcommerce-build.yml@main
    secrets: inherit
```

The workflow builds two images and pushes them to `ghcr.io/<your-repo>`:

- `sha-<sha7>`: the Next.js
  [standalone server](https://nextjs.org/docs/app/api-reference/config/next-config-js/output),
  `node server.js` on port 3000;
- `sha-<sha7>-cache-seed`: the pages that `next build` prerendered. The
  platform loads them into the shared page cache before the release serves,
  so the first visitors get cached pages.

Requirements:

- a `build` script in `package.json`, or `next` alone;
- a route that answers `GET /api/health` with HTTP 200 (or another health
  path set in your project's platform settings). The platform uses it to
  check that a release is ready; the build warns when `/api/health` is
  missing;
- Node from `.nvmrc`, `.node-version` or `engines.node` (20, 22 or 24; the
  default is 22). The package manager follows your lockfile (npm, Yarn or
  pnpm).

Build-time settings: repository variables named `NEXT_PUBLIC_*` or `GC_*`,
and secrets named `GC_*` or `NEXT_PUBLIC_*`, are available to `next build`.
Other variables and secrets are not. Runtime settings come from the platform.

### Shared page cache

Your project carries no cache code and no cache configuration. The build
wraps your `next.config` (`.js`, `.mjs`, `.ts` or `.mts`) and adds:

- `cacheHandler`: [`graphcommerce/cache-handler.mjs`](graphcommerce/cache-handler.mjs),
  which keeps ISR pages and fetch results in the shared cache volume that the
  platform mounts (`CACHE_DIR`). One revalidation updates every instance.
  Without `CACHE_DIR` (local development) the cache is private to the
  process;
- `cacheMaxMemorySize: 0`, so no instance keeps an old copy in memory;
- `output: 'standalone'`, when you set no `output`;
- the commit as the build ID, when you set no `generateBuildId`.

Images (`/_next/image`) are not in this cache: the content delivery network
keeps them.

If your `next.config` sets its own `cacheHandler`, the build keeps it and
prints a warning. `output: 'export'` stops the build: the platform runs a
server.

A `Dockerfile` in your repository replaces the recipe; a file in
`.platform/` replaces the recipe's file of the same name.

## Background

The [deployyy operator](https://github.com/ho-nl/deployyy-operator)
manages environments as Kubernetes resources: branch = environment,
previews with scale-to-zero, database migrations as a checkpointed state
machine, and a live development mode per preview. This repository is the
public edge of that platform.

## Laravel

A Laravel project on the platform builds with `laravel-build.yml`:

```yaml
# .github/workflows/build.yaml — builds every branch
name: Build
on: push
concurrency:
  group: build-${{ github.ref_name }}
  cancel-in-progress: true
jobs:
  build:
    uses: ho-nl/deployyy/.github/workflows/laravel-build.yml@main
    secrets: inherit
```

The PHP version comes from `require.php` in `composer.json` (8.2–8.4). The
recipe (`recipes/laravel`) builds two images, `php-fpm-<sha>` and
`nginx-<sha>`, including your Vite/Mix assets when there is a `package.json`.
A `Dockerfile` in your repository replaces the recipe; a file in `.platform/`
replaces the recipe's file of the same name.

The platform configures the application through environment variables —
`DB_*`, `REDIS_*`, `APP_URL`, `APP_KEY`, mail and S3 — so do not commit a
`.env`. Your own variables are managed per environment and win over the
platform's. Migrations run before a release serves (`php artisan migrate
--force` unless the project declares its own release commands); a failed
migration keeps the previous release online.
