# PLATFORM BUILD RECIPE — line mageos-320 (Mage OS 3.2.x = Magento core
# 2.4.9, PHP 8.4). Owned by the platform (deployyy-operator repo,
# platform/build/). The hosted build (deployyy-operator internal/build)
# overlays it into a project checkout UNLESS the project ships its own
# Dockerfile (a repo-local Dockerfile wins wholesale, and repo-local
# .platform/* files win per file). Improvements here reach every hosted
# project on its next build.
#
# Per-project knobs arrive as build-args (the hosted build resolves them):
#   PHP_VERSION      — 8.4 (the line's default), 8.5 or 8.3, the project's choice
#   (Magento 2.4.9 supports all three; 8.5 waits for its live trial before the
#   platform offers it).
#   PLATFORM_LOCALES — space-separated store-view locales for the DB-less
#   static-content:deploy (the project's build locales; en_US default).
ARG PHP_VERSION=8.4

# Stage 1: dependencies + compile + static content
# The directories of the composer.json path repositories, for the dependency
# step: a change elsewhere in the code keeps the dependency layer.
FROM thecodingmachine/php:${PHP_VERSION}-v5-fpm-node22 AS path-repositories
COPY . /tmp/src/
RUN php /tmp/src/.platform/path-repositories.php /tmp/src /tmp/path-repositories

FROM thecodingmachine/php:${PHP_VERSION}-v5-fpm-node22 AS builder

ENV PHP_EXTENSION_GD=1 PHP_EXTENSION_INTL=1 PHP_EXTENSION_OPCACHE=1 \
    PHP_EXTENSION_PDO_MYSQL=1 PHP_EXTENSION_BCMATH=1 PHP_EXTENSION_SOAP=1 \
    PHP_EXTENSION_SOCKETS=1 PHP_EXTENSION_SODIUM=1 PHP_EXTENSION_XSL=1 \
    PHP_EXTENSION_ZIP=1 PHP_EXTENSION_PCNTL=1 PHP_INI_MEMORY_LIMIT=4G

COPY .platform/php-extensions.php .platform/php-build-env.sh /usr/local/lib/deployyy/
USER docker
COPY --chown=docker:docker composer.json composer.lock* /tmp/magento/
COPY --from=path-repositories --chown=docker:docker /tmp/path-repositories/ /tmp/magento/
# The project's own patches (vaimo/composer-patches, `extra.patches-search: patches` or
# `extra.patches` sources under ./patches/) MUST be in the context BEFORE `composer install`
# — the plugin applies them during install, and a missing directory is a SILENT no-op:
# every patch skipped, build green, unpatched image shipped (seen on the StyleSmuggler
# 0-day, Sansec 2026-09-05, where the guard lives in exactly such a patch). The
# character-class glob keeps the directory OPTIONAL (COPY fails on a plain missing source;
# with composer.json as a guaranteed match it does not), so projects without patches build
# unchanged; the stray composer.json copy is removed again. Layer caching is preserved:
# only composer.json/lock and patches/ invalidate the install layer, not the whole tree.
COPY --chown=docker:docker composer.json patche[s] /tmp/magento/patches/
RUN rm -f /tmp/magento/patches/composer.json
WORKDIR /tmp/magento
# The extensions that composer.json and composer.lock require: on in each PHP step
# (php-build-env.sh, with the PHP_* build variables) and in the image (php-entrypoint.sh).
RUN php /usr/local/lib/deployyy/php-extensions.php /tmp/php-extensions
# composer's credentials (auth.json content) arrive as the BuildKit secret composer-auth,
# exported as COMPOSER_AUTH for this step only: they are in no layer and no build cache,
# and a changed token (a hosted build gets a new GitHub token per build) does not change
# this step's cache key.
RUN --mount=type=secret,id=build-env,required=false,mode=0444 \
    --mount=type=secret,id=composer-auth,required=false,mode=0444 \
    . /usr/local/lib/deployyy/php-build-env.sh; \
    if [ -s /run/secrets/composer-auth ]; then export COMPOSER_AUTH="$(cat /run/secrets/composer-auth)"; fi; \
    composer install --no-interaction --no-dev --prefer-dist --optimize-autoloader
COPY --chown=docker:docker . /tmp/magento/
# Fail the build for a patch that never reached the code. vaimo/composer-patches can skip
# a patch WITHOUT failing — a header naming no installed package, or the folder scan's
# implicit `@version >=0.0.0` against a branch install (dev-main) — and the build then
# says "Nothing to patch", stays green and ships the code unpatched (deployyy-operator
# #276). This checks the installed code itself: every *.patch in patches/ must reverse
# cleanly in its target package, or the build stops naming the file.
RUN --mount=type=secret,id=build-env,required=false,mode=0444 \
    . /usr/local/lib/deployyy/php-build-env.sh; \
    php .platform/check-patches.php
# Ensure app/etc/config.php defines default store scopes — static-content:deploy
# needs a default website at build time (no DB). Injected only if absent.
RUN --mount=type=secret,id=build-env,required=false,mode=0444 \
    . /usr/local/lib/deployyy/php-build-env.sh; \
    php -r '$f="app/etc/config.php"; $c=file_exists($f)?(include $f):[]; if(empty($c["scopes"]["websites"])){ $c["scopes"]=["websites"=>["base"=>["website_id"=>"1","code"=>"base","name"=>"Main Website","sort_order"=>"0","default_group_id"=>"1","is_default"=>"1"]],"groups"=>["1"=>["group_id"=>"1","website_id"=>"1","name"=>"Main Website Store","root_category_id"=>"2","default_store_id"=>"1"]],"stores"=>["default"=>["store_id"=>"1","code"=>"default","website_id"=>"1","group_id"=>"1","name"=>"Default Store View","sort_order"=>"0","is_active"=>"1"]]]; file_put_contents($f,"<?php\nreturn ".var_export($c,true).";\n"); fwrite(STDERR,"default scopes injected\n"); }'
# Bake the authoritative list of cache types this codebase declares.
#
# A cache type that is ABSENT from env.php's `cache_types` is DISABLED — Magento's
# Framework\App\Cache\State::isEnabled() returns false for a missing key, silently and
# with no warning anywhere. The list is NOT a function of the Magento version alone: it
# varies by version AND edition AND installed third-party modules. Measured on real
# images: CE 2.4.6 = 14 types, CE 2.4.7 = 15 (adds graphql_query_resolver_result),
# Adobe Commerce 2.4.7 = 18 (adds target_rule, admin_ui_sdk, webhooks_response), and
# CE 2.4.9 = 16 because a third-party module (Algolia) declares algolia_indexer.
# That last one is why this is generated from the codebase instead of hardcoded per
# version: no version/edition table can predict a third-party module's cache type.
#
# env.php reads the generated app/etc/cache_types.php. Regenerate/inspect by hand with:
#   find vendor app -path '*/etc/cache.xml' | xargs grep -ho '<type name="[a-z_]*"' \
#     | sed 's/.*name="//;s/"//' | sort -u
RUN set -eu; \
    find vendor app -path '*/etc/cache.xml' -print0 2>/dev/null \
      | xargs -0 -r grep -ho '<type name="[a-z_]*"' \
      | sed 's/.*name="//;s/"//' | sort -u > /tmp/cache_types.txt; \
    test -s /tmp/cache_types.txt || { echo "FATAL: no etc/cache.xml declares a cache type. Check the composer install output above."; exit 1; }; \
    { echo "<?php"; \
      echo "// GENERATED AT BUILD TIME by the platform Dockerfile — do not edit."; \
      echo "// Every cache type this codebase declares; a type missing here is DISABLED."; \
      echo "return ["; \
      sed 's/^/    "/; s/$/",/' /tmp/cache_types.txt; \
      echo "];"; } > app/etc/cache_types.php; \
    echo "Cache types: $(wc -l < /tmp/cache_types.txt), $(tr '\n' ' ' < /tmp/cache_types.txt)"
# Magento's configuration check off: the module Deployyy_ConfigCheckOff disables the
# plugin configHash, so web pods of the previous configuration keep serving after
# app:config:import (README, "Configuration check"). The runtime stage verifies the
# compiled plugin lists.
RUN --mount=type=secret,id=build-env,required=false,mode=0444 \
    . /usr/local/lib/deployyy/php-build-env.sh; \
    php .platform/config-check-off.php install
# Static content must be deployed for EVERY locale the store views use. The build is
# DB-less, so Magento cannot discover the locales itself (they live in core_config_data
# as general/locale/code per store) — they must be listed explicitly (scaffold.py's
# --locales / PLATFORM_LOCALES) and KEPT IN SYNC WITH THE STORE VIEWS.
#
# Deploying too few does NOT fail the build and does NOT fail a page render: the other
# store views still return HTTP 200, but every CSS/JS asset they reference 404s, because
# in production mode Magento never generates static content on demand. `ls` lies too —
# the missing locale dirs exist but hold ~1 file. Verify per-locale assets over HTTP.
ARG PLATFORM_LOCALES="en_US"
RUN --mount=type=secret,id=build-env,required=false,mode=0444 \
    . /usr/local/lib/deployyy/php-build-env.sh; \
    php bin/magento setup:di:compile \
    && php bin/magento setup:static-content:deploy -f -j"$(nproc)" ${PLATFORM_LOCALES} \
    && rm -rf var/cache var/page_cache var/generation
# WORKAROUND — guarantee the adminhtml JS translation dictionary exists.
#
# This is a deliberate, known-lossy workaround, not a stray `touch`. Magento's admin
# requests js-translation.json via requirejs ('text!js-translation.json'), but
# static-content:deploy never writes it for the adminhtml area in this DB-LESS build, so
# the admin 404s on it. The generator (Magento\Deploy\Service\DeployTranslationsDictionary
# ::deploy()) wraps its work in try/catch and only logs, so the build stays green while
# silently shipping nothing. Frontend themes are unaffected — they get theirs.
#
# It is DB-dependent, verified in-cluster rather than assumed: running the SAME image's
# `static-content:deploy -a adminhtml en_US` with a database attached DID produce the
# file. Json\PreProcessor::process() calls translate->loadData(), which reads the
# `translation` table.
#
# LIMITATION: an empty dictionary is byte-equivalent to a DB-backed deploy ONLY for an
# en_US admin with no admin JS translations. For a NON-ENGLISH admin locale this DROPS
# real admin JS translations. The proper fix is a DB-backed static-content:deploy, which
# the platform's DB-less build model does not currently allow.
RUN set -eu; for d in pub/static/adminhtml/*/*/*/; do \
      [ -d "$d" ] || continue; \
      [ -e "$d/js-translation.json" ] || { printf '{}' > "$d/js-translation.json"; echo "seeded empty js-translation.json in $d"; }; \
    done
# Sample data's own stylesheet, baked into pub/media like the rest of its media.
#
# Magento_ThemeSampleData writes pub/media/styles.css DURING setup:install (HeadStyle::add,
# its comment header stripped) and links every page to it through design/head/includes
# ({{MEDIA_URL}}styles.css) — the Luma homepage and the sample content blocks are styled
# by nothing else. A store whose media is a bucket only has the file if that one install
# put it there: a store copied from another (a project born from a template) inherits the
# link in its database and gets the file only from the source's bucket, and the Mage-OS
# template's bucket did not carry it (2026-10-03: /media/styles.css 404 on the template
# and on every project copied from it, the homepage half unstyled). Baked, it reaches the
# bucket through the same remote-storage:sync that carries the sample-data media. Written
# only when absent. (A fresh setup:install appends its own copy to this one: the same few
# contact-page rules twice, which is harmless.)
RUN --mount=type=secret,id=build-env,required=false,mode=0444 \
    . /usr/local/lib/deployyy/php-build-env.sh; \
    set -eu; for f in vendor/*/module-theme-sample-data/fixtures/styles.css; do \
      [ -f "$f" ] || continue; \
      [ -e pub/media/styles.css ] || php -r '$c=preg_replace("/^\/\*[\s\S]+?\*\//","",file_get_contents($argv[1])); if(trim($c)!==""){file_put_contents("pub/media/styles.css",$c); fwrite(STDERR,"sample-data styles.css baked\n");}' "$f"; \
    done
# The injected scopes were only needed for the DB-less static-content:deploy.
# Strip them so the runtime image ships a scope-free config.php: a fresh
# setup:install owns scope creation in the DB, and a baked scope block would
# make app:config:import choke (DataDifferenceCalculator array_combine mismatch).
RUN --mount=type=secret,id=build-env,required=false,mode=0444 \
    . /usr/local/lib/deployyy/php-build-env.sh; \
    php -r '$f="app/etc/config.php"; $c=include $f; unset($c["scopes"]); file_put_contents($f,"<?php\nreturn ".var_export($c,true).";\n"); fwrite(STDERR,"build-only scopes stripped\n");'

# Stage 2: php-fpm runtime
#
# No sudo, as in recipes/magento: the base image's `php` wrapper and entrypoint write
# the PHP config (PHP_EXTENSION_*/PHP_INI_*) through `sudo`, which a container with
# no-new-privileges cannot run. Every process that starts PHP without the entrypoint
# then got a bare PHP: no php.ini, no extensions but opcache — the agent workspace's
# Claude Code container (claude-rc) ran exactly that (2026-10-03, magento2-liquid:
# "a very limited php cli that doesn't have anything loaded"). Here the app user owns
# the few files the config step writes and the sudo-less entrypoint and `php` wrapper
# (.platform/php-entrypoint.sh, .platform/php-wrapper.sh) do the same work, so any
# process — a Job, `kubectl exec`, an agent — gets the configured PHP.
#
# OPcache holds a Magento installation: about 50 000 PHP files with the generated code,
# more than the image's 10 000 keys and 128 MB hold once a shop serves its checkout,
# account and GraphQL pages too, and the image's 8 MB of interned strings is full after
# one listing page. The image never changes after the build, so OPcache does not check
# file times; a dev box sets PHP_INI_OPCACHE__VALIDATE_TIMESTAMPS=1 at runtime.
FROM thecodingmachine/php:${PHP_VERSION}-v5-fpm-node22 AS php-fpm
ARG PHP_VERSION
# The PHP version of the role `app`, read by the operator (docs/MANUAL-BUILDS.md).
LABEL app.deployyy.php="${PHP_VERSION}"
ENV PHP_EXTENSION_GD=1 PHP_EXTENSION_INTL=1 PHP_EXTENSION_OPCACHE=1 \
    PHP_EXTENSION_PDO_MYSQL=1 PHP_EXTENSION_BCMATH=1 PHP_EXTENSION_SOAP=1 \
    PHP_EXTENSION_SOCKETS=1 PHP_EXTENSION_SODIUM=1 PHP_EXTENSION_XSL=1 \
    PHP_EXTENSION_ZIP=1 PHP_EXTENSION_PCNTL=1 PHP_INI_MEMORY_LIMIT=2G \
    PHP_INI_OPCACHE__ENABLE=1 PHP_INI_DATE__TIMEZONE=Europe/Amsterdam \
    PHP_INI_OPCACHE__MEMORY_CONSUMPTION=512 PHP_INI_OPCACHE__INTERNED_STRINGS_BUFFER=64 \
    PHP_INI_OPCACHE__MAX_ACCELERATED_FILES=130000 PHP_INI_OPCACHE__VALIDATE_TIMESTAMPS=0 \
    TEMPLATE_PHP_INI=production
COPY --from=builder --chown=docker:docker /tmp/magento /var/www/html
WORKDIR /var/www/html
COPY --chown=docker:docker .platform/env.php /var/www/html/app/etc/env.php
# This env.php sets the host-derived values as CONFIG__ variables, outside the
# `system` section Magento hashes. The operator reads the label from the image
# and leaves those values out of its import hash.
LABEL app.deployyy.config-env="1"
# Magento's configuration check (plugin configHash) is disabled; the build fails
# otherwise (below). The operator then imports a configuration-only release while
# the previous pods serve.
LABEL app.deployyy.config-check="off"
USER root
# The base image's ~/.bash_profile runs symfony-autocomplete, which prints a PHP 8.4
# deprecation at each terminal login and keeps bash from reading ~/.profile and ~/.bashrc.
RUN rm -f /home/docker/.bash_profile
RUN apt-get update && apt-get install -y --no-install-recommends default-mysql-client \
    && rm -rf /var/lib/apt/lists/*

# PHP mail() pipes to sendmail-tagged: the setting `sendmail_path` in
# zz-mailpit-sendmail.ini of fpm and cli. sendmail-tagged relays over SMTP to
# MP_SENDMAIL_SMTP_ADDR, the mail service that the platform sets below production.
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

# Unprivileged PHP: the app user owns the files the config step writes (the PHP config,
# the php.ini links of TEMPLATE_PHP_INI, the locales of LOCALES), and sudo is gone.
COPY --chmod=0755 .platform/php-entrypoint.sh /usr/local/bin/deployyy-php-entrypoint.sh
COPY --chmod=0755 .platform/php-wrapper.sh /usr/bin/php
COPY --from=builder /tmp/php-extensions /usr/local/lib/deployyy/php-extensions
RUN set -eux; \
    ln -sf "/usr/lib/php/${PHP_VERSION}/php.ini-${TEMPLATE_PHP_INI}.cli" "/etc/php/${PHP_VERSION}/cli/php.ini"; \
    ln -sf "/usr/lib/php/${PHP_VERSION}/php.ini-${TEMPLATE_PHP_INI}" "/etc/php/${PHP_VERSION}/fpm/php.ini"; \
    test -e "/etc/php/${PHP_VERSION}/cli/php.ini" && test -e "/etc/php/${PHP_VERSION}/fpm/php.ini"; \
    touch "/etc/php/${PHP_VERSION}/mods-available/generated_conf.ini"; \
    chown -R docker:docker "/etc/php/${PHP_VERSION}/cli/conf.d" "/etc/php/${PHP_VERSION}/fpm/conf.d" \
        /var/lib/php/modules "/etc/php/${PHP_VERSION}/mods-available/generated_conf.ini" \
        /opt/php_env_fingerprint /usr/lib/locale; \
    chown docker:docker "/etc/php/${PHP_VERSION}/cli" "/etc/php/${PHP_VERSION}/fpm"; \
    gpasswd -d docker sudo || true; \
    rm -f /etc/sudoers.d/*; \
    su docker -s /bin/bash -c /usr/local/bin/deployyy-php-entrypoint.sh
# Magento rewrites app/etc/env.php as static values whenever it saves deployment
# config (DeploymentConfig\Writer), which drops env.php's $_ENV assignments of the
# CONFIG__ values. A read-only copy runs before every PHP script, in its own scope, so
# every request and command gets them.
# Plain COPY and chmod in a RUN: COPY --chmod also sets the mode of the directory it
# creates, and a directory without x stops every PHP process (exit 255).
COPY .platform/env.php /usr/local/lib/deployyy/env.php
RUN printf '%s\n' '<?php' '(static function (): void { require __DIR__ . "/env.php"; })();' \
      > /usr/local/lib/deployyy/prepend.php \
    && chown -R root:root /usr/local/lib/deployyy \
    && chmod 0755 /usr/local/lib/deployyy \
    && chmod 0644 /usr/local/lib/deployyy/env.php /usr/local/lib/deployyy/prepend.php
ENV PHP_INI_AUTO_PREPEND_FILE=/usr/local/lib/deployyy/prepend.php
USER 1000:1000
# A script as the runtime user (docker, uid 1000) must run the prepend file, or the
# build fails. `php -r` skips auto_prepend_file, so the check is a script. The plugin
# configHash must be off in every compiled area, or the build fails.
RUN printf '%s\n' '<?php' \
      'if (!isset($_ENV["CONFIG__DEFAULT__CATALOG__SEARCH__OPENSEARCH_SERVER_HOSTNAME"])) {' \
      '    fwrite(STDERR, "auto_prepend_file did not run as " . get_current_user() . "\n");' \
      '    exit(1);' \
      '}' \
      'echo "auto_prepend_file ran\n";' > /tmp/prepend-check.php \
    && php /tmp/prepend-check.php && rm /tmp/prepend-check.php \
    && php .platform/config-check-off.php verify
ENTRYPOINT ["/usr/local/bin/deployyy-php-entrypoint.sh"]
CMD ["php-fpm"]

# Stage 3: nginx (serves pub/, proxies PHP to FPM on :9000)
FROM nginx:1.26-alpine AS nginx
# OS security fixes the base image does not have yet: the build's image scan
# fails on a fixable CRITICAL finding (.github/actions/build-scan).
# PLATFORM_OS_UPDATES_DAY (the build date) re-runs this once a day even when
# the layer is cached. nginx itself comes from nginx.org and is not upgraded.
ARG PLATFORM_OS_UPDATES_DAY=""
RUN apk upgrade --no-cache
COPY .platform/nginx-default.conf /etc/nginx/conf.d/default.conf
# The project's nginx files (.platform/nginx/*.conf), checked with the server block.
RUN --mount=type=bind,source=.platform,target=/tmp/platform \
    mkdir -p /etc/nginx/project.d \
    && if ls /tmp/platform/nginx/*.conf >/dev/null 2>&1; then cp /tmp/platform/nginx/*.conf /etc/nginx/project.d/; fi \
    && nginx -t
COPY --from=builder /tmp/magento/pub /var/www/html/pub
