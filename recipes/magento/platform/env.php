<?php
/**
 * Platform-injected Magento env.php. Reads configuration from environment
 * variables provided by the platform stack (the magento-env ConfigMap +
 * the operator-synced secrets), so the image is environment-agnostic.
 */
$e = static fn(string $k, $d = null) => getenv($k) !== false ? getenv($k) : $d;

// Every cache type this codebase declares, generated AT BUILD TIME by the Dockerfile
// (see the cache_types.php step there for the full rationale and the regeneration
// command). A cache type ABSENT from `cache_types` is DISABLED — Magento's
// Framework\App\Cache\State::isEnabled() returns false for a missing key, silently.
// The list depends on the Magento version, the edition AND the installed third-party
// modules, so it is derived from the vendor/app tree rather than hardcoded here.
//
// The fallback below only applies if this file is used OUTSIDE the platform build (the
// Dockerfile fails the build if it cannot generate the list). It is the Magento OPEN
// SOURCE 2.4.7 list — on Adobe Commerce it silently omits target_rule, admin_ui_sdk and
// webhooks_response, and it knows nothing about third-party cache types.
$cacheTypesFile = __DIR__ . '/cache_types.php';
$cacheTypes = is_file($cacheTypesFile) ? include $cacheTypesFile : [
    'config', 'layout', 'block_html', 'collections', 'reflection', 'db_ddl',
    'compiled_config', 'eav', 'customer_notification', 'config_integration',
    'config_integration_api', 'graphql_query_resolver_result', 'full_page',
    'config_webservice', 'translate',
];

// The cache backend's NAME, which changed meaning in 2.4.9. From 2.4.9 Magento caches
// through Symfony: SymfonyAdapterProvider resolves the backend by a short name ('redis'
// or 'valkey') and silently falls back to the FILESYSTEM for anything else — including
// the class name below, which made 2.4.9 run without its Redis cache (found 2026-10-03:
// zero commands on redis-cache during a full page render). Before 2.4.9 the old factory
// needs that class name and would treat 'redis' as unknown in turn. One recipe builds
// every line, so ask the codebase which cache layer it has.
$cacheBackend = class_exists(\Magento\Framework\Cache\Frontend\Adapter\SymfonyAdapterProvider::class)
    ? 'redis'
    : 'Magento\\Framework\\Cache\\Backend\\Redis';

$config = [
    // Marks the deployment as installed (the DB is installed out-of-band by the
    // provisioner). Without this, Magento redirects everything to /setup/.
    'install' => ['date' => $e('MAGENTO_INSTALL_DATE', 'Wed, 01 Jan 2025 00:00:00 +0000')],
    'backend' => ['frontName' => $e('MAGENTO_ADMIN_PATH', 'admin')],
    'crypt' => ['key' => $e('MAGENTO_CRYPT_KEY')],
    'db' => [
        'connection' => [
            'default' => [
                'host' => $e('DB_HOST', 'magento-mysql-haproxy'),
                'dbname' => $e('DB_NAME', 'magento'),
                'username' => $e('DB_USER', 'root'),
                'password' => $e('DB_PASSWORD', ''),
                'active' => '1',
                'driver_options' => [1014 => false],
            ],
        ],
        'table_prefix' => '',
    ],
    'resource' => ['default_setup' => ['connection' => 'default']],
    'x-frame-options' => 'SAMEORIGIN',
    'MAGE_MODE' => $e('MAGE_MODE', 'production'),
    'cache_types' => array_fill_keys($cacheTypes, 1),
    'session' => [
        'save' => 'redis',
        'redis' => [
            'host' => $e('REDIS_SESSION_HOST', 'redis-session'),
            'port' => $e('REDIS_SESSION_PORT', '6379'),
            'database' => $e('REDIS_SESSION_DB', '0'),
            'disable_locking' => '1',
        ],
    ],
    'cache' => [
        'frontend' => [
            'default' => [
                'backend' => $cacheBackend,
                'backend_options' => [
                    'server' => $e('REDIS_CACHE_HOST', 'redis-cache'),
                    'port' => $e('REDIS_CACHE_PORT', '6379'),
                    'database' => $e('REDIS_CACHE_DB', '0'),
                ],
            ],
            'page_cache' => [
                'backend' => $cacheBackend,
                'backend_options' => [
                    'server' => $e('REDIS_CACHE_HOST', 'redis-cache'),
                    'port' => $e('REDIS_CACHE_PORT', '6379'),
                    'database' => '1',
                ],
            ],
        ],
    ],
    'queue' => [
        'amqp' => [
            'host' => $e('RABBITMQ_HOST', 'rabbitmq'),
            'port' => $e('RABBITMQ_PORT', '5672'),
            'user' => $e('RABBITMQ_USER', 'magento'),
            'password' => $e('RABBITMQ_PASSWORD', ''),
            'virtualhost' => '/',
        ],
    ],
    'system' => [
        'default' => [
            'catalog' => ['search' => [
                'engine' => 'opensearch',
                'opensearch_server_hostname' => $e('OPENSEARCH_HOST', 'opensearch'),
                'opensearch_server_port' => $e('OPENSEARCH_PORT', '9200'),
                'opensearch_index_prefix' => 'magento2',
                'opensearch_enable_auth' => '0',
            ]],
        ],
    ],
    'directories' => ['document_root_is_pub' => true],
];

// The page cache in front of this environment (App.spec.front.pageCache).
// Varnish on — the default — is where Magento sends its purges. Off, the
// operator leaves VARNISH_HOST empty: there is no HTTP cache host to purge,
// and Magento serves its built-in full page cache (caching_application 1),
// pinned here so a database that says "Varnish" cannot leave the shop
// without any page cache at all.
$varnishHost = $e('VARNISH_HOST', 'varnish-service');
if ($varnishHost !== '') {
    $config['http_cache_hosts'] = [['host' => $varnishHost, 'port' => 80]];
} else {
    $config['system']['default']['system']['full_page_cache']['caching_application'] = '1';
}

// Queue consumers (deployyy-operator docs/MAGENTO-PROCESSES.md). The operator sets
// these only when the project DECLARES its consumers (Magento2App.spec.magento.consumers);
// unset, nothing is written and Magento's defaults apply, exactly as before.
//
//   MAGENTO_CONSUMERS_RUNNER=cron  every consumer is started by Magento's own
//                                  `consumers_runner` cron job (mode `all`);
//   MAGENTO_CONSUMERS_RUNNER=off   cron starts none — the consumer process runs
//                                  exactly the declared list (mode `listed`);
//   MAGENTO_CONSUMERS_MAX_MESSAGES the runner's max_messages;
//   MAGENTO_CONSUMERS_WAIT_FOR_MESSAGES=0  an idle consumer exits instead of
//                                  blocking, so the operator's shared loop moves on
//                                  to the next consumer (and cron-started ones do
//                                  not idle in the cron process's memory).
$consumersRunner = $e('MAGENTO_CONSUMERS_RUNNER');
if ($consumersRunner === 'cron' || $consumersRunner === 'off') {
    $config['cron_consumers_runner'] = [
        'cron_run' => $consumersRunner === 'cron',
        // An empty value counts as unset: (int) '' is 0, which Magento reads as
        // "no limit".
        'max_messages' => (int) (($e('MAGENTO_CONSUMERS_MAX_MESSAGES') ?? '') !== ''
            ? $e('MAGENTO_CONSUMERS_MAX_MESSAGES') : '1000'),
        'consumers' => [],
    ];
}
if (($e('MAGENTO_CONSUMERS_WAIT_FOR_MESSAGES') ?? '') !== '') {
    $config['queue']['consumers_wait_for_messages'] = (int) $e('MAGENTO_CONSUMERS_WAIT_FOR_MESSAGES');
}

// Pin the base URL from the environment rather than trusting core_config_data.
//
// env.php's `system` section is the HIGHEST-precedence config source
// (systemConfigInitialDataProvider, sortOrder 1000 — above the DB's 100), so this
// cleanly overrides core_config_data at default scope. A database imported from another
// environment carries THAT environment's base_url, which would redirect this env away
// from itself. Changing MAGENTO_BASE_URL in the magento-env ConfigMap is what moves the
// env to a new hostname (e.g. at cutover).
//
// Guarded: an unset var must not write a null base_url, which breaks Magento.
// NB only default scope is pinned — a store/website-scoped core_config_data row for the
// same path still wins, so check for one before relying on this.
if ($e('MAGENTO_BASE_URL')) {
    $config['system']['default']['web'] = [
        'unsecure' => ['base_url' => $e('MAGENTO_BASE_URL')],
        'secure' => ['base_url' => $e('MAGENTO_BASE_URL')],
        // redirect_to_base=0 so the env still SERVES on its other hosts (the
        // automatic deployyy.app host always routes) instead of 301-ing them to
        // the canonical one. The operator decides the canonical host; this just
        // stops Magento fighting it.
        'url' => ['redirect_to_base' => '0'],
    ];
}

// Per-store base_urls — classic multi-domain multi-store. The operator emits a
// JSON object {store_view_code: base_url} (MAGENTO_STORE_BASE_URLS) from the
// Environment's storeDomains; each lands in the `stores` scope so that store
// view generates its URLs on its own domain. env.php's `system` section
// outranks core_config_data at every scope, so this pins the store base_url
// regardless of the imported database. The paired nginx Host->MAGE_RUN_CODE map
// makes a Luma frontend resolve the store natively; a headless GraphCommerce
// frontend ignores the run-code but still needs these for correct absolute URLs.
if ($e('MAGENTO_STORE_BASE_URLS')) {
    $storeBaseUrls = json_decode($e('MAGENTO_STORE_BASE_URLS'), true);
    if (is_array($storeBaseUrls)) {
        foreach ($storeBaseUrls as $storeCode => $baseUrl) {
            if (!is_string($storeCode) || !is_string($baseUrl) || $baseUrl === '') {
                continue;
            }
            $config['system']['stores'][$storeCode]['web'] = [
                'unsecure' => ['base_url' => $baseUrl],
                'secure' => ['base_url' => $baseUrl],
            ];
        }
    }
}

// Dedicated admin hostname — Magento's custom admin URL. When the operator sets
// MAGENTO_ADMIN_BASE_URL (from the Environment's adminDomain) the admin is
// served on its own host instead of the storefront's. Empty = admin stays on
// the default host + the admin frontName (the common case), so this block is
// inert unless a dedicated admin domain is declared.
if ($e('MAGENTO_ADMIN_BASE_URL')) {
    $config['system']['default']['admin']['url'] = [
        'use_custom' => '1',
        'custom' => $e('MAGENTO_ADMIN_BASE_URL'),
    ];
}

// Pin minification to the BUILD-TIME REALITY.
//
// THE RULE: runtime config must describe the artefacts actually baked into the image.
// In production mode Magento never regenerates static artefacts at runtime
// (Framework\View\Design\FileResolution\Fallback\TemplateFile::getFile() returns the
// minified *path* under MODE_PRODUCTION without minifying), so any runtime config that
// disagrees with the build makes Magento ask for files the image does not contain:
//   * dev/template/minify_html=1 -> templates resolve to var/view_preprocessed/... which
//     is not in the image (var/ is an emptyDir) => HTTP 500 on every page;
//   * dev/js|css/minify_files=1 on an unminified build -> *.min.js / *.min.css URLs that
//     were never deployed => HTTP 404 on every stylesheet and script (and the inverse
//     for a minified build).
// An imported database routinely carries the legacy server's settings, so the image
// decides. The Dockerfile records what static-content:deploy produced in
// app/etc/static_build.php (derived from the files in pub/static, not from a setting);
// this pins exactly that. env.php's `system` section outranks core_config_data.
//
// The fallback applies only outside the platform build: a DB-less deploy with no
// config.php overrides bakes unminified assets.
$staticBuildFile = __DIR__ . '/static_build.php';
$staticBuild = is_file($staticBuildFile) ? include $staticBuildFile : ['js' => '0', 'css' => '0', 'html' => '0'];
$config['system']['default']['dev'] = [
    'template' => ['minify_html' => $staticBuild['html']],
    'js' => ['minify_files' => $staticBuild['js']],
    'css' => ['minify_files' => $staticBuild['css']],
];

// Media lives on S3 (pub/media -> s3://<bucket>/media/), matching the other Magento envs
// on this platform. The stack injects the magento-s3 secret via envFrom; the pods mount
// NO media volume (var/ is an emptyDir), so without this Magento would write uploads to
// the container filesystem — lost on every restart and never shared across replicas.
//
// Guarded on AWS_S3_BUCKET so an env without S3 configured falls back to local storage
// rather than booting with a broken remote_storage driver.
if ($e('AWS_S3_BUCKET')) {
    $config['remote_storage'] = [
        'driver' => 'aws-s3',
        'prefix' => '',
        'config' => [
            'bucket' => $e('AWS_S3_BUCKET'),
            'region' => $e('AWS_S3_REGION', 'gra'),
            'endpoint' => $e('AWS_S3_ENDPOINT'),
            'path_style' => '1',
            'key' => $e('AWS_ACCESS_KEY_ID'),
            'secret' => $e('AWS_SECRET_ACCESS_KEY'),
        ],
    ];
}

return $config;
