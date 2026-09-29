# Changelog

What changed on the deployyy platform, newest first. Everything below can be
done from the console at [deployyy.app](https://deployyy.app), through the
platform API, and through the MCP tools, unless an entry says otherwise.

## 2026-09-29

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

- `recipes/laravel`: new Laravel build recipe and reusable workflow.
- `mageos-3`: an empty `VARNISH_HOST` now means Magento's own page cache.
- The recipe copies your project's `patches/` before `composer install`, so
  composer-patches applies them at install time.
- PHP and nginx build caches are kept in separate cache scopes.
- A missing `COMPOSER_AUTH` is a valid state and no longer fails the build.
