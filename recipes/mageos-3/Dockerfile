# PLATFORM BUILD RECIPE — line mageos-320 (Mage OS 3.2.x = Magento core
# 2.4.9, PHP 8.4). Owned by the platform (deployyy-operator repo,
# platform/build/); consumed by the reusable magento2-build workflow, which
# overlays it into a project checkout UNLESS the project ships its own
# Dockerfile (escape hatch — a repo-local Dockerfile wins wholesale, and
# repo-local .platform/* files win per file). Improvements here reach every
# consuming repo on their next build — the injected-copy era ("elke wees z'n
# eigen Dockerfile") is over for repos on the caller flow.
#
# Per-project knobs arrive as build-args (the workflow resolves them):
#   PLATFORM_LOCALES — space-separated store-view locales for the DB-less
#   static-content:deploy (from deployyy.json .build.locales; en_US default).
# Stage 1: dependencies + compile + static content
FROM thecodingmachine/php:8.4-v5-fpm-node22 AS builder

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
# every patch skipped, build green, unpatched image shipped (seen on the StyleSmuggler
# 0-day, Sansec 2026-09-05, where the guard lives in exactly such a patch). The
# character-class glob keeps the directory OPTIONAL (COPY fails on a plain missing source;
# with composer.json as a guaranteed match it does not), so projects without patches build
# unchanged; the stray composer.json copy is removed again. Layer caching is preserved:
# only composer.json/lock and patches/ invalidate the install layer, not the whole tree.
COPY --chown=docker:docker composer.json patche[s] /tmp/magento/patches/
RUN rm -f /tmp/magento/patches/composer.json
WORKDIR /tmp/magento
RUN composer install --no-interaction --no-dev --prefer-dist --optimize-autoloader \
    && rm -f /home/docker/.composer/auth.json
COPY --chown=docker:docker . /tmp/magento/
# NEVER let Magento marketplace credentials survive into the published image.
# `COPY . /tmp/magento/` above pulls in the workflow-generated auth.json, and the
# php-fpm stage ships this whole directory — so without this the image published to
# GHCR would contain the credentials. It CANNOT simply be .dockerignore'd: the
# builder needs it for its own COPY (composer install), so it is deleted here instead.
RUN rm -f /tmp/magento/auth.json
# Ensure app/etc/config.php defines default store scopes — static-content:deploy
# needs a default website at build time (no DB). Injected only if absent.
RUN php -r '$f="app/etc/config.php"; $c=file_exists($f)?(include $f):[]; if(empty($c["scopes"]["websites"])){ $c["scopes"]=["websites"=>["base"=>["website_id"=>"1","code"=>"base","name"=>"Main Website","sort_order"=>"0","default_group_id"=>"1","is_default"=>"1"]],"groups"=>["1"=>["group_id"=>"1","website_id"=>"1","name"=>"Main Website Store","root_category_id"=>"2","default_store_id"=>"1"]],"stores"=>["default"=>["store_id"=>"1","code"=>"default","website_id"=>"1","group_id"=>"1","name"=>"Default Store View","sort_order"=>"0","is_active"=>"1"]]]; file_put_contents($f,"<?php\nreturn ".var_export($c,true).";\n"); fwrite(STDERR,"default scopes injected\n"); }'
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
    test -s /tmp/cache_types.txt || { echo "FATAL: no cache.xml <type> entries found"; exit 1; }; \
    { echo "<?php"; \
      echo "// GENERATED AT BUILD TIME by the platform Dockerfile — do not edit."; \
      echo "// Every cache type this codebase declares; a type missing here is DISABLED."; \
      echo "return ["; \
      sed 's/^/    "/; s/$/",/' /tmp/cache_types.txt; \
      echo "];"; } > app/etc/cache_types.php; \
    echo "cache types baked: $(wc -l < /tmp/cache_types.txt) -> $(tr '\n' ' ' < /tmp/cache_types.txt)"
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
RUN php bin/magento setup:di:compile \
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
# The injected scopes were only needed for the DB-less static-content:deploy.
# Strip them so the runtime image ships a scope-free config.php: a fresh
# setup:install owns scope creation in the DB, and a baked scope block would
# make app:config:import choke (DataDifferenceCalculator array_combine mismatch).
RUN php -r '$f="app/etc/config.php"; $c=include $f; unset($c["scopes"]); file_put_contents($f,"<?php\nreturn ".var_export($c,true).";\n"); fwrite(STDERR,"build-only scopes stripped\n");'

# Stage 2: php-fpm runtime
FROM thecodingmachine/php:8.4-v5-fpm-node22 AS php-fpm
ENV PHP_EXTENSION_GD=1 PHP_EXTENSION_INTL=1 PHP_EXTENSION_OPCACHE=1 \
    PHP_EXTENSION_PDO_MYSQL=1 PHP_EXTENSION_BCMATH=1 PHP_EXTENSION_SOAP=1 \
    PHP_EXTENSION_SOCKETS=1 PHP_EXTENSION_SODIUM=1 PHP_EXTENSION_XSL=1 \
    PHP_EXTENSION_ZIP=1 PHP_EXTENSION_PCNTL=1 PHP_INI_MEMORY_LIMIT=2G \
    PHP_INI_OPCACHE__ENABLE=1 PHP_INI_DATE__TIMEZONE=Europe/Amsterdam
COPY --from=builder --chown=docker:docker /tmp/magento /var/www/html
WORKDIR /var/www/html
COPY --chown=docker:docker .platform/env.php /var/www/html/app/etc/env.php
USER root
RUN apt-get update && apt-get install -y --no-install-recommends default-mysql-client \
    && rm -rf /var/lib/apt/lists/*

# Mailpit sendmail drop-in. PHP's mail() pipes to /usr/sbin/sendmail, which does not
# exist in this image — so Magento mail failed SILENTLY (no error, no message, nothing
# in Mailpit; the admin forgot-password POST still returned its normal 302). Point
# sendmail_path at the sendmail-tagged shim, which relays over SMTP to
# MP_SENDMAIL_SMTP_ADDR (set by the platform's magento-env ConfigMap) via the mailpit
# binary — and, when MAIL_TAG is set (ephemeral/preview envs), prepends an X-Tags header
# so the customer family's shared Mailpit tags each message with its env (see the shim's
# header).
#
# The ini paths are PHP-VERSION-SPECIFIC (/etc/php/<version>/{fpm,cli}/conf.d/), so they
# are templated from the same 8.4 the base images use — hardcoding one
# version onto another image silently re-breaks mail (the drop-in lands in a directory
# no PHP reads). Both fpm AND cli: cron/consumers send mail from the CLI SAPI.
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
        | tee /etc/php/8.4/fpm/conf.d/zz-mailpit-sendmail.ini \
              /etc/php/8.4/cli/conf.d/zz-mailpit-sendmail.ini > /dev/null
USER docker

# Stage 3: nginx (serves pub/, proxies PHP to FPM on :9000)
FROM nginx:1.26-alpine AS nginx
COPY .platform/nginx-default.conf /etc/nginx/conf.d/default.conf
# Empty Host->store map so the `include` in default.conf resolves and nginx
# starts even when nothing is mounted (single-domain env / standalone image).
# The operator overlays this file with the magento-host-store ConfigMap when
# store domains are bound. See nginx-default.conf.
RUN printf '# no store domains bound\n' > /etc/nginx/host-store.map
COPY --from=builder /tmp/magento/pub /var/www/html/pub
