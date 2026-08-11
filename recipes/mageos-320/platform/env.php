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
                'backend' => 'Magento\\Framework\\Cache\\Backend\\Redis',
                'backend_options' => [
                    'server' => $e('REDIS_CACHE_HOST', 'redis-cache'),
                    'port' => $e('REDIS_CACHE_PORT', '6379'),
                    'database' => $e('REDIS_CACHE_DB', '0'),
                ],
            ],
            'page_cache' => [
                'backend' => 'Magento\\Framework\\Cache\\Backend\\Redis',
                'backend_options' => [
                    'server' => $e('REDIS_CACHE_HOST', 'redis-cache'),
                    'port' => $e('REDIS_CACHE_PORT', '6379'),
                    'database' => '1',
                ],
            ],
        ],
    ],
    'http_cache_hosts' => [['host' => $e('VARNISH_HOST', 'varnish-service'), 'port' => 80]],
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
    ];
}

// Pin minification to the BUILD-TIME REALITY.
//
// THE RULE: runtime config must describe the artefacts actually baked into the image.
// In production mode Magento never regenerates static artefacts at runtime
// (Framework\View\Design\FileResolution\Fallback\TemplateFile::getFile() returns the
// minified *path* under MODE_PRODUCTION without minifying), so any runtime config that
// disagrees with the build makes Magento ask for files the image does not contain.
//
// THE VALUES BELOW FOLLOW FROM THAT RULE FOR *THIS* SCAFFOLD, and only for it: this
// image runs setup:static-content:deploy at BUILD time with NO database, so it uses the
// config.xml defaults — i.e. minification OFF — and therefore bakes UNMINIFIED assets.
// An imported database that switches minification ON then breaks the site:
//   * dev/template/minify_html=1 -> templates resolve to var/view_preprocessed/... which
//     was never generated (and is masked by the var/ emptyDir anyway) => HTTP 500 on
//     every page;
//   * dev/js|css/minify_files=1  -> asset URLs become *.min.js / *.min.css, which were
//     never deployed => HTTP 404 on every stylesheet and script.
// Legacy servers get away with minification because they run static-content:deploy on
// the server, with the database attached, onto a persistent var/.
//
// DO NOT COPY THESE VALUES BLINDLY INTO A DIFFERENT IMAGE MODEL. A vendor-freeze image
// (pub/static copied verbatim from a legacy server whose DB had minification ON)
// contains ONLY minified assets and needs js/css set to '1' — the INVERSE — while
// minify_html stays '0' because var/view_preprocessed is still absent. Re-derive from
// the tree that is actually in the image, e.g.:
//   find pub/static/frontend -name '*.min.css' | wc -l   # 0 => js/css must be '0'
$config['system']['default']['dev'] = [
    'template' => ['minify_html' => '0'],
    'js' => ['minify_files' => '0'],
    'css' => ['minify_files' => '0'],
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
