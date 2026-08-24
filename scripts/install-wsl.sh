#!/usr/bin/env bash
set -Eeuo pipefail

# DevLAN WSL bootstrap. It is called by install.ps1 with one argument such as
# 8.5. All commands and package names below are fixed; the version is checked
# before it is used.
php_minor="${1:-8.5}"
install_caddy="${2:-1}"
if [[ ! "$php_minor" =~ ^8\.(3|4|5)$ ]]; then
    printf 'Unsupported PHP branch: %s (expected 8.3, 8.4 or 8.5)\n' "$php_minor" >&2
    exit 2
fi

if [[ "$EUID" -eq 0 ]]; then
    SUDO=()
else
    SUDO=(sudo)
fi

apt_install() {
    "${SUDO[@]}" apt-get install -y "$@"
}

has_package() {
    apt-cache show "$1" >/dev/null 2>&1
}

restart_service() {
    local service_name="$1"
    if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files "$service_name.service" >/dev/null 2>&1; then
        if "${SUDO[@]}" systemctl enable --now "$service_name"; then
            return 0
        fi
    fi
    if command -v service >/dev/null 2>&1; then
        "${SUDO[@]}" service "$service_name" restart
    fi
}

export DEBIAN_FRONTEND=noninteractive
"${SUDO[@]}" apt-get update
apt_install ca-certificates curl gnupg debian-keyring debian-archive-keyring apt-transport-https lsb-release software-properties-common unzip git

php_prefix="php${php_minor}"
if ! has_package "${php_prefix}-fpm"; then
    # Ubuntu's official repository may lag behind the requested active branch.
    # The maintained Ondřej Surý PPA is used only on Ubuntu in that case.
    . /etc/os-release
    if [[ "${ID:-}" == "ubuntu" ]] && command -v add-apt-repository >/dev/null 2>&1; then
        "${SUDO[@]}" add-apt-repository -y ppa:ondrej/php
        "${SUDO[@]}" apt-get update
    fi
fi

if has_package "${php_prefix}-fpm"; then
    php_packages=(
        "${php_prefix}-fpm"
        "${php_prefix}-cli"
        "${php_prefix}-mbstring"
        "${php_prefix}-xml"
        "${php_prefix}-curl"
        "${php_prefix}-zip"
        "${php_prefix}-mysql"
        "${php_prefix}-pgsql"
        "${php_prefix}-bcmath"
        "${php_prefix}-intl"
        "${php_prefix}-gd"
    )
    fpm_service="${php_prefix}-fpm"
else
    # Distribution packages are the safe fallback when the requested branch
    # is unavailable. doctor reports the exact branch afterward.
    php_packages=(php-fpm php-cli php-mbstring php-xml php-curl php-zip php-mysql php-pgsql php-bcmath php-intl php-gd)
    fpm_service="$(find /etc/init.d -maxdepth 1 -type f -name 'php*-fpm' -printf '%f\n' 2>/dev/null | sort | head -n1 || true)"
    if [[ -z "$fpm_service" ]]; then
        fpm_service="php-fpm"
    fi
fi

apt_install "${php_packages[@]}" composer

# Use Caddy's official Debian/Ubuntu repository. The package installs the
# service files; install.ps1 supplies the DevLAN Caddyfile afterward.
if [[ "$install_caddy" == "1" ]] && ! command -v caddy >/dev/null 2>&1; then
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | "${SUDO[@]}" gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | "${SUDO[@]}" tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
    "${SUDO[@]}" chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
    "${SUDO[@]}" apt-get update
    apt_install caddy
fi

pool_file="$(find /etc/php -type f -path '*/fpm/pool.d/www.conf' | sort | head -n1 || true)"
if [[ -n "$pool_file" ]]; then
    "${SUDO[@]}" sed -Ei 's/^[;[:space:]]*pm[[:space:]]*=[[:space:]]*.*/pm = ondemand/' "$pool_file"
    "${SUDO[@]}" sed -Ei 's/^[;[:space:]]*pm\.max_children[[:space:]]*=[[:space:]]*.*/pm.max_children = 10/' "$pool_file"
    "${SUDO[@]}" sed -Ei 's/^[;[:space:]]*pm\.max_requests[[:space:]]*=[[:space:]]*.*/pm.max_requests = 500/' "$pool_file"
    if ! grep -Eq '^[[:space:]]*pm\.process_idle_timeout[[:space:]]*=' "$pool_file"; then
        printf '\npm.process_idle_timeout = 10s\n' | "${SUDO[@]}" tee -a "$pool_file" >/dev/null
    else
        "${SUDO[@]}" sed -Ei 's/^[;[:space:]]*pm\.process_idle_timeout[[:space:]]*=[[:space:]]*.*/pm.process_idle_timeout = 10s/' "$pool_file"
    fi
fi

restart_service "$fpm_service"

socket="$(find /run/php -maxdepth 1 -type s -name 'php*-fpm.sock' | sort | head -n1 || true)"
if [[ -n "$socket" ]]; then
    "${SUDO[@]}" ln -sfn "$socket" /run/php/php-fpm.sock
fi

if [[ "$install_caddy" == "1" ]]; then
    printf 'WSL dependencies ready: PHP %s, PHP-FPM socket /run/php/php-fpm.sock, Composer and Caddy\n' "$php_minor"
else
    printf 'WSL dependencies ready: PHP %s, PHP-FPM socket /run/php/php-fpm.sock and Composer\n' "$php_minor"
fi
