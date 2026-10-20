# PLATFORM BUILD RECIPE — Laravel (any PHP 8.2–8.4 project). Consumed by the
# reusable laravel-build workflow, which overlays it into a project checkout
# UNLESS the project ships its own Dockerfile (a repo-local Dockerfile wins
# wholesale; repo-local .platform/* files win per file).
#
# The image contract the deployyy LaravelApp platform runs (deployyy-operator
# internal/platform/laravel):
#   - target php-fpm: the application at /var/www/html, php-fpm on :9000,
#     `supercronic` on PATH (thecodingmachine/php ships it) for a declared
#     schedule; everything configured through environment variables
#     (laravel-env + app-vars), never a baked .env;
#   - target nginx: public/ and the server block, :8080, PHP to 127.0.0.1:9000.
#
# Build-args (the workflow resolves them): PHP_VERSION from composer.json's
# `require.php`.
ARG PHP_VERSION=8.4

FROM thecodingmachine/php:${PHP_VERSION}-v5-fpm-node22 AS builder
ENV PHP_EXTENSION_GD=1 PHP_EXTENSION_INTL=1 PHP_EXTENSION_BCMATH=1 \
    PHP_EXTENSION_PDO_MYSQL=1 PHP_EXTENSION_PDO_PGSQL=1 PHP_EXTENSION_REDIS=1 \
    PHP_EXTENSION_ZIP=1 PHP_EXTENSION_PCNTL=1 PHP_EXTENSION_EXIF=1 \
    PHP_INI_MEMORY_LIMIT=2G
USER docker
COPY --chown=docker:docker composer.json composer.lock* /tmp/app/
WORKDIR /tmp/app
# Dependencies first, without the project's scripts (they need the code):
# only composer.json/lock invalidate this layer. composer's credentials
# (auth.json content) arrive as the BuildKit secret composer-auth, exported as
# COMPOSER_AUTH for the step only: in no layer and no build cache, and a
# changed token does not change the cache key.
RUN --mount=type=secret,id=composer-auth,required=false,mode=0444 \
    if [ -s /run/secrets/composer-auth ]; then export COMPOSER_AUTH="$(cat /run/secrets/composer-auth)"; fi; \
    composer install --no-interaction --no-dev --prefer-dist --no-scripts --no-autoloader
COPY --chown=docker:docker . /tmp/app/
RUN --mount=type=secret,id=composer-auth,required=false,mode=0444 \
    if [ -s /run/secrets/composer-auth ]; then export COMPOSER_AUTH="$(cat /run/secrets/composer-auth)"; fi; \
    composer install --no-interaction --no-dev --prefer-dist --optimize-autoloader
# Front-end assets (Vite/Mix) when the project has any; node_modules stays
# behind in this stage. git's configuration from the BuildKit secret build-env
# (GIT_CONFIG_*: on a hosted build, a GitHub token for private git
# dependencies) is exported for this step only. Only those single-line
# assignments are evaluated: the other variables (NODE_ENV) would change what
# npm installs.
RUN --mount=type=secret,id=build-env,required=false,mode=0444 \
    if [ -f package.json ]; then \
      if [ -f /run/secrets/build-env ]; then set -a; \
        eval "$(sed -nE "\#^GIT_CONFIG_[A-Z0-9_]*=('[^']*'|[A-Za-z0-9_@%+=:,./-]*)\$#p" /run/secrets/build-env)"; set +a; fi; \
      if [ -f package-lock.json ]; then npm ci; else npm install; fi \
      && npm run build --if-present \
      && rm -rf node_modules; \
    fi

FROM thecodingmachine/php:${PHP_VERSION}-v5-fpm AS php-fpm
ENV PHP_EXTENSION_GD=1 PHP_EXTENSION_INTL=1 PHP_EXTENSION_BCMATH=1 \
    PHP_EXTENSION_PDO_MYSQL=1 PHP_EXTENSION_PDO_PGSQL=1 PHP_EXTENSION_REDIS=1 \
    PHP_EXTENSION_ZIP=1 PHP_EXTENSION_PCNTL=1 PHP_EXTENSION_EXIF=1 \
    PHP_EXTENSION_OPCACHE=1 PHP_INI_OPCACHE__ENABLE=1 \
    PHP_INI_MEMORY_LIMIT=512M PHP_INI_DATE__TIMEZONE=Europe/Amsterdam
COPY --from=builder --chown=docker:docker /tmp/app /var/www/html
WORKDIR /var/www/html
# Writable runtime directories: the container filesystem is per pod, and
# everything that must outlive one goes to Redis, the database or S3.
RUN mkdir -p storage/framework/cache storage/framework/sessions \
             storage/framework/views storage/logs bootstrap/cache

FROM nginx:1.26-alpine AS nginx
# OS security fixes the base image does not have yet: the build's image scan
# fails on a fixable CRITICAL finding (.github/actions/build-scan).
# PLATFORM_OS_UPDATES_DAY (the build date) re-runs this once a day even when
# the layer is cached. nginx itself comes from nginx.org and is not upgraded.
ARG PLATFORM_OS_UPDATES_DAY=""
RUN apk upgrade --no-cache
COPY .platform/nginx-default.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /tmp/app/public /var/www/html/public
