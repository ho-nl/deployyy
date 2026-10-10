# Changelog

What changed on the deployyy platform, newest first. Everything below can be
done from the console at [deployyy.app](https://deployyy.app), through the
platform API, and through the MCP tools, unless an entry says otherwise.

## 2026-10-10

### Magento

- **Product images are cached in the browser for a year.** With media on
  remote storage, every image is answered by Magento's `get.php`, and those
  responses carried no cache headers, so a browser fetched every image again
  on each page view. A media or static file answered through `get.php` or
  `static.php` now carries `Cache-Control: public, max-age=31536000`, as a
  file served from disk already did. A missing file (404) is not cached.
  Takes effect at a project's next build.

## 2026-10-09

### Builds

- **Several applications in one repository.** A repository may hold a Magento
  in one folder and its GraphCommerce storefront in another, each a project of
  its own. The platform then sets the repository variable `DEPLOYYY_APPS`
  (each application's folder, image and settings) in place of the single
  build variables, and each build workflow builds the applications of its
  kind, one job per application. A push builds only the applications whose
  folder changed; the others re-use the release of the push before under the
  new commit's tags. A manual run builds everything. Caller workflows need no
  change, and a repository with one application builds exactly as before
  (README, "Several applications in one repository").

## 2026-10-08

### Builds

- **A storefront is built against its own Magento backend.** A GraphCommerce
  build used the Magento endpoint written in the repository, which could be
  any old backend. A storefront paired with a Magento project on the platform
  is now built against that project's environment for the branch: the same
  branch, else the branch the pull request merges into, else the Magento
  project's main branch. The platform publishes it as the repository variable
  `DEPLOYYY_GC_MAGENTO_ENDPOINTS`, and `nextjs-build.yml` builds with the
  branch's entry as `GC_MAGENTO_ENDPOINT` and waits for it to answer first,
  which wakes a sleeping preview. Builds made by Deployyy do the same. Caller
  workflows need no change (README, "The Magento backend a storefront is
  built against").

- **Build an application that lives in a subfolder.** A repository whose
  application is in `src`, `apps/web` or another folder builds from that
  folder: the platform sets the repository variable `DEPLOYYY_ROOT_DIR` from
  the project's root directory, and the Magento, Laravel, Next.js and
  GraphCommerce builds read `composer.json`, `package.json`, the
  `Dockerfile`, `.platform/` and the rest from there and use it as the build
  context. Connecting a repository whose root holds no application now finds
  the application one folder down. Nothing changes for a repository without
  the variable, and caller workflows need no change (README, "Application in
  a subfolder").

- **A caller workflow from before 2026-10-07 builds again.** The central
  build asked for `actions: read` and `id-token: write` itself, and GitHub
  does not start a reusable workflow that asks for more than its caller
  grants: a branch with the old `secrets: inherit` caller ended in
  `startup_failure`, without a log to say why. The central jobs now take the
  caller's grants, and the named `COMPOSER_AUTH` / `MAGENTO_AUTH_JSON` secrets
  are declared again, so an old caller builds and its log says what to change
  (the caller in Step 3 of the README). A missing `actions: read` or
  `id-token: write` is a warning that names it.

### Service stack

- **New Magento 2.4.9 and Mage-OS 3 projects get Adobe's tested stack:**
  RabbitMQ 4.3 and Valkey 9 (with MariaDB 12 and OpenSearch 3). These are
  now the only queue and cache versions offered for those lines. Existing
  projects keep the versions they run.
- **PHP 8.5** builds on 2.4.9 and Mage-OS 3, but you cannot choose it yet:
  the console offers it after its live trial. PHP 8.4 stays the default.

## 2026-10-07

### Varnish

- **Your own VCL, from your repository.** Commit `varnish-snippet.vcl` (added
  to the platform's VCL) or `varnish.vcl` (replaces it) at the root of the
  repository. The next build of that branch uses it once it compiles; one
  that does not compile is never used, and the environment's page cache says
  why.
- **The Magento recipe no longer adds `GraphCommerce_GraphQlVarnishPostCache`**,
  and the repository variable `DEPLOYYY_GRAPHQL_POST_CACHE` is gone. A project
  that wants GraphQL POST requests cached requires the module in its own
  `composer.json`. The console no longer reports whether GraphQL POST
  requests are cached.

### Builds

- **GitHub is the only place for a project's variables and secrets.** Every
  build workflow (Magento, Laravel, Next.js/GraphCommerce) takes all of them
  in one go, without naming any, and the caller is the same in every project
  — from any GitHub organization:

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

  Every secret is masked, every value is an environment variable of the build
  (and the BuildKit secret `build-env`), and after a successful build the same
  set reaches the branch's environment on Deployyy for the running app —
  authenticated with the build's GitHub OIDC token, no stored credential. A
  GitHub Environment named after the branch overrides the repository's values.
  `COMPOSER_AUTH` is simply one of your secrets. **Update your caller
  workflows**: the named `COMPOSER_AUTH` / `MAGENTO_AUTH_JSON` secrets are no
  longer declared, so a caller that still passes them by name stops with
  "secret is not defined in the referenced workflow". Values that were set in
  the console move to GitHub: the console now lists names only.
- **An unapplied patch fails the build.** Every `*.patch` file in `patches/`
  must be in the installed code after `composer install`, or the Magento and
  Mage-OS build stops and names the file. Declare patches in `extra.patches`
  of `composer.json`: the `extra.patches-search` folder scan skipped some
  patches without an error (a branch install such as `dev-main`, or a header
  naming no installed package), and the build shipped the code unpatched. A
  file that must stay in `patches/` without being applied carries `@skip` in
  its header.

## 2026-10-03

### Builds

- **Choose your PHP version.** A Magento, Mage-OS or Laravel project picks its
  PHP from the versions its release supports — Magento 2.4.9, 2.4.8 and
  Mage-OS 3: 8.4 or 8.3; Magento 2.4.7: 8.3 or 8.2; Magento 2.4.6: 8.2 or
  8.1; Laravel: 8.4, 8.3 or 8.2 — and can change it later, for the project or
  for one environment. The platform sets the repository variable
  `DEPLOYYY_PHP`; do not edit it by hand. A project that chose a version gets
  images tagged with it (`php-fpm-8.3-<sha7>`, `nginx-8.3-<sha7>`), and only
  those are released, so an environment never runs a build of another PHP.
  After a change, the next build of each branch makes the new images: push,
  or re-run the latest build. Without a choice nothing changes.

## 2026-09-29

### Builds

- **Next.js and GraphCommerce builds.** New reusable workflows
  `nextjs-build.yml` and `graphcommerce-build.yml`. The build wires the
  platform's shared page cache into your Next.js config, so your project
  carries no cache code. A second image holds the pages the build
  prerendered; the platform loads them into the cache, so a release starts
  warm.
- **Magento Open Source 2.4.6, 2.4.7, 2.4.8 and 2.4.9** build with a
  platform recipe (PHP 8.2, 8.3, 8.4 and 8.4). Your repository no longer
  needs its own Dockerfile for these releases.
- **GraphQL POST requests are cached.** Magento builds include
  `GraphCommerce_GraphQlVarnishPostCache`. Set the repository variable
  `DEPLOYYY_GRAPHQL_POST_CACHE` to `0` to leave it out.
- **Every image is scanned.** A critical vulnerability that has a fix is
  reported as a warning, and stops the release once the project sets
  `DEPLOYYY_SCAN_ENFORCE=1`; the build reports all other findings. Each image carries an
  SBOM and a provenance attestation.
- **Unprivileged images.** The new Magento and Next.js images run without
  root and without `sudo`.

### Agent workspaces

A workspace is a development sandbox for one of your projects, connected to a
coding agent. It starts from a branch, or from another workspace, and pushes
only to its own work branch.

- **Start, fork, stop and review workspaces** from the project page, the API
  or MCP. Each workspace has its own copy of the project's database, taken
  from the preview seed.
- **The agent speaks ACP** (Agent Client Protocol) over a WebSocket with a
  per-workspace token. You can rotate the token at any time.
- **Bring your own model key.** Save an Anthropic API key or a Claude
  subscription token in team settings. The key is written once and never
  shown back: the console records only that a key is saved, and when. No
  workflow ever takes the key as an input, so it never ends up in a run's
  history.
- **Guard rails.** A workspace will not start without a saved key. Outbound
  traffic goes through an allowlist. A workspace sleeps after 30 idle
  minutes and wakes on the next connection. It ends after 7 days at most;
  the branch it pushed stays.
- **Plan limits.** Every plan now includes concurrent workspaces: Personal 1,
  Development 2, Standard 3.

### Backups and restore

- **See every backup** of an environment's database, both the nightly ones
  and the ones people took, with its size and whether it can be downloaded
  or restored.
- **Back up now.** Take a backup on demand, the same way the nightly one is
  taken. An owner approves it in the console.
- **Download a backup.** The console hands your browser a link that is valid
  for 15 minutes. The link is never stored in a workflow.
- **Restore a backup** into an environment. The restore is validated before
  it goes live, and an owner approves the switch-over.
- **Backups are now kept per environment.** Before this change, environments
  in the same project shared one backup location.
- **PostgreSQL** projects get the same copy, import and version-change flows
  as MySQL and MariaDB. Grants survive a plain PostgreSQL dump.

## 2026-09-28

### Environment operations

- **Capacity.** Set the minimum and maximum instances of each part of an
  environment, within your plan.
- **Storage.** Grow an environment's storage, within your plan's limits.
- **Pause, stop and start.** Pause an environment, or stop it, and start it
  again later. Its data is kept either way. Pausing or stopping production
  needs an owner.
- **Maintenance page.** Put up a maintenance page with your own message.
  Addresses you list still see the project, so you can check your work
  before you take the page down. The message and the address list are kept
  while the page is off.
- **Protection.** Limit a staging or preview environment to a list of
  addresses and add a password. Production always stays public. The
  password is set on the environment's page and is never stored in a
  workflow.
- **Redirects.** Add and remove redirects per environment, for example
  `www.shop.example.com` to `shop.example.com`.
- **Release history and roll back.** See every release that went live on an
  environment, and roll back to an earlier one. The environment then stays
  on that release until you release it again.
- **Restart** an environment.
- **Variables.** Manage project and environment variables. The console shows
  variable names only; values are write-only.
- **Front switches.** Turn the content delivery network, the page cache and
  the firewall on or off per project, as far as the project's platform
  offers each one.
- **Previews on or off** per project. Turning them off removes every preview
  and its data.

### Domains

- **Add a domain you own** to a persistent environment. The console tells
  you the DNS record to set and shows when the domain is live. The first
  domain becomes the environment's main address, which every link and email
  the project sends uses.
- **Remove a domain** from an environment.

### New project types

- **Laravel.** Laravel projects run on the platform, with a build recipe in
  this repository (`recipes/laravel`) and a reusable build workflow.
- **Windmill.** Windmill runs as a packaged platform, on PostgreSQL with
  scheduled backups.
- **Elasticsearch** and **PostgreSQL** are available as data services,
  alongside MySQL/MariaDB, OpenSearch, Redis and RabbitMQ.
- **Magento and Laravel** projects can put a content delivery network in
  front of their environments.

### Seeing what your project is doing

- **Environment canvas.** One view of what runs your project and what it
  uses.
- **Output and logs.** When setup or a deploy fails, the console shows the
  end of your project's own output from the failing step. It also offers
  full log search over a time range, and live log streaming.
- **Load.** How busy an environment is, as a page, through the API, over
  MCP, and live.

### Console

- Members see the API documentation for the platform, the workflows and MCP.
- MCP tools are marked when they only read.
- The console uses one word per concept. A project is *stopped* and
  *restored*, not deleted. A build that went live is a *release*.

## 2026-09-27

- **Monthly usage and billing.** Each team sees its projects' monthly usage,
  measured per minute from the resources they actually use, and can
  authorize a payment method for metered billing. Failed payments are
  retried.
- **Support and feedback** go through workflows that stay open until the
  support issue is closed.
- **One workflow engine.** A workflow started in the console, through the
  API or through MCP runs the same way everywhere. It keeps its progress
  across restarts, and its questions can be answered in chat.
- **Backup storage** is measured per project.

## 2026-09-24 – 2026-09-26

- **Database conversions** (for example MySQL to MariaDB) run as verified
  migrations. They need an approval, and they can be stopped, resumed or
  retried.
- **The main environment is explicit** for every project.
- **Custom domains on the content delivery network** are handed over once
  the edge itself confirms the domain.
- **Console sign-in** stays on the public host and survives the content
  delivery network.

## Build recipes (this repository)

- `recipes/nextjs` + `nextjs-build.yml` / `graphcommerce-build.yml`: Next.js
  standalone image (`sha-<sha7>`, uid 1001) and a cache-seed image
  (`sha-<sha7>-cache-seed`). The Next config is wrapped at build time with
  `cacheHandler`, `cacheMaxMemorySize: 0`, `output: 'standalone'` and the
  commit as build ID. The seed covers App Router and Pages Router prerenders
  on Next 14, 15 and 16.
- `graphcommerce/cache-handler.mjs`: no longer makes Turbopack (Next 16)
  trace the whole project into the standalone output.
- `recipes/magento`: Magento Open Source 2.4.6–2.4.9 (lines `magento-246` …
  `magento-249`). Cache types from the installed modules, a primary key for
  `queue_poison_pill` when the tree has none, `patches/` before
  `composer install`, runtime minification pinned to what the build
  deployed, the GraphQL POST cache module, php.ini `production`.
- `recipes/magento` images run as uid 1000 (php-fpm, no `sudo`) and on
  `nginx-unprivileged`; see `docs/PSS-RESTRICTED.md`.
- All reusable build workflows push by digest, scan with trivy, and tag only
  after every image passed; SBOM + provenance (`mode=max`) on every image.
- `lint.yml`: actionlint, shellcheck, syntax checks and a drift check for the
  support files that `recipes/magento` shares with `recipes/mageos-3`.
- `recipes/laravel`: new Laravel build recipe and reusable workflow.
- `mageos-3`: an empty `VARNISH_HOST` now means Magento's own page cache.
- The recipe copies your project's `patches/` before `composer install`, so
  composer-patches applies them at install time.
- PHP and nginx build caches are kept in separate cache scopes.
- A missing `COMPOSER_AUTH` is a valid state and no longer fails the build.
