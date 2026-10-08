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

### Step 2 — Put your variables and secrets in GitHub

GitHub is the only place your project's variables and secrets live. Add them
under your repository's **Settings → Secrets and variables → Actions**:
secrets for keys and passwords, variables for everything that may be read
back (a public address, a feature flag). Each one reaches both the build and
the running shop — Magento's `config.php` / `env.php` read them with
`getenv()`.

- **Per environment**: create a GitHub Environment named after the branch
  (**Settings → Environments**, for example `main` or `staging`) and give it
  its own variables and secrets. They override the repository's for that
  branch. A branch without such an Environment uses the repository's values.
- **Composer credentials**: a secret named `COMPOSER_AUTH` holding the
  content of your `auth.json` (Magento Marketplace or Packagist keys).
  Mage-OS on public packages needs none.
- Changes reach the build and the running shop **with the next build** of the
  branch: push, or re-run the latest build. A redeploy or rollback without a
  build keeps the values the last build delivered.

The platform does not use organization secrets of its own, and it does not
store your values anywhere you have to manage: the console lists their names,
read-only, and links back here.

### Step 3 — Add the build workflows

Add two caller workflows to your repository. They hand over every variable
and secret in one go, without naming any — so they are the same in every
project, and you never edit them when you add a value:

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
    permissions:
      contents: read
      packages: write
      actions: read
      id-token: write
    with:
      vars: ${{ toJSON(vars) }}
    secrets:
      all: ${{ toJSON(secrets) }}
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
    permissions:
      contents: read
      packages: write
      actions: read
      id-token: write
    with:
      vars: ${{ toJSON(vars) }}
    secrets:
      all: ${{ toJSON(secrets) }}
```

Why this shape:

- `secrets: all: ${{ toJSON(secrets) }}` passes every secret by one declared
  name, which GitHub allows from any organization. `secrets: inherit` only
  reaches a reusable workflow in the caller's own organization, so a
  repository outside `ho-nl` would build without its secrets.
- `vars` passes every variable the same way.
- `permissions`: `packages: write` pushes the images, `actions: read` looks up
  the GitHub Environment named after the branch, and `id-token: write` lets
  the build prove to Deployyy which repository and branch it is — that is how
  the values reach the running shop without a stored credential.
- The central jobs ask for no permissions of their own: they take what the
  caller grants. A caller that grants less still builds, and the build log
  names the missing grant as a warning (`actions: read`: no per-branch
  GitHub Environment; `id-token: write`: no values delivered). A caller from
  before 2026-10-07 (`secrets: inherit`, or `COMPOSER_AUTH` by name, no
  `permissions`) builds the same way, with a warning that shows the lines
  above to put in its place.

What the build does with them: every secret is masked in the log, every
value becomes an environment variable of the build steps and the BuildKit
secret `build-env` (a repository `Dockerfile` reads it with
`RUN --mount=type=secret,id=build-env`), and after a successful build the
same set is delivered to the branch's environment on Deployyy, where the
console lists the names only. If Deployyy cannot take them — the branch has
no environment, the project is not connected — the build says so as a warning
and does not fail; the environment keeps the values it had.

Limits, honestly:

- A GitHub Environment's values reach the build because the build job runs
  *in* that Environment. Its protection rules (required reviewers, wait
  timers, branch rules) therefore apply to the build of that branch.
- GitHub never shows a secret's value again, so Deployyy only ever receives
  the values from a build; there is no "sync now" without one.
- A preview environment that is still being created when its first build
  finishes is retried for about two minutes; a later environment gets its
  values from the next build.
- The calling job (`uses:`) cannot run in a GitHub Environment itself, so
  `toJSON(secrets)` there carries the repository's and organization's
  secrets only; the Environment's arrive because the build job inside the
  central workflow runs in it. That lookup needs `actions: read`.
- Organization secrets and variables arrive only when the organization
  grants them to the repository. Builds GitHub runs without secrets — a pull
  request from a fork, a Dependabot push — build without them and deliver
  nothing a secret held.
- Names must be shell-variable shaped (`A_Z0_9`, not starting with a digit);
  others are left out with a warning. GitHub stores secret names in upper
  case. A name that is both a variable and a secret is a secret. Names
  starting with `DEPLOYYY_` are the platform's build settings and do not reach
  the running shop; names the platform sets for the shop itself (database,
  cache) keep the platform's value.

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
- PHP is your release line's default (2.4.6: 8.2, 2.4.7: 8.3, 2.4.8,
  2.4.9 and Mage-OS 3: 8.4) unless you choose another version the line
  supports in the console. The platform then sets the repository variable
  `DEPLOYYY_PHP`, and the build tags its images with the version
  (`php-fpm-8.3-<sha7>`). After you change it, push or re-run the latest
  build: the environment keeps its current build until the new one exists.
  PHP 8.5 (Adobe's tested version for 2.4.9 and Mage-OS 3) builds on those
  lines, but the console offers it only after its live trial.
  If your repository has its own `Dockerfile`, it receives the version as the
  `PHP_VERSION` build argument.
- The static-content locales come from the repository variable
  `DEPLOYYY_MAGENTO_LOCALES`. The platform sets this variable from your
  project configuration. The default is `en_US`. Do not edit the variable
  by hand — the platform converges it.

To change your configuration, contact the platform team. A dashboard for
self-service configuration is planned.

### Application in a subfolder (monorepo)

Your application does not have to be at the root of the repository. When it
lives in a folder, say `src` or `apps/web`, the project's root directory names
that folder. The platform finds it when you connect a repository whose root
holds no application (it looks one folder down), and sets the repository
variable `DEPLOYYY_ROOT_DIR` from it. Do not edit the variable by hand: the
platform converges it, and deletes it when the application is at the root.

The build then reads everything from that folder: `composer.json` and
`composer.lock`, `package.json`, `.nvmrc` / `.node-version`, your
`Dockerfile`, `.platform/`, `deployyy.json`, `patches/`, `.trivyignore` and
the Varnish VCL files. The folder is the docker build context, so nothing
above it reaches the image. Wherever this README says "the root of your
repository", read "the application's folder". Your caller workflow does not
change.

The folder must be a relative path inside the repository (no leading `/`, no
`.` or `..`), must exist in the commit being built and must not be a
symbolic link. Otherwise the build stops and says which rule it broke. Unset
or empty is the repository root, exactly as before.

### Varnish VCL (Magento)

The page cache runs the platform's VCL. To change it, commit a file at the
root of your repository:

- `varnish-snippet.vcl` is added to the platform's VCL. A `sub vcl_recv { … }`
  there runs before the platform's and falls through to it.
- `varnish.vcl` replaces the platform's VCL. Its backend is host
  `magento-web`, port `8080`, and its purge ACL must admit `10.0.0.0/8`,
  `172.16.0.0/12` and `192.168.0.0/16`.

The recipes copy the file into the image with the rest of the repository. The
platform reads it from the build an environment serves and compiles it with
the same Varnish first. A VCL that does not compile is never used: the one
before it keeps serving, and the console says why. The recipes add no Varnish
or GraphQL module to your project; caching behaviour that needs a Magento
module is the project's own choice, in its `composer.json`.

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
   in a `patches/` directory at the repository root and declare each one in
   `extra.patches` of `composer.json`:

   ```json
   "extra": {
       "patches": {
           "mage-os/magento2-base": {
               "What the patch fixes": "patches/the-fix.patch"
           }
       }
   }
   ```

   The recipe copies that directory into the build context **before**
   `composer install`, so the plugin can apply the patches, and then
   **fails the build** for every `*.patch` file in `patches/` that is not in
   the installed code (`.platform/check-patches.php`). Prefer `extra.patches`
   over the `extra.patches-search` folder scan: the scan silently skips a
   patch whose header names no installed package, and every patch for a
   branch install (`dev-main`) unless the header carries `@version *` — the
   build said "Nothing to patch" and shipped the code unpatched. A file that
   belongs in `patches/` but must not be applied carries `@skip` in its
   header. A project without a `patches/` directory builds unchanged.

## Recipes

| Line | Dir | PHP | Status |
|---|---|---|---|
| `mageos-3` | [`recipes/mageos-3/`](recipes/mageos-3/) | 8.4 (8.5: not validated yet) | ✅ validated live (3.2.0, 3.4.0) on 8.4 |
| `magento-249` (Magento Open Source 2.4.9) | [`recipes/magento/`](recipes/magento/) | 8.4 (8.5: not validated yet) | 🟡 image builds and starts; not validated live |
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
  `composer install`, and a patch file there that did not reach the
  installed code fails the build (see step 4 above).
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

- A **critical** vulnerability that has a fixed version is flagged. It
  **fails the build** once your repository sets the Actions variable
  `DEPLOYYY_SCAN_ENFORCE` to `1`; until then it is a warning and the build
  goes on. Enforcement is opt-in per project for now: when the gate was
  added, most running images had such a finding (npm's bundled `tar` in the
  PHP base image, OpenSSL in nginx alpine, older `next`), and an enforcing
  gate would have stopped their next deploy. Make your report clean, then
  switch it on.
- Every other finding is **reported**: the job summary shows the counts and
  the critical and high findings, and the full report is a run artifact
  (`trivy-<image>`).
- To accept a finding, add its ID to a `.trivyignore` file at the root of
  your repository, with a comment that gives the reason.

The build tags the images (the tags the platform deploys) only when every
image of the build passed (with enforcement on; a reported finding does not
hold the tags back). Each image carries an SBOM and a provenance
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
    permissions:
      contents: read
      packages: write
      actions: read
      id-token: write
    with:
      vars: ${{ toJSON(vars) }}
    secrets:
      all: ${{ toJSON(secrets) }}
```

The hand-off is the same as for Magento (Step 3 above), and so is where your
values live: GitHub, per repository or per GitHub Environment named after the
branch.

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

Settings: every variable and secret of the project is available to `next
build` (as on Vercel; only `NEXT_PUBLIC_*` ends up in the browser bundle), and
after the build to the running server in `process.env`.

### Shared page cache

Your project carries no cache code and no cache configuration. The build
wraps your `next.config` (`.js`, `.mjs`, `.ts` or `.mts`) and adds:

- `cacheHandler`: [`recipes/nextjs/platform/cache-handler.mjs`](recipes/nextjs/platform/cache-handler.mjs),
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
    permissions:
      contents: read
      packages: write
      actions: read
      id-token: write
    with:
      vars: ${{ toJSON(vars) }}
    secrets:
      all: ${{ toJSON(secrets) }}
```

The PHP version comes from `require.php` in `composer.json` (8.2–8.4). The
recipe (`recipes/laravel`) builds two images, `php-fpm-<sha>` and
`nginx-<sha>`, including your Vite/Mix assets when there is a `package.json`.
A `Dockerfile` in your repository replaces the recipe; a file in `.platform/`
replaces the recipe's file of the same name.

The platform configures the application through environment variables —
`DB_*`, `REDIS_*`, `APP_URL`, `APP_KEY`, mail and S3 — so do not commit a
`.env`. Your own variables and secrets live in GitHub (repository, or a GitHub
Environment named after the branch) and win over the platform's. Migrations run before a release serves (`php artisan migrate
--force` unless the project declares its own release commands); a failed
migration keeps the previous release online.
