# deployyy

Dockerfile recipes for Magento 2, Next.js, GraphCommerce and Laravel projects
on [Deployyy](https://deployyy.app). The hosted build of Deployyy
(deployyy-operator [`internal/build`](https://github.com/ho-nl/deployyy-operator/tree/main/internal/build))
builds each project with the recipe of its release line. A team that builds
in its own CI follows deployyy-operator
[`docs/MANUAL-BUILDS.md`](https://github.com/ho-nl/deployyy-operator/blob/main/docs/MANUAL-BUILDS.md).

## Configuration

Your repository holds no Deployyy configuration file. Project settings live
in the Deployyy console.

- **Release line**: read from `composer.json`.
- **Service stack**: database, search, queue and cache are project settings.
- **PHP version**: the release line's default unless you choose another
  supported version in the console. Defaults: 2.4.6 uses 8.2, 2.4.7 uses 8.3,
  2.4.8, 2.4.9 and Mage-OS 3 use 8.4.
  - The environment keeps its current release until the build of the new
    version exists.
  - PHP 8.5 builds on 2.4.9 and Mage-OS 3. The console offers 8.5 after its
    live trial.
  - A repository `Dockerfile` receives the version as build argument
    `PHP_VERSION`.
- **Static-content locales**: the build locales of the project. Default:
  `en_US`.
- **Build variables**: the platform team keeps them in
  `build-secrets-<repository>`. Every value is in the BuildKit secret
  `build-env`. A repository `Dockerfile` reads it with
  `RUN --mount=type=secret,id=build-env`.

These values reach the build only. The running application reads its
variables from Deployyy: Environment variables in the console, or
`deployyy env add`.

To change a setting the console does not offer, contact Deployyy support.

### Application in a subfolder

The application can live in a folder, such as `src` or `apps/web`. The
project's root directory names that folder.

- If the repository root holds no application, connecting the repository
  sets the root directory to the subfolder that holds one.
- The build reads `composer.json`, `composer.lock`, `package.json`, `.nvmrc`,
  `.node-version`, `Dockerfile`, `.platform/`, `patches/` and the Varnish VCL
  files from that folder.
- The folder is the Docker build context. Files outside it do not reach the
  image.
- In this README, "the root of your repository" means the application
  folder.

The folder must be a relative path inside the repository. It cannot start
with `/`, contain `.` or `..`, or be a symbolic link. It must exist in the
commit being built. Otherwise the build fails with the broken rule. An empty
value means the repository root.

### Several applications in one repository

One repository can hold several applications, each in its own folder. An
example: Magento in `magento` and its GraphCommerce storefront in
`storefront`. Each application is a separate Deployyy project with its own
folder, and builds and deploys on its own.

- **A push builds only changed applications.** If a push changes nothing in a
  folder, the new commit's tags point at the previous release. The folder is
  the whole build context, so the image is identical.
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

1. **Release line.** The build reads the release line from
   `composer.json`. For example, `mage-os/product-community-edition: 3.*` maps
   to `mageos-3`. The match is on the Mage-OS major version.
2. **Recipe.** The build uses the recipe for that line from
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

### Images

The build builds each container target of the recipe and pushes one image per
role to `<image source>/<role>:<sha7>-<key8>`. `<key8>` is the build key of the
build inputs.

| Recipe | Role `app` | Other roles |
|---|---|---|
| `magento`, `mageos-3`, `laravel` | target `php-fpm`, label `app.deployyy.php` | `nginx`: target `nginx` |
| `nextjs` | target `runtime` | `cache-seed`: target `cache-seed`, optional |

### FastBoot (Magento)

When your project installs
[`graphcommerce/magento-fast-boot`](https://github.com/graphcommerce-org/magento2-GraphCommerce_FastBoot),
the recipes switch it on:

- The web container runs `bin/magento fastboot:prepare` before PHP-FPM starts.
  If it fails, FastBoot fills its caches on the first requests.
- PHP-FPM preloads the classes listed in `preload-classes.txt` at the root of
  your repository. Without that file, nothing is preloaded. A dev box never
  preloads, because its code changes.

To make the list, copy `var/cache/preload/classes.txt` from a running web
container after it has served real traffic, and commit it as
`preload-classes.txt`. Refresh it when the classes your shop uses change.

## Recipes

| Line | Directory | PHP | Status |
|---|---|---|---|
| `mageos-3` | [`recipes/mageos-3/`](recipes/mageos-3/) | 8.4, 8.5 not validated | Validated live on 3.2.0 and 3.4.0 with PHP 8.4 |
| `mageos-4`, Magento core 2.4.9 | [`recipes/mageos-3/`](recipes/mageos-3/) | 8.4, 8.5 not validated | Not validated live. |
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
  key. The next `setup:upgrade` then adds the key. The hosted build does not
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
storage.

**`env.php` is the platform's.** A repository cannot bring its own. The build
installs the recipe's `env.php` at `.platform/env.php`, replacing a committed
one with a warning to delete it. A repository with its own `Dockerfile` must copy
`.platform/env.php` to `/var/www/html/app/etc/env.php`; the build fails when the
image's `env.php` differs. Set shop configuration
that differs per environment as a `CONFIG__…` variable instead, for example
`CONFIG__DEFAULT__SMTP__CONFIGURATION_OPTION__HOST`: Magento reads these like an
`env.php` setting and locks the field in the admin. A changed variable needs no
`app:config:import`. Configuration that is the same everywhere belongs in
`app/etc/config.php`.

The platform sets these paths as `CONFIG__…` variables, and its value replaces
a project variable of the same path: the base URLs of every scope,
`web/url/redirect_to_base`, the admin URL, the search host and port, the page
cache (`system/full_page_cache/caching_application`) and, below production, the
cookie domain and the static and media base URLs. A store or website code with
a capital letter or `__` cannot be named by a variable; its base URLs stay in
`env.php`'s `system` section.

**Configuration check.** The recipes disable a Magento safety check: the
plugin `configHash` (`Magento\Deploy\Model\Plugin\ConfigChangeDetector`, on
`Magento\Framework\App\FrontControllerInterface` in `Magento_Deploy`'s
`etc/di.xml`). With it, every page, GraphQL and REST request answers 500 "The
configuration file has changed" while the configuration in `app/etc` differs
from the last `app:config:import`. The build adds the module
`Deployyy_ConfigCheckOff` (`.platform/config-check-off.php`), fails when the
plugin still runs in a compiled area, and labels the image
`app.deployyy.config-check=off`. On that label the platform imports a
release that changes only configuration while the previous release keeps
serving, with no maintenance page. This is safe on the platform because:

- each process reads its own `config.php` and `env.php` `system` values and
  its `CONFIG__` variables, which override the database at runtime;
- the cache key prefix is a digest of that configuration, so two releases
  never share cached configuration;
- the platform runs `app:config:import` for every release that changes the
  configuration, while the previous release serves, and reports a failed
  import on the deployment.

The remaining gap: a store, website or theme that is new in `config.php` is
missing from the database until the import has run, and one removed from
`config.php` is gone from the database for the previous release from then on. Cron, consumers and `bin/magento` commands never ran this
check. `app:config:status` and `config:set` still compare the hashes.

A release line without a recipe fails the build, with the list of supported
lines. Adobe Commerce has no recipe. For Adobe Commerce, add your own `Dockerfile`.

## Unprivileged images

Images from `recipes/magento` and `recipes/nextjs` run as a non-root user,
without `sudo` and without extra Linux capabilities. Details:
[`docs/PSS-RESTRICTED.md`](docs/PSS-RESTRICTED.md).

## Next.js and GraphCommerce

The build makes two images:

- the app: the Next.js
  [standalone server](https://nextjs.org/docs/app/api-reference/config/next-config-js/output),
  `node server.js` on port 3000.
- the cache seed: the pages `next build` prerendered. They are loaded into the
  shared page cache before the release serves traffic, so first visitors get
  cached pages.

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

The build sets that environment's endpoint as `GC_MAGENTO_ENDPOINT`. That
value overrides `magentoEndpoint` in `graphcommerce.config.js`. Before
building, the build checks that the endpoint returns a GraphQL response.

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

The shared cache is on or off per project in the console. Switching it needs no
new build: the operator adds or removes `CACHE_DIR` and the `/cache` storage and
restarts the environment, and the handler follows `CACHE_DIR`.

**A repository with its own `Dockerfile`** gets none of the above, so the shared
cache stores nothing until the build carries a handler. Add once, then build:

- `cacheHandler` pointing at a handler that writes to `process.env.CACHE_DIR`
  when it is set. Copy
  [`recipes/nextjs/platform/cache-handler.mjs`](recipes/nextjs/platform/cache-handler.mjs)
  next to your `next.config` and make sure it is in the standalone output.
- `cacheMaxMemorySize: 0`, so no instance serves a stale copy from memory.

A `Dockerfile` in your repository replaces the recipe. A file in `.platform/`
replaces the recipe file with the same name.

## Laravel

- **PHP**: from `require.php` in `composer.json`, 8.2 to 8.4.
- **Images**: `recipes/laravel` builds the targets `php-fpm` (role `app`) and
  `nginx` (role `nginx`), with your Vite or Mix assets if there is a
  `package.json`.
- **Own files**: a `Dockerfile` in your repository replaces the recipe. A file
  in `.platform/` replaces the recipe file with the same name.
- **Configuration**: environment variables set `DB_*`, `REDIS_*`, `APP_URL`,
  `APP_KEY`, mail and S3. Do not commit a `.env` file.
- **Migrations**: `php artisan migrate --force` runs before a release serves
  traffic, unless the project declares its own release commands. If a
  migration fails, the previous release keeps serving.
