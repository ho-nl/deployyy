# Changelog

Changes to Deployyy, newest first. Each feature works in the console at
[deployyy.app](https://deployyy.app), the API and the MCP tools. Exceptions
are noted per entry.

## 2026-10-10

### Magento

- **FastBoot is switched on when a project installs it.** With
  `graphcommerce/magento-fast-boot` in the project, the web container runs
  `fastboot:prepare` before PHP-FPM starts, and PHP-FPM preloads the classes
  of the project's `preload-classes.txt` (see README, FastBoot). Takes effect
  at a project's next build.
  FastBoot keeps its GraphQL schema cache under Magento's own cache, with no
  setting of its own, from its release of 2026-10-10. A project on an older
  FastBoot keeps that cache off until it updates the package.

- **Varnish caches the pages.** With Varnish in front (the default), the
  recipe's `env.php` now also selects it as Magento's page cache. Before, a
  database without that setting kept Magento on its built-in cache. Magento
  then marked every page uncacheable for Varnish, and each first visit of a
  page rendered in full. Takes effect at a project's next build.

- **OPcache holds the whole shop.** The recipe's PHP keeps up to 130 000
  compiled files in 512 MB, with 64 MB of interned strings, and does not
  check file times (the image never changes; a dev box still does). The
  image's defaults (10 000 files, 128 MB, 8 MB) were nearly full after one
  listing page. Takes effect at a project's next build.

- **Every store view uses the environment's URL.** Before, a store-scope or
  website-scope `base_url` in the database overrode the environment's URL. A
  copied, moved or imported database kept its old host. After a project move,
  links and images pointed at a dead host.
  - The recipe's `env.php` pins the base URL of every store view
    (`MAGENTO_STORE_BASE_URLS`) and website (`MAGENTO_WEBSITE_BASE_URLS`).
  - Outside production, cookies are host-only, and static and media URLs
    follow the base URL.
  - Production keeps the cookie domain and media host from the database.
  - `base_link_url` keeps the value from the database. A headless shop points
    it at its storefront for emails and the sitemap.
  - Applies on a project's next build. Requires the Deployyy release that
    reads store views.

- **`env.php` reads services by their generic names.** The recipe's `env.php`
  for Magento Open Source and Mage-OS reads:
  - `DB_*` for the database, including `DB_PORT`
  - `SEARCH_HOST` and `SEARCH_PORT`
  - `SESSION_HOST` and `SESSION_PORT`
  - `CACHE_HOST` and `CACHE_PORT`
  - `QUEUE_*` and `MEDIA_*`

  `OPENSEARCH_*`, `REDIS_SESSION_*`, `REDIS_CACHE_*`, `RABBITMQ_*`,
  `AWS_S3_*`, `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` are no longer
  read. Without a queue service, Magento uses its MySQL queue. Without a media
  bucket, Magento keeps files on local storage. Applies on a project's next
  build. Requires the Deployyy release that provides these names.

- **Browsers cache product images for a year.** With media on remote storage,
  Magento's `get.php` serves every image. Those responses had no cache
  headers, so browsers downloaded every image on each page view. Media and
  static files served through `get.php` or `static.php` now send
  `Cache-Control: public, max-age=31536000`, like files served from disk. A
  404 is not cached. Applies on a project's next build.

## 2026-10-09

### Builds

- **Several applications in one repository.** A repository can hold Magento in
  one folder and its GraphCommerce storefront in another. Each application is
  a separate project.
  - The repository variable `DEPLOYYY_APPS` lists each application's folder,
    image and settings. It replaces the single-application build variables.
  - Each build workflow builds the applications of its type, one job per
    application.
  - A push builds only applications whose folder changed. The others get the
    new commit's tags on the previous push's release.
  - A manual run builds every application.
  - Caller workflows need no change. A repository with one application builds
    as before.

  See README, "Several applications in one repository".

## 2026-10-08

### Builds

- **A storefront builds against its own Magento backend.** Before, a
  GraphCommerce build used the Magento endpoint in the repository, which could
  point at any backend.
  - A storefront paired with a Magento project builds against that project's
    environment for the branch. The order: same branch, then the pull
    request's target branch, then the Magento project's main branch.
  - The repository variable `DEPLOYYY_GC_MAGENTO_ENDPOINTS` holds the
    endpoints. `nextjs-build.yml` builds with the branch's entry as
    `GC_MAGENTO_ENDPOINT`.
  - The build waits for the endpoint to respond first. The request wakes a
    paused preview.
  - Hosted builds use the same endpoint.
  - Caller workflows need no change.

  See README, "Magento backend for a storefront".

- **Build an application from a subfolder.** For an application in `src`,
  `apps/web` or another folder:
  - The repository variable `DEPLOYYY_ROOT_DIR` holds the project's root
    directory.
  - Magento, Laravel, Next.js and GraphCommerce builds read `composer.json`,
    `package.json`, the `Dockerfile`, `.platform/` and other files from that
    folder. The folder is the build context.
  - Connecting a repository with no application at its root selects the
    subfolder that holds one.
  - Without the variable, nothing changes. Caller workflows need no change.

  See README, "Application in a subfolder".

- **Caller workflows from before 2026-10-07 build again.** The shared build
  requested `actions: read` and `id-token: write`. GitHub refuses to start a
  reusable workflow that requests more permissions than its caller grants. A
  branch with the old `secrets: inherit` caller ended in `startup_failure`,
  with no log.
  - The build jobs use the caller's permissions.
  - The named secrets `COMPOSER_AUTH` and `MAGENTO_AUTH_JSON` are declared
    again.
  - An old caller builds, with a warning to switch to the caller in README
    Step 3.
  - A missing `actions: read` or `id-token: write` produces a warning that
    names the permission.

### Service stack

- **New Magento 2.4.9 and Mage-OS 3 projects get Adobe's tested stack:**
  RabbitMQ 4.3 and Valkey 9, with MariaDB 12 and OpenSearch 3. These are the
  only queue and cache versions offered for those lines. Existing projects
  keep their versions.
- **PHP 8.5** builds on 2.4.9 and Mage-OS 3. The console offers 8.5 after its
  live trial. PHP 8.4 remains the default.

## 2026-10-07

### Varnish

- **Your own VCL, from your repository.** Commit `varnish-snippet.vcl` to add
  to the Deployyy VCL, or `varnish.vcl` to replace it, at the repository root.
  The branch's next build uses the file once the VCL compiles. A VCL that
  fails to compile is not used, and the environment's page cache shows the
  compile error.
- **The Magento recipe no longer adds `GraphCommerce_GraphQlVarnishPostCache`.**
  The repository variable `DEPLOYYY_GRAPHQL_POST_CACHE` is removed. To cache
  GraphQL POST requests, require the module in your `composer.json`. The
  console no longer shows whether GraphQL POST requests are cached.

### Builds

- **GitHub holds all project variables and secrets.** Every build workflow,
  for Magento, Laravel, Next.js and GraphCommerce, takes all of them without
  naming any. The caller is identical in every project and works from any
  GitHub organization:

  ```yaml
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

  - Every secret is masked in the log.
  - Every value is an environment variable of the build and part of the
    BuildKit secret `build-env`.
  - After a successful build, the same values go to the branch's environment
    for the running application. The build authenticates with its GitHub OIDC
    token, with no stored credential.
  - A GitHub Environment named after the branch overrides the repository's
    values.
  - `COMPOSER_AUTH` is a regular secret.
  - **Update your caller workflows.** The named secrets `COMPOSER_AUTH` and
    `MAGENTO_AUTH_JSON` are no longer declared. A caller that passes them by
    name fails with "secret is not defined in the referenced workflow".
  - Values set in the console move to GitHub. The console lists names only.
- **An unapplied patch fails the build.** Every `*.patch` file in `patches/`
  must be in the installed code after `composer install`. Otherwise the
  Magento or Mage-OS build fails and names the file.
  - Declare patches in `extra.patches` of `composer.json`. The
    `extra.patches-search` folder scan skipped two kinds of patch without an
    error. Patches for a branch install such as `dev-main`, and patches whose
    header names no installed package. Those builds shipped unpatched code.
  - Add `@skip` to the header of a file that stays in `patches/` but must not
    be applied.

## 2026-10-03

### Builds

- **Choose your PHP version.** A Magento, Mage-OS or Laravel project picks its
  PHP version from the versions its release supports:
  - Magento 2.4.9, 2.4.8 and Mage-OS 3: 8.4 or 8.3
  - Magento 2.4.7: 8.3 or 8.2
  - Magento 2.4.6: 8.2 or 8.1
  - Laravel: 8.4, 8.3 or 8.2

  Change the version later for the project or for one environment. The choice
  is stored in the repository variable `DEPLOYYY_PHP`. Do not edit it by hand.
  Images then carry the version in their tag, such as `php-fpm-8.3-<sha7>` and
  `nginx-8.3-<sha7>`. Only those images are released, so an environment never
  runs a build for another PHP version. After a change, push or re-run the
  latest build of each branch. Without a choice, nothing changes.

## 2026-09-29

### Builds

- **Next.js and GraphCommerce builds.** New reusable workflows
  `nextjs-build.yml` and `graphcommerce-build.yml`. The build adds the shared
  page cache to your Next.js config, so your project needs no cache code. A
  second image holds the prerendered pages. They are loaded into the cache
  before a release serves traffic.
- **Magento Open Source 2.4.6, 2.4.7, 2.4.8 and 2.4.9** build with a recipe,
  on PHP 8.2, 8.3, 8.4 and 8.4. These releases need no repository Dockerfile.
- **GraphQL POST requests are cached.** Magento builds include
  `GraphCommerce_GraphQlVarnishPostCache`. Set the repository variable
  `DEPLOYYY_GRAPHQL_POST_CACHE` to `0` to leave the module out.
- **Every image is scanned.** A critical vulnerability with a fix produces a
  warning. With `DEPLOYYY_SCAN_ENFORCE=1`, it blocks the release. All other
  findings are listed in the job summary. Each image carries an SBOM and a
  provenance attestation.
- **Unprivileged images.** The new Magento and Next.js images run without root
  and without `sudo`.

### Agent workspaces

A workspace is a development sandbox for one project, connected to a coding
agent. It starts from a branch or from another workspace, and pushes only to
its own work branch.

- **Start, fork, stop and review workspaces** from the project page, the API
  or MCP. Each workspace gets its own copy of the project's database, from
  the preview seed.
- **ACP connection.** The agent uses the Agent Client Protocol over a
  WebSocket, with a token per workspace. Rotate the token at any time.
- **Your own model key.** Save an Anthropic API key or a Claude subscription
  token in team settings. The key is write-only. The console shows only that a
  key is saved, and when. No workflow takes the key as an input, so the key
  never appears in a run's history.
- **Limits.** A workspace needs a saved key to start. Outbound traffic goes
  through an allowlist. A workspace sleeps after 30 idle minutes and wakes on
  the next connection. A workspace ends after 7 days at most. Its pushed
  branch remains.
- **Plan limits.** Concurrent workspaces per plan: Personal 1, Development 2,
  Standard 3.

### Backups and restore

- **Backup list.** Every backup of an environment's database, nightly and
  manual, with its size and whether it can be downloaded or restored.
- **Back up now.** Take an on-demand backup, made the same way as the nightly
  one. An owner approves it in the console.
- **Download a backup.** The download link is valid for 15 minutes and is
  never stored in a workflow.
- **Restore a backup** into an environment. The restore is validated before
  it goes live. An owner approves the switch-over.
- **Backups per environment.** Before, environments of one project shared one
  backup location.
- **PostgreSQL** projects get the copy, import and version-change workflows of
  MySQL and MariaDB. Grants survive a plain PostgreSQL dump.

## 2026-09-28

### Environment operations

- **Capacity.** Set the minimum and maximum instances of each part of an
  environment, within your plan.
- **Storage.** Grow an environment's storage, within your plan's limits.
- **Pause, stop and start.** Pause or stop an environment, and start it again
  later. The data is kept either way. Pausing or stopping production requires
  an owner.
- **Maintenance page.** Show a maintenance page with your own message.
  Listed IP addresses bypass the page, to check your work before you take the
  page down. The message and address list are kept while the page is off.
- **Protection.** Restrict a staging or preview environment to a list of IP
  addresses and add a password. Production is always public. The password is
  set on the environment's page and never stored in a workflow.
- **Redirects.** Add and remove redirects per environment, for example
  `www.shop.example.com` to `shop.example.com`.
- **Release history and roll back.** Every release that went live on an
  environment, with roll back to an earlier one. The environment then stays on
  that release until the next release.
- **Restart** an environment.
- **Variables.** Manage project and environment variables. The console shows
  names only. Values are write-only.
- **Front switches.** Turn the content delivery network, page cache and
  firewall on or off per project. Availability depends on the project type.
- **Previews on or off** per project. Turning previews off removes every
  preview and its data.

### Domains

- **Add a domain you own** to a persistent environment. The console shows the
  DNS record to set, and when the domain is live. The first domain becomes the
  environment's main address. Every link and email from the project uses that
  address.
- **Remove a domain** from an environment.

### New project types

- **Laravel**, with a build recipe in this repository (`recipes/laravel`) and a
  reusable build workflow.
- **Windmill**, as a packaged application on PostgreSQL with scheduled
  backups.
- **Elasticsearch** and **PostgreSQL** data services, next to MySQL, MariaDB,
  OpenSearch, Redis and RabbitMQ.
- **Content delivery network** in front of Magento and Laravel environments.

### Project activity

- **Environment canvas.** One view of what runs your project and what it uses.
- **Output and logs.** When setup or a deploy fails, the console shows your
  project's last output lines from the failing step. Search full logs
  over a time range, or stream them live.
- **Load.** How busy an environment is, on a page, through the API, over MCP,
  and live.

### Console

- Members see the API documentation for the Deployyy API, the workflows and
  MCP.
- Read-only MCP tools are marked.
- One word per concept. A project is *stopped* and *restored*, not deleted. A
  build that went live is a *release*.

## 2026-09-27

- **Monthly usage and billing.** Each team sees its projects' monthly usage,
  measured per minute from actual resource use. Authorize a payment method for
  metered billing. Failed payments are retried.
- **Support and feedback** run as workflows that stay open until the support
  issue is closed.
- **One workflow engine.** A workflow runs the same way from the console, the
  API or MCP. Progress survives restarts. Questions can be answered in chat.
- **Backup storage** is measured per project.

## 2026-09-24 to 2026-09-26

- **Database conversions**, for example MySQL to MariaDB, run as verified
  migrations. They need an approval, and can be stopped, resumed or retried.
- **Every project has an explicit main environment.**
- **Custom domains on the content delivery network** switch over after the
  edge confirms the domain.
- **Console sign-in** stays on the public host and works behind the content
  delivery network.

## Build recipes (this repository)

- `recipes/nextjs` with `nextjs-build.yml` and `graphcommerce-build.yml`:
  Next.js standalone image `sha-<sha7>`, uid 1001, and a cache-seed image
  `sha-<sha7>-cache-seed`. The build wraps the Next config with
  `cacheHandler`, `cacheMaxMemorySize: 0`, `output: 'standalone'` and the
  commit as build ID. The seed covers App Router and Pages Router prerenders on
  Next 14, 15 and 16.
- `graphcommerce/cache-handler.mjs`: Turbopack on Next 16 no longer traces the
  whole project into the standalone output.
- `recipes/magento`: Magento Open Source 2.4.6 to 2.4.9, lines `magento-246`
  to `magento-249`, with:
  - cache types from the installed modules
  - an optional primary key for `queue_poison_pill`
  - `patches/` before `composer install`
  - runtime minification matched to the build
  - the GraphQL POST cache module
  - php.ini `production`
- `recipes/magento` images run as uid 1000, with php-fpm without `sudo`, and
  on `nginx-unprivileged`. See `docs/PSS-RESTRICTED.md`.
- All reusable build workflows push by digest, scan with Trivy, and tag only
  after every image passes. Every image carries an SBOM and provenance with
  `mode=max`.
- `lint.yml`: actionlint, shellcheck, syntax checks, and a drift check for the
  support files `recipes/magento` shares with `recipes/mageos-3`.
- `recipes/laravel`: new Laravel build recipe and reusable workflow.
- `mageos-3`: an empty `VARNISH_HOST` selects Magento's built-in page cache.
- The recipe copies your `patches/` before `composer install`, so
  composer-patches applies them at install time.
- PHP and nginx build caches use separate cache scopes.
- A missing `COMPOSER_AUTH` no longer fails the build.
