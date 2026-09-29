# syntax=docker/dockerfile:1.7
# PLATFORM BUILD RECIPE — Magento Open Source 2.4.6, 2.4.7, 2.4.8 and 2.4.9
# (lines magento-246 … magento-249). Consumed by the reusable magento2-build
# workflow, which overlays it into a project checkout UNLESS the project ships
# its own Dockerfile (a repo-local Dockerfile wins wholesale; repo-local
# .platform/* files win per file). Derived from the live-validated mageos-3
# recipe; the differences are marked "(not in mageos-3)".
#
# Build-args (the workflow resolves them):
#   PHP_VERSION      — 8.2 (2.4.6), 8.3 (2.4.7), 8.4 (2.4.8, 2.4.9).
#   PLATFORM_LOCALES — space-separated store-view locales for the DB-less
#                      static-content:deploy (en_US default).
#   PLATFORM_GRAPHQL_POST_CACHE — 1 (default) adds
#                      GraphCommerce_GraphQlVarnishPostCache, 0 leaves it out.
#
# The known traps of these lines, and where each is handled:
#   - cache_types follow the INSTALLED modules, not the version: generated
#     from the vendor/app tree into app/etc/cache_types.php (below).
#   - queue_poison_pill has no primary key in core, which Group Replication
#     rejects: a platform module adds one when no module in the tree does
#     (below).
#   - composer-patches need patches/ in the context BEFORE composer install
#     (below).
#   - runtime minification must match the baked artefacts: the build records
#     what static-content:deploy produced in app/etc/static_build.php and
#     env.php pins exactly that (below, and .platform/env.php).
ARG PHP_VERSION=8.4

# Stage 1: dependencies + compile + static content
FROM thecodingmachine/php:${PHP_VERSION}-v5-fpm-node22 AS builder

ENV PHP_EXTENSION_GD=1 PHP_EXTENSION_INTL=1 PHP_EXTENSION_OPCACHE=1 \
    PHP_EXTENSION_PDO_MYSQL=1 PHP_EXTENSION_BCMATH=1 PHP_EXTENSION_SOAP=1 \
    PHP_EXTENSION_SOCKETS=1 PHP_EXTENSION_SODIUM=1 PHP_EXTENSION_XSL=1 \
    PHP_EXTENSION_ZIP=1 PHP_EXTENSION_PCNTL=1 PHP_INI_MEMORY_LIMIT=4G

USER docker
COPY --chown=docker:docker auth.json /home/docker/.composer/auth.json
COPY --chown=docker:docker composer.json composer.lock* /tmp/magento/
# The project's own patches (vaimo/composer-patches, `extra.patches-search: patches` or
# `extra.patches` sources under ./patches/) MUST be in the context BEFORE `composer install`
# — the plugin applies them during install, and a missing directory is a SILENT no-op:
# every patch skipped, build green, unpatched image shipped. The character-class glob keeps
# the directory OPTIONAL (COPY fails on a plain missing source; with composer.json as a
# guaranteed match it does not); the stray composer.json copy is removed again.
COPY --chown=docker:docker composer.json patche[s] /tmp/magento/patches/
RUN rm -f /tmp/magento/patches/composer.json
WORKDIR /tmp/magento
RUN composer install --no-interaction --no-dev --prefer-dist --optimize-autoloader \
    && rm -f /home/docker/.composer/auth.json
COPY --chown=docker:docker . /tmp/magento/
# NEVER let Marketplace credentials reach the published image: `COPY .` above pulls in
# the workflow-written auth.json, and the php-fpm stage ships this whole directory.
RUN rm -f /tmp/magento/auth.json
# (not in mageos-3) The workflow's checkout of this repo sits in the build context; it is
# not part of the application.
RUN rm -rf /tmp/magento/.platform-recipes

# (not in mageos-3) Platform modules. Each goes to app/code (Magento's own
# non-composer module path, loaded by app/etc/NonComposerComponentRegistration.php)
# and is enabled in app/etc/config.php, so the project's composer.lock is not touched.
# A module the project already has, or disabled on purpose in config.php, is left alone.
#
# 1. GraphCommerce_GraphQlVarnishPostCache (M11): makes POST /graphql with an
#    X-Document-ID header cacheable in Varnish — GraphCommerce sends POST. Pinned by
#    git commit (content-addressed; GitHub tarballs are not byte-stable). Its
#    config.xml defaults graphql/session/disable to 1, which POST caching needs.
#    https://github.com/graphcommerce-org/magento2-graphcommerce_graphqlvarnishpostcache
ARG PLATFORM_GRAPHQL_POST_CACHE=1
ARG GRAPHQL_POST_CACHE_REPO=https://github.com/graphcommerce-org/magento2-graphcommerce_graphqlvarnishpostcache
ARG GRAPHQL_POST_CACHE_COMMIT=0954df63d43b983bb2a89014f998e2d990d9a86f
RUN set -eu; \
    if [ "$PLATFORM_GRAPHQL_POST_CACHE" != "1" ]; then echo "GraphQL POST cache module: off (build-arg)"; exit 0; fi; \
    if grep -rqs --include=registration.php "'GraphCommerce_GraphQlVarnishPostCache'" vendor app/code; then \
      echo "GraphQL POST cache module: the project has it, platform copy not added"; exit 0; fi; \
    tmp=$(mktemp -d); git -C "$tmp" init -q; \
    git -C "$tmp" fetch -q --depth 1 "$GRAPHQL_POST_CACHE_REPO" "$GRAPHQL_POST_CACHE_COMMIT"; \
    test "$(git -C "$tmp" rev-parse FETCH_HEAD)" = "$GRAPHQL_POST_CACHE_COMMIT"; \
    git -C "$tmp" checkout -q FETCH_HEAD; \
    mkdir -p app/code/GraphCommerce; \
    cp -r "$tmp/src/GraphQlVarnishPostCache" app/code/GraphCommerce/GraphQlVarnishPostCache; \
    rm -rf app/code/GraphCommerce/GraphQlVarnishPostCache/Test "$tmp"; \
    echo "GraphQL POST cache module: added at $GRAPHQL_POST_CACHE_COMMIT"
# 2. Deployyy_QueuePoisonPillPk: core declares queue_poison_pill (Magento_MessageQueue)
#    with one column and NO primary key, in every 2.4.x release. Percona/MySQL Group
#    Replication rejects all DML on a table without a PK, and
#    Magento\MessageQueue\Setup\Recurring writes to it on every setup:upgrade and cache
#    flush. This module declares the id column + PRIMARY in declarative schema, so
#    setup:upgrade adds them (existing rows get ids). Added only when no module in the
#    tree already declares a primary key for the table.
COPY --chown=docker:docker <<'EOF' /tmp/platform-modules/QueuePoisonPillPk/registration.php
<?php
// Added by the deployyy platform build (recipes/magento/Dockerfile).
\Magento\Framework\Component\ComponentRegistrar::register(
    \Magento\Framework\Component\ComponentRegistrar::MODULE,
    'Deployyy_QueuePoisonPillPk',
    __DIR__
);
EOF
COPY --chown=docker:docker <<'EOF' /tmp/platform-modules/QueuePoisonPillPk/etc/module.xml
<?xml version="1.0"?>
<!-- Added by the deployyy platform build (recipes/magento/Dockerfile). -->
<config xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
        xsi:noNamespaceSchemaLocation="urn:magento:framework:Module/etc/module.xsd">
    <module name="Deployyy_QueuePoisonPillPk">
        <sequence>
            <module name="Magento_MessageQueue"/>
        </sequence>
    </module>
</config>
EOF
COPY --chown=docker:docker <<'EOF' /tmp/platform-modules/QueuePoisonPillPk/etc/db_schema.xml
<?xml version="1.0"?>
<!--
    Added by the deployyy platform build (recipes/magento/Dockerfile).
    queue_poison_pill needs a primary key under Group Replication (ERROR 3098 on any DML).
-->
<schema xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
        xsi:noNamespaceSchemaLocation="urn:magento:framework:Setup/Declaration/Schema/etc/schema.xsd">
    <table name="queue_poison_pill" resource="default" engine="innodb" comment="Sequence table for poison pill versions">
        <column xsi:type="int" name="id" unsigned="true" nullable="false" identity="true" comment="Entity ID"/>
        <constraint xsi:type="primary" referenceId="PRIMARY">
            <column name="id"/>
        </constraint>
    </table>
</schema>
EOF
COPY --chown=docker:docker <<'EOF' /tmp/platform-modules/QueuePoisonPillPk/etc/db_schema_whitelist.json
{
    "queue_poison_pill": {
        "column": {
            "id": true
        },
        "constraint": {
            "PRIMARY": true
        }
    }
}
EOF
# Opt-in (PLATFORM_QUEUE_POISON_PILL_PK=1): it changes the schema of the live
# database on the next setup:upgrade, so a project turns it on deliberately. Needed
# only on Group Replication (Percona); MariaDB and single-node MySQL accept the table.
ARG PLATFORM_QUEUE_POISON_PILL_PK=0
RUN set -eu; \
    if [ "$PLATFORM_QUEUE_POISON_PILL_PK" != "1" ]; then \
      echo "queue_poison_pill: off (build-arg PLATFORM_QUEUE_POISON_PILL_PK=1 turns it on)"; \
      rm -rf /tmp/platform-modules; exit 0; fi; \
    found=$(find vendor app/code -name db_schema.xml -print0 2>/dev/null \
      | xargs -0 -r php -r 'foreach (array_slice($argv, 1) as $f) { $x = @simplexml_load_file($f); if ($x && $x->xpath("//table[@name=\"queue_poison_pill\"]/constraint[@*[local-name()=\"type\"]=\"primary\"]")) echo "$f\n"; }'); \
    if [ -n "$found" ]; then \
      echo "queue_poison_pill: primary key declared in $found, platform module not added"; \
    else \
      mkdir -p app/code/Deployyy && cp -r /tmp/platform-modules/QueuePoisonPillPk app/code/Deployyy/QueuePoisonPillPk; \
      echo "queue_poison_pill: no primary key in the tree, Deployyy_QueuePoisonPillPk added"; \
    fi; \
    rm -rf /tmp/platform-modules
# Enable the platform modules in config.php. An explicit 0 in the project's config.php
# is the project's decision and stays. A config.php without a module list (a project
# that never ran module:enable) is left alone: Magento cannot compile it either way.
RUN php -r '$f = "app/etc/config.php"; if (!is_file($f)) exit(0); $c = include $f; if (!isset($c["modules"])) exit(0); \
      foreach (["GraphCommerce_GraphQlVarnishPostCache" => "app/code/GraphCommerce/GraphQlVarnishPostCache", "Deployyy_QueuePoisonPillPk" => "app/code/Deployyy/QueuePoisonPillPk"] as $m => $d) { \
        if (!is_dir($d)) continue; \
        if (array_key_exists($m, $c["modules"])) { fwrite(STDERR, "$m: config.php says {$c["modules"][$m]}, kept\n"); continue; } \
        $c["modules"][$m] = 1; fwrite(STDERR, "$m: enabled\n"); } \
      file_put_contents($f, "<?php\nreturn " . var_export($c, true) . ";\n");'

# Ensure app/etc/config.php defines default store scopes — static-content:deploy
# needs a default website at build time (no DB). Injected only if absent.
RUN php -r '$f="app/etc/config.php"; $c=file_exists($f)?(include $f):[]; if(empty($c["scopes"]["websites"])){ $c["scopes"]=["websites"=>["base"=>["website_id"=>"1","code"=>"base","name"=>"Main Website","sort_order"=>"0","default_group_id"=>"1","is_default"=>"1"]],"groups"=>["1"=>["group_id"=>"1","website_id"=>"1","name"=>"Main Website Store","root_category_id"=>"2","default_store_id"=>"1"]],"stores"=>["default"=>["store_id"=>"1","code"=>"default","website_id"=>"1","group_id"=>"1","name"=>"Default Store View","sort_order"=>"0","is_active"=>"1"]]]; file_put_contents($f,"<?php\nreturn ".var_export($c,true).";\n"); fwrite(STDERR,"default scopes injected\n"); }'
# Bake the authoritative list of cache types this codebase declares.
#
# A cache type that is ABSENT from env.php's `cache_types` is DISABLED — Magento's
# Framework\App\Cache\State::isEnabled() returns false for a missing key, silently. The list
# varies by version AND edition AND installed third-party modules (CE 2.4.6 = 14 types,
# CE 2.4.7 = 15, a third-party module can add its own), so it is generated from the tree.
# env.php reads the generated app/etc/cache_types.php.
RUN set -eu; \
    find vendor app -path '*/etc/cache.xml' -print0 2>/dev/null \
      | xargs -0 -r grep -ho '<type name="[a-z_]*"' \
      | sed 's/.*name="//;s/"//' | sort -u > /tmp/cache_types.txt; \
    test -s /tmp/cache_types.txt || { echo "FATAL: no cache.xml <type> entries found"; exit 1; }; \
    { echo "<?php"; \
      echo "// GENERATED AT BUILD TIME by the platform Dockerfile — do not edit."; \
      echo "// Every cache type this codebase declares; a type missing here is DISABLED."; \
      echo "return ["; \
      sed 's/^/    "/; s/$/",/' /tmp/cache_types.txt; \
      echo "];"; } > app/etc/cache_types.php; \
    echo "cache types baked: $(wc -l < /tmp/cache_types.txt) -> $(tr '\n' ' ' < /tmp/cache_types.txt)"
# Static content must be deployed for EVERY locale the store views use. The build is
# DB-less, so Magento cannot discover the locales itself — they come from
# PLATFORM_LOCALES and must be KEPT IN SYNC WITH THE STORE VIEWS. Too few locales does not
# fail the build or a page render: the other store views return HTTP 200 while every
# CSS/JS asset they reference 404s. Verify per-locale assets over HTTP.
ARG PLATFORM_LOCALES="en_US"
RUN php bin/magento setup:di:compile \
    && php bin/magento setup:static-content:deploy -f -j"$(nproc)" ${PLATFORM_LOCALES} \
    && rm -rf var/cache var/page_cache var/generation
# (not in mageos-3) Record what static-content:deploy actually baked, for env.php.
#
# In production mode Magento never regenerates static artefacts at runtime, so the
# runtime minification settings must describe the files in the image. The build is
# DB-less: static-content:deploy used config.php's `system` values or the config.xml
# defaults (off). A database imported later can say otherwise; env.php pins what is here.
#   js  — minified iff the deploy wrote requirejs-min-resolver.min.js (Magento writes it
#         only with dev/js/minify_files on).
#   css — minified iff a theme's styles-m.min.css exists (Magento/luma and Magento/blank
#         both have styles-m; with minification off only styles-m.css exists).
#   html — always 0: minified templates go to var/view_preprocessed, which is not in the
#         runtime image (var/ is an emptyDir).
RUN set -eu; \
    js=0; css=0; \
    if [ -n "$(find pub/static/frontend -name requirejs-min-resolver.min.js -print -quit)" ]; then js=1; fi; \
    if [ -n "$(find pub/static/frontend -name styles-m.min.css -print -quit)" ]; then css=1; fi; \
    printf '<?php\n// GENERATED AT BUILD TIME by the platform Dockerfile — do not edit.\n// What static-content:deploy baked into this image; env.php pins runtime config to it.\nreturn ["js" => "%s", "css" => "%s", "html" => "0"];\n' "$js" "$css" > app/etc/static_build.php; \
    echo "static build: js minified=$js css minified=$css html minified=0"
# WORKAROUND — guarantee the adminhtml JS translation dictionary exists. The DB-less
# static-content:deploy never writes js-translation.json for adminhtml (the generator
# needs the `translation` table and only logs its failure), so the admin 404s on it.
# An empty dictionary is correct for an en_US admin; a non-English admin loses its JS
# translations until the platform has a DB-backed deploy. See recipes/mageos-3.
RUN set -eu; for d in pub/static/adminhtml/*/*/*/; do \
      [ -d "$d" ] || continue; \
      [ -e "$d/js-translation.json" ] || { printf '{}' > "$d/js-translation.json"; echo "seeded empty js-translation.json in $d"; }; \
    done
# The injected scopes were only needed for the DB-less static-content:deploy. Strip them
# so the runtime image ships a scope-free config.php (a baked scope block makes
# app:config:import choke on a fresh install).
RUN php -r '$f="app/etc/config.php"; $c=include $f; unset($c["scopes"]); file_put_contents($f,"<?php\nreturn ".var_export($c,true).";\n"); fwrite(STDERR,"build-only scopes stripped\n");'

# Stage 2: php-fpm runtime
#
# (not in mageos-3) Runs under Pod Security Standard `restricted`: numeric non-root
# user, no sudo, nothing that needs privilege escalation. The base image's entrypoint
# re-execs itself through `sudo` to write PHP config from PHP_EXTENSION_*/PHP_INI_*
# variables; here the few files it writes are owned by the app user, and a sudo-less
# entrypoint (.platform/php-entrypoint.sh) and `php` wrapper (.platform/php-wrapper.sh)
# do the same work. Runtime PHP_INI_* variables (dev mode sets opcache ones) still work.
# See docs/PSS-RESTRICTED.md.
FROM thecodingmachine/php:${PHP_VERSION}-v5-fpm AS php-fpm
ENV PHP_EXTENSION_GD=1 PHP_EXTENSION_INTL=1 PHP_EXTENSION_OPCACHE=1 \
    PHP_EXTENSION_PDO_MYSQL=1 PHP_EXTENSION_BCMATH=1 PHP_EXTENSION_SOAP=1 \
    PHP_EXTENSION_SOCKETS=1 PHP_EXTENSION_SODIUM=1 PHP_EXTENSION_XSL=1 \
    PHP_EXTENSION_ZIP=1 PHP_EXTENSION_PCNTL=1 PHP_INI_MEMORY_LIMIT=2G \
    PHP_INI_OPCACHE__ENABLE=1 PHP_INI_DATE__TIMEZONE=Europe/Amsterdam \
    TEMPLATE_PHP_INI=production
USER root
RUN apt-get update && apt-get install -y --no-install-recommends default-mysql-client \
    && rm -rf /var/lib/apt/lists/*

# Mailpit sendmail drop-in: PHP's mail() pipes to sendmail, which this image lacks, so
# Magento mail failed SILENTLY. sendmail-tagged relays over SMTP to MP_SENDMAIL_SMTP_ADDR
# (the platform's Mailpit catcher) and adds X-Tags from MAIL_TAG. The ini paths are
# PHP-version-specific, so they follow $PHP_VERSION (the base image sets it). Both fpm AND
# cli: cron and consumers send mail from the CLI SAPI.
ENV MAILPIT_VERSION=v1.30.6 \
    MAILPIT_SHA256=00f7570bf47c9683944f6189379ce4b7d4974b1c9cd911f93c55d4ad945bf3d2
COPY .platform/sendmail-tagged /usr/local/bin/sendmail-tagged
RUN curl -fsSL "https://github.com/axllent/mailpit/releases/download/${MAILPIT_VERSION}/mailpit-linux-amd64.tar.gz" \
        -o /tmp/mailpit.tar.gz \
    && echo "${MAILPIT_SHA256}  /tmp/mailpit.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/mailpit.tar.gz -C /usr/local/bin mailpit \
    && chmod +x /usr/local/bin/mailpit /usr/local/bin/sendmail-tagged \
    && rm /tmp/mailpit.tar.gz \
    && printf 'sendmail_path = /usr/local/bin/sendmail-tagged -t\n' \
        | tee "/etc/php/${PHP_VERSION}/fpm/conf.d/zz-mailpit-sendmail.ini" \
              "/etc/php/${PHP_VERSION}/cli/conf.d/zz-mailpit-sendmail.ini" > /dev/null

# Unprivileged PHP: the app user owns the few files the config step writes, the php.ini
# templates are linked now (the entrypoint did that as root), and sudo is gone.
COPY --chmod=0755 .platform/php-entrypoint.sh /usr/local/bin/deployyy-php-entrypoint.sh
COPY --chmod=0755 .platform/php-wrapper.sh /usr/bin/php
RUN set -eux; \
    ln -sf "/usr/lib/php/${PHP_VERSION}/php.ini-${TEMPLATE_PHP_INI}.cli" "/etc/php/${PHP_VERSION}/cli/php.ini"; \
    ln -sf "/usr/lib/php/${PHP_VERSION}/php.ini-${TEMPLATE_PHP_INI}" "/etc/php/${PHP_VERSION}/fpm/php.ini"; \
    test -e "/etc/php/${PHP_VERSION}/cli/php.ini" && test -e "/etc/php/${PHP_VERSION}/fpm/php.ini"; \
    touch "/etc/php/${PHP_VERSION}/mods-available/generated_conf.ini"; \
    chown -R docker:docker "/etc/php/${PHP_VERSION}/cli/conf.d" "/etc/php/${PHP_VERSION}/fpm/conf.d" \
        /var/lib/php/modules "/etc/php/${PHP_VERSION}/mods-available/generated_conf.ini" \
        /opt/php_env_var_cache.php; \
    gpasswd -d docker sudo || true; \
    rm -f /etc/sudoers.d/*; \
    su docker -s /bin/bash -c /usr/local/bin/deployyy-php-entrypoint.sh
COPY --from=builder --chown=docker:docker /tmp/magento /var/www/html
WORKDIR /var/www/html
COPY --chown=docker:docker .platform/env.php /var/www/html/app/etc/env.php
# Numeric, so the kubelet can verify runAsNonRoot without a securityContext.
LABEL app.deployyy.pod-security="restricted"
USER 1000:1000
ENTRYPOINT ["/usr/local/bin/deployyy-php-entrypoint.sh"]
CMD ["php-fpm"]

# Stage 3: nginx (serves pub/, proxies PHP to FPM on :9000)
#
# (not in mageos-3) nginx-unprivileged: the master runs as uid 101 and listens on
# 8080 (the platform port), pid and temp files under /tmp — no root, no capabilities.
FROM nginxinc/nginx-unprivileged:1.26-alpine AS nginx
COPY .platform/nginx-default.conf /etc/nginx/conf.d/default.conf
# Empty Host->store map so the `include` in default.conf resolves and nginx starts even
# when nothing is mounted. The operator overlays this file with the magento-host-store
# ConfigMap when store domains are bound. See nginx-default.conf.
USER root
# OS security fixes the base image does not have yet: the build's image scan
# fails on a fixable CRITICAL finding (.github/actions/build-scan).
# PLATFORM_OS_UPDATES_DAY (the build date) re-runs this once a day even when
# the layer is cached. nginx itself comes from nginx.org and is not upgraded.
ARG PLATFORM_OS_UPDATES_DAY=""
RUN apk upgrade --no-cache \
    && printf '# no store domains bound\n' > /etc/nginx/host-store.map
USER 101:101
COPY --from=builder /tmp/magento/pub /var/www/html/pub
LABEL app.deployyy.pod-security="restricted"
