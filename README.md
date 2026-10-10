# deployyy

Build workflows and Dockerfile recipes for Magento 2, Next.js, GraphCommerce
and Laravel projects on [Deployyy](https://deployyy.app).

Each git branch gets its own environment. Preview environments pause when
idle. Your repository holds your code and two short caller workflows. Project
settings live in the Deployyy console.

## Get started

### Step 1: Connect your repository

Install the Deployyy GitHub App on your repository:
**[github.com/apps/deployyy-platform](https://github.com/apps/deployyy-platform)**.

Then connect the repository to a project in the Deployyy console. If your
GitHub organization has no Deployyy team yet, contact Deployyy support.

### Step 2: Add your variables and secrets to GitHub

Keep every project variable and secret in GitHub, under the repository's
**Settings → Secrets and variables → Actions**. Use secrets for keys and
passwords. Use variables for values that are safe to read back, such as a
public address or a feature flag.

Every value reaches the build and the running application. Magento's
`config.php` and `env.php` read them with `getenv()`.

- **Per environment**: create a GitHub Environment named after the branch,
  such as `main` or `staging`, under **Settings → Environments**. Its
  variables and secrets override the repository's for that branch. A branch
  without a GitHub Environment uses the repository's values.
- **Composer credentials**: add a secret `COMPOSER_AUTH` with the contents of
  your `auth.json`, for Magento Marketplace or Packagist keys. Mage-OS on
  public packages needs no credentials.
- **Changes apply with the next build** of the branch. Push, or re-run the
  latest build. A redeploy or rollback without a build keeps the values of the
  last build.

The Deployyy console lists variable and secret names only, read-only, with
links back to GitHub.

### Step 3: Add the build workflows

Add two caller workflows. They pass every variable and secret without naming
any, so they are identical in every project. Adding a value needs no change
to them.

```yaml
# .github/workflows/preview-build.yaml: builds every branch except main
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
# .github/workflows/build.yaml: builds main
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

What each part does:

- `secrets: all: ${{ toJSON(secrets) }}` passes every secret under one
  declared name. GitHub allows that from any organization. `secrets: inherit`
  works only inside the `ho-nl` organization, so other repositories would
  build without their secrets.
- `vars: ${{ toJSON(vars) }}` passes every variable.
- `packages: write` pushes the images.
- `actions: read` reads the GitHub Environment named after the branch.
- `id-token: write` lets the build prove its repository and branch to
  Deployyy. Variables and secrets then reach the running application without
  a stored credential.

The build jobs use only the permissions the caller grants. A missing
permission produces a warning in the build log:

| Missing | Effect |
|---|---|
| `actions: read` | The branch's GitHub Environment is not used. |
| `id-token: write` | Variables and secrets do not reach the environment. |

A caller written before 2026-10-07 uses `secrets: inherit` or passes
`COMPOSER_AUTH` by name, without `permissions`. Such a caller builds, with a
warning to switch to the workflows above.

During the build:

- Every secret is masked in the log.
- Every value is an environment variable of the build steps.
- Every value is in the BuildKit secret `build-env`. A repository
  `Dockerfile` reads it with `RUN --mount=type=secret,id=build-env`.
- After a successful build, the same values go to the branch's environment
  on Deployyy.

If the branch has no environment, or the repository is not connected, the
build logs a warning and passes. The environment keeps its previous values.

Limits:

- The build job runs in the branch's GitHub Environment. That Environment's
  protection rules, such as required reviewers, wait timers and branch rules,
  apply to the build.
- GitHub never shows a secret value again. Values reach Deployyy only through
  a build.
- If a new preview environment is not ready when its first build finishes,
  delivery retries for about two minutes. After that, the next build delivers
  the values.
- The calling job cannot run in a GitHub Environment. Its `toJSON(secrets)`
  holds only repository and organization secrets. Environment secrets come in
  through the build job, which needs `actions: read`.
- Organization secrets and variables arrive only if the organization grants
  them to the repository.
- GitHub runs some builds without secrets, such as a pull request from a fork
  or a Dependabot push. Those builds deliver no secret values.
- Names must be valid shell variable names: letters, digits and underscores,
  not starting with a digit. Other names are skipped with a warning.
- GitHub stores secret names in upper case.
- A name used for both a variable and a secret is treated as a secret.
- Names starting with `DEPLOYYY_` are build settings. They do not reach the
  running application.
- Values from the project's services, such as database and cache settings,
  override yours.

### Step 4: Push a branch

Push a branch. The workflow builds two images and pushes them to
`ghcr.io/<owner>/<repo>` with commit tags `php-fpm-<sha7>` and `nginx-<sha7>`.
When the tags exist, the preview environment deploys at
`https://<branch>.<project>.deployyy.app`. The workflow does not deploy.

## Configuration

Your repository holds no Deployyy configuration file. Project settings live
in the Deployyy console.

- **Release line**: read from `composer.json`.
- **Service stack**: database, search, queue and cache are project settings.
- **PHP version**: the release line's default unless you choose another
  supported version in the console. Defaults: 2.4.6 uses 8.2, 2.4.7 uses 8.3,
  2.4.8, 2.4.9 and Mage-OS 3 use 8.4.
  - The choice is stored in the repository variable `DEPLOYYY_PHP`.
  - Images then carry the version in their tag, such as `php-fpm-8.3-<sha7>`.
  - After a change, push or re-run the latest build. The environment keeps its
    current release until the new build exists.
  - PHP 8.5 builds on 2.4.9 and Mage-OS 3. The console offers 8.5 after its
    live trial.
  - A repository `Dockerfile` receives the version as build argument
    `PHP_VERSION`.
- **Static-content locales**: the repository variable
  `DEPLOYYY_MAGENTO_LOCALES`, set from the project settings. Default:
  `en_US`.

Do not edit `DEPLOYYY_*` repository variables by hand. Project settings
overwrite them.

To change a setting the console does not offer, contact Deployyy support.

### Application in a subfolder

The application can live in a folder, such as `src` or `apps/web`. The
project's root directory names that folder.

- If the repository root holds no application, connecting the repository
  sets the root directory to the subfolder that holds one.
- The repository variable `DEPLOYYY_ROOT_DIR` holds the folder. It is removed
  when the application is at the root.
- The build reads `composer.json`, `composer.lock`, `package.json`, `.nvmrc`,
  `.node-version`, `Dockerfile`, `.platform/`, `deployyy.json`, `patches/`,
  `.trivyignore` and the Varnish VCL files from that folder.
- The folder is the Docker build context. Files outside it do not reach the
  image.
- In this README, "the root of your repository" means the application
  folder.
- The caller workflows stay the same.

The folder must be a relative path inside the repository. It cannot start
with `/`, contain `.` or `..`, or be a symbolic link. It must exist in the
commit being built. Otherwise the build fails with the broken rule. An empty
value means the repository root.

### Several applications in one repository

One repository can hold several applications, each in its own folder. An
example: Magento in `magento` and its GraphCommerce storefront in
`storefront`. Each application is a separate Deployyy project with its own
folder, and builds and deploys on its own.

- **Callers**: one caller workflow per type, such as `magento2-build.yml` and
  `graphcommerce-build.yml`. Neither names a folder.
- **Settings**: the repository variable `DEPLOYYY_APPS` lists each
  application's folder, image and settings. It replaces `DEPLOYYY_ROOT_DIR`,
  `DEPLOYYY_PHP`, `DEPLOYYY_MAGENTO_LOCALES` and
  `DEPLOYYY_GC_MAGENTO_ENDPOINTS`, which are then removed. Do not edit
  `DEPLOYYY_APPS` by hand.
- **Jobs**: each workflow builds the applications of its type, one job per
  application, named `build (<folder>)`.
- **Images**: the repository's first project publishes to
  `ghcr.io/<owner>/<repo>`. Every other project publishes to
  `ghcr.io/<owner>/<repo>/<folder>`, so tags never collide.
- **A push builds only changed applications.** If a push changes nothing in a
  folder, the new commit's tags point at the previous push's release. That job
  is named `re-use (<folder>)`. The folder is the whole build context, so the
  image is identical.
- **Full builds**: every application builds on a manual run
  (`workflow_dispatch`), a re-run, a force push or a new branch. An
  application also builds when its previous release is missing. An
  application at the repository root always builds.
- **Settings outside the folder**, such as a variable, the locales or a recipe
  update, apply with the application's next change. To apply them immediately, run the
  workflow by hand.
- **Files at the repository root**, such as a lockfile or `.github`, are
  outside every folder and trigger no build. An application that needs them
  must use the repository root as its folder.
- **A storefront pairs with the Magento application** of the same repository,
  per branch.

### Varnish VCL (Magento)

The page cache uses the Deployyy VCL. To change it, commit one of these files
to the root of your repository:

- `varnish-snippet.vcl` is added to the Deployyy VCL. A `sub vcl_recv { … }`
  in it runs first, then continues into the Deployyy `vcl_recv`.
- `varnish.vcl` replaces the Deployyy VCL. Use backend host `magento-web`,
  port `8080`. The purge ACL must allow `10.0.0.0/8`, `172.16.0.0/12` and
  `192.168.0.0/16`.

The recipes copy the file into the image. An environment compiles the VCL
from the release it runs, with the same Varnish version. A VCL that fails to
compile is not used. The previous VCL keeps serving, and the console shows
the compile error.

The recipes add no Varnish or GraphQL module to your project. A caching
module, if you need one, goes in your `composer.json`.

## How the build works

1. **Release line.** The workflow reads the release line from
   `composer.json`. For example, `mage-os/product-community-edition: 3.*` maps
   to `mageos-3`. Mage-OS 3.x releases share one Magento 2.4.9 core and one
   service stack, so the match is on the major version.
2. **Recipe.** The workflow builds with the recipe for that line from
   [`recipes/`](recipes/): a Dockerfile plus `.platform/` support files.
   Recipe updates apply to your project on its next build.
3. **Your own files.** A `Dockerfile` in your repository replaces the recipe
   Dockerfile. A file in your `.platform/` directory replaces only the recipe
   file with the same name.
4. **Patches.** Put [vaimo/composer-patches](https://github.com/vaimo/composer-patches)
   files in a `patches/` directory. Declare each one in `extra.patches` of
   `composer.json`:

   ```json
   "extra": {
       "patches": {
           "mage-os/magento2-base": {
               "What the patch fixes": "patches/the-fix.patch"
           }
       }
   }
   ```

   The recipe copies `patches/` into the build before `composer install`, so
   the plugin applies them. After the install, `.platform/check-patches.php`
   fails the build for every `*.patch` file in `patches/` that is not in the
   installed code.

   Use `extra.patches`, not the `extra.patches-search` folder scan. The scan
   skips a patch whose header names no installed package. It also skips every
   patch for a branch install such as `dev-main`, unless the header has
   `@version *`. `check-patches.php` then fails the build for those patches.

   Add `@skip` to the header of a patch file that must not be applied. A
   project without `patches/` is not affected.

## Recipes

| Line | Directory | PHP | Status |
|---|---|---|---|
| `mageos-3` | [`recipes/mageos-3/`](recipes/mageos-3/) | 8.4, 8.5 not validated | Validated live on 3.2.0 and 3.4.0 with PHP 8.4 |
| `magento-249`, Magento Open Source 2.4.9 | [`recipes/magento/`](recipes/magento/) | 8.4, 8.5 not validated | Image builds and starts. Not validated live. |
| `magento-248`, 2.4.8 | [`recipes/magento/`](recipes/magento/) | 8.4 | Image builds and starts. Not validated live. |
| `magento-247`, 2.4.7 | [`recipes/magento/`](recipes/magento/) | 8.3 | Image builds and starts. Not validated live. |
| `magento-246`, 2.4.6 | [`recipes/magento/`](recipes/magento/) | 8.2 | Image builds and starts. Not validated live. |
| Next.js, GraphCommerce | [`recipes/nextjs/`](recipes/nextjs/) | Node 20, 22, 24 | Validated locally on Next 14, 15 and 16. Not validated live. |
| Laravel | [`recipes/laravel/`](recipes/laravel/) | 8.2 to 8.4 | See [Laravel](#laravel). |

The Magento Open Source lines share one recipe. The recipe handles these known
problems:

- **Cache types** come from the installed modules, every `etc/cache.xml`, not
  from the Magento version. Magento disables a cache type that is missing
  from `env.php`, without a warning.
- **`queue_poison_pill`** has no primary key in core. MySQL Group Replication
  refuses writes to such a table, and `setup:upgrade` writes to it.
  MariaDB and single-node MySQL accept the table. Build argument
  `PLATFORM_QUEUE_POISON_PILL_PK=1` adds the module
  `Deployyy_QueuePoisonPillPk`, unless a module in your project declares the
  key. The next `setup:upgrade` then adds the key. The build workflows do not
  set this argument.
- **Patches**: `patches/` is in the build before `composer install`. A patch
  file that is not in the installed code fails the build, see step 4 above.
- **Minification**: the build records whether it deployed minified JS and CSS
  in `app/etc/static_build.php`. `env.php` sets runtime minification to match,
  so database settings cannot request files missing from the image.

**Services.** `env.php` reads each service by its generic name. A service's
variables start with its name. The default services are `db`, `search`,
`session`, `cache`, `queue` and `media`. The recipe reads:

- `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`
- `SEARCH_HOST`, `SEARCH_PORT`
- `SESSION_HOST`, `SESSION_PORT`
- `CACHE_HOST`, `CACHE_PORT`
- `QUEUE_HOST`, `QUEUE_PORT`, `QUEUE_USER`, `QUEUE_PASSWORD`, `QUEUE_VHOST`
- `MEDIA_BUCKET`, `MEDIA_ENDPOINT`, `MEDIA_REGION`, `MEDIA_ACCESS_KEY_ID`,
  `MEDIA_SECRET_ACCESS_KEY`

The queue and the media bucket are optional. Without `QUEUE_HOST`, Magento
uses its MySQL queue. Without `MEDIA_BUCKET`, Magento keeps files on local
storage. A repository with its own `.platform/env.php` must read the same
names.

A release line without a recipe fails the build, with the list of supported
lines. Adobe Commerce has no recipe. For Adobe Commerce, add your own `Dockerfile`.

## Image scanning, SBOM and provenance

Every build pushes its images by digest first, then scans each one with
[Trivy](https://trivy.dev):

- **Critical with a fix**: a critical vulnerability that has a fixed version.
  With the repository variable `DEPLOYYY_SCAN_ENFORCE=1`, such a finding
  fails the build. Without that variable, the build logs a warning and
  continues.
- **Other findings**: listed in the job summary with counts and the critical
  and high rows. The full report is the run artifact `trivy-<image>`.
- **Accepted findings**: add the ID to `.trivyignore` at the repository root,
  with a comment giving the reason.

Enforcement is off by default. When the scan was introduced, most running
images had a critical finding with a fix. Examples: npm's bundled `tar` in
the PHP base image, OpenSSL in nginx alpine, older `next` versions. Fix your
findings, then set `DEPLOYYY_SCAN_ENFORCE=1`.

The build tags the images only after every image passes the scan. Without
enforcement, a finding does not block the tags. Environments deploy only
tagged images.

Each image carries an SBOM and a provenance attestation. To read them:

```sh
docker buildx imagetools inspect ghcr.io/<owner>/<repo>:<tag> --format '{{json .SBOM}}'
docker buildx imagetools inspect ghcr.io/<owner>/<repo>:<tag> --format '{{json .Provenance}}'
```

## Unprivileged images

Images from `recipes/magento` and `recipes/nextjs` run as a non-root user,
without `sudo` and without extra Linux capabilities. Details:
[`docs/PSS-RESTRICTED.md`](docs/PSS-RESTRICTED.md).

## Next.js and GraphCommerce

A Next.js or GraphCommerce project builds with `nextjs-build.yml` or
`graphcommerce-build.yml`. Both run the same build.

```yaml
# .github/workflows/build.yaml: builds every branch
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

Variables and secrets work as for Magento, see Step 2 and Step 3.

The workflow pushes two images to `ghcr.io/<owner>/<repo>`:

- `sha-<sha7>`: the Next.js
  [standalone server](https://nextjs.org/docs/app/api-reference/config/next-config-js/output),
  `node server.js` on port 3000.
- `sha-<sha7>-cache-seed`: the pages `next build` prerendered. They are loaded
  into the shared page cache before the release serves traffic, so first
  visitors get cached pages.

Requirements:

- A `build` script in `package.json`, or `next` as a dependency.
- A route that returns HTTP 200 on `GET /api/health`, or another health path
  set in the project settings. A release serves traffic only after its health
  path returns 200. The build logs a warning when `/api/health` is missing.
- Node 20, 22 or 24, from `.nvmrc`, `.node-version` or `engines.node`.
  Default: 22. The package manager follows your lockfile: npm, Yarn or pnpm.

Every variable and secret is available to `next build` and, at runtime, to
the server in `process.env`. As on Vercel, only `NEXT_PUBLIC_*` values end up
in the browser bundle.

### Magento backend for a storefront

A GraphCommerce storefront paired with a Magento project builds against one
Magento environment. The build picks the first that exists:

1. The Magento environment of the same branch.
2. The environment of the branch your pull request merges into, and so on up
   to the main branch.
3. The Magento project's main branch.

The repository variable `DEPLOYYY_GC_MAGENTO_ENDPOINTS` holds each branch's
endpoint as JSON. The build sets the branch's entry as `GC_MAGENTO_ENDPOINT`.
That value overrides `magentoEndpoint` in `graphcommerce.config.js` and any
`GC_MAGENTO_ENDPOINT` variable of your own.

Before building, the workflow waits up to 10 minutes for the endpoint to
return a GraphQL response. The request also wakes a paused preview. Do not
edit `DEPLOYYY_GC_MAGENTO_ENDPOINTS` by hand. Without the variable, your own
endpoint is used.

### Shared page cache

Your project needs no cache code or cache configuration. The build wraps
your `next.config` (`.js`, `.mjs`, `.ts` or `.mts`) and adds:

- `cacheHandler`: [`recipes/nextjs/platform/cache-handler.mjs`](recipes/nextjs/platform/cache-handler.mjs).
  ISR pages and fetch results go to the shared cache storage at `CACHE_DIR`.
  One revalidation updates every instance. Without `CACHE_DIR`, for example
  in local development, each process has its own cache.
- `cacheMaxMemorySize: 0`, so no instance keeps a stale copy in memory.
- `output: 'standalone'`, if you set no `output`.
- The commit as the build ID, if you set no `generateBuildId`.

Images from `/_next/image` are not in this cache. The content delivery
network caches them.

If your `next.config` sets its own `cacheHandler`, the build keeps it and logs
a warning. `output: 'export'` fails the build, because environments run a
Next.js server.

A `Dockerfile` in your repository replaces the recipe. A file in `.platform/`
replaces the recipe file with the same name.

## Laravel

A Laravel project builds with `laravel-build.yml`:

```yaml
# .github/workflows/build.yaml: builds every branch
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

- **PHP**: from `require.php` in `composer.json`, 8.2 to 8.4.
- **Images**: `recipes/laravel` builds `php-fpm-<sha7>` and `nginx-<sha7>`,
  with your Vite or Mix assets if there is a `package.json`.
- **Own files**: a `Dockerfile` in your repository replaces the recipe. A file
  in `.platform/` replaces the recipe file with the same name.
- **Configuration**: environment variables set `DB_*`, `REDIS_*`, `APP_URL`,
  `APP_KEY`, mail and S3. Do not commit a `.env` file.
- **Your values**: variables and secrets from GitHub, per repository or per
  GitHub Environment, override the configuration values above.
- **Migrations**: `php artisan migrate --force` runs before a release serves
  traffic, unless the project declares its own release commands. If a
  migration fails, the previous release keeps serving.
