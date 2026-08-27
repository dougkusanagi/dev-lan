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

packages_installed() {
    local package
    for package in "$@"; do
        if [[ "$(dpkg-query -W -f='${db:Status-Abbrev}' "$package" 2>/dev/null || true)" != "ii " ]]; then
            return 1
        fi
    done
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

ensure_wsl_systemd() {
    local config='/etc/wsl.conf'
    local temporary
    temporary="$(mktemp)"
    if [[ -f "$config" ]]; then
        awk '
            function add_systemd() {
                if (in_boot && !systemd_seen) {
                    print "systemd=true"
                    systemd_seen=1
                }
            }
            /^[[:space:]]*\[/ {
                add_systemd()
                section=tolower($0)
                in_boot=(section ~ /^\[[[:space:]]*boot[[:space:]]*\][[:space:]]*$/)
                if (in_boot) boot_seen=1
            }
            in_boot && $0 ~ /^[[:space:]]*systemd[[:space:]]*=/ {
                if (!systemd_seen) {
                    print "systemd=true"
                    systemd_seen=1
                }
                next
            }
            { print }
            END {
                add_systemd()
                if (!boot_seen) {
                    print ""
                    print "[boot]"
                    print "systemd=true"
                }
            }
        ' "$config" > "$temporary"
    else
        printf '[boot]\nsystemd=true\n' > "$temporary"
    fi
    "${SUDO[@]}" install -m 0644 "$temporary" "$config"
    rm -f "$temporary"
}

php_prefix="php${php_minor}"
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
required_packages=("${php_packages[@]}" composer acl)
if [[ "$install_caddy" == "1" ]]; then
    required_packages+=(caddy)
fi
provision_candidates=("${required_packages[@]}" php-fpm php-cli php-mbstring php-xml php-curl php-zip php-mysql php-pgsql php-bcmath php-intl php-gd)

# Keep a narrow, append-only ownership ledger for packages that this bootstrap
# newly installs. It is consumed by `devlan uninstall`; packages that were
# already present are deliberately never attributed to DevLAN.
provenance_dir='/etc/devlan'
provenance_file="$provenance_dir/bootstrap-packages"
provenance_files="$provenance_dir/bootstrap-files"
before_packages="$(mktemp)"
before_sources="$(mktemp)"
trap 'rm -f "$before_packages" "$before_sources"' EXIT
"${SUDO[@]}" install -d -m 0755 "$provenance_dir"
for package in "${provision_candidates[@]}" ca-certificates curl gnupg debian-keyring debian-archive-keyring apt-transport-https lsb-release software-properties-common unzip git acl; do
    if [[ "$(dpkg-query -W -f='${db:Status-Abbrev}' "$package" 2>/dev/null || true)" == "ii " ]]; then
        printf '%s\n' "$package" >> "$before_packages"
    fi
done
find /etc/apt/sources.list.d /etc/apt/trusted.gpg.d -maxdepth 1 -type f -print 2>/dev/null | sort > "$before_sources" || true

# Preserve the exact pre-bootstrap systemd setting for a later three-state
# restore. The marker means that a missing file must be removed on uninstall.
if [[ -f /etc/wsl.conf ]]; then
    if [[ ! -f "$provenance_dir/wsl.conf.before" ]]; then
        "${SUDO[@]}" cp -- /etc/wsl.conf "$provenance_dir/wsl.conf.before"
    fi
elif [[ ! -e "$provenance_dir/wsl.conf.missing" ]]; then
    "${SUDO[@]}" touch "$provenance_dir/wsl.conf.missing"
fi

caddy_was_available=0
if [[ "$install_caddy" == "1" ]] && ! command -v caddy >/dev/null 2>&1; then
    caddy_was_available=1
fi
if [[ "$install_caddy" == "1" ]]; then
    # Record whether the service unit existed before bootstrap. A pre-existing
    # Caddy must not be disabled merely because DevLAN used it as its edge.
    if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files caddy.service 2>/dev/null | grep -Eq '^caddy\.service[[:space:]]'; then
        if [[ ! -e "$provenance_dir/caddy-service.before" && ! -e "$provenance_dir/caddy-service.missing" ]]; then
            "${SUDO[@]}" touch "$provenance_dir/caddy-service.before"
        fi
    elif [[ ! -e "$provenance_dir/caddy-service.before" && ! -e "$provenance_dir/caddy-service.missing" ]]; then
        "${SUDO[@]}" touch "$provenance_dir/caddy-service.missing"
    fi
    if [[ -d /var/lib/caddy ]]; then
        if [[ ! -e "$provenance_dir/caddy-data.before" && ! -e "$provenance_dir/caddy-data.missing" ]]; then
            "${SUDO[@]}" touch "$provenance_dir/caddy-data.before"
        fi
    elif [[ ! -e "$provenance_dir/caddy-data.before" && ! -e "$provenance_dir/caddy-data.missing" ]]; then
        "${SUDO[@]}" touch "$provenance_dir/caddy-data.missing"
    fi
fi

if packages_installed "${required_packages[@]}"; then
    printf 'WSL packages already installed; skipping apt update/install\n'
else
    export DEBIAN_FRONTEND=noninteractive
    "${SUDO[@]}" apt-get update
    apt_install ca-certificates curl gnupg debian-keyring debian-archive-keyring apt-transport-https lsb-release software-properties-common unzip git acl

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
        apt_install "${php_packages[@]}" composer
    else
        # Distribution packages are the safe fallback when the requested branch
        # is unavailable. doctor reports the exact branch afterward.
        php_packages=(php-fpm php-cli php-mbstring php-xml php-curl php-zip php-mysql php-pgsql php-bcmath php-intl php-gd)
        apt_install "${php_packages[@]}" composer
    fi
fi

fpm_service="${php_prefix}-fpm"
if ! systemctl list-unit-files "$fpm_service.service" >/dev/null 2>&1 && [[ ! -e "/etc/init.d/$fpm_service" ]]; then
    fpm_service="$(find /etc/init.d -maxdepth 1 -type f -name 'php*-fpm' -printf '%f\n' 2>/dev/null | sort | head -n1 || true)"
    if [[ -z "$fpm_service" ]]; then
        fpm_service="php-fpm"
    fi
fi

# Use Caddy's official Debian/Ubuntu repository. The package installs the
# service files; install.ps1 supplies the DevLAN Caddyfile afterward.
if [[ "$install_caddy" == "1" ]] && ! command -v caddy >/dev/null 2>&1; then
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | "${SUDO[@]}" gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | "${SUDO[@]}" tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
    "${SUDO[@]}" chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
    "${SUDO[@]}" apt-get update
    apt_install caddy
fi

if [[ "$caddy_was_available" == "1" ]]; then
    for file in /etc/apt/sources.list.d/caddy-stable.list /usr/share/keyrings/caddy-stable-archive-keyring.gpg; do
        if [[ -e "$file" ]] && ! grep -Fxq "$file" "$provenance_files" 2>/dev/null; then
            printf '%s\n' "$file" | "${SUDO[@]}" tee -a "$provenance_files" >/dev/null
        fi
    done
fi

# systemd is required by the single WSL Caddy topology. The edit is
# idempotent and preserves unrelated sections/comments in /etc/wsl.conf;
# WSL applies it after the next explicit `wsl --shutdown`.
ensure_wsl_systemd

pool_file="$(find /etc/php -type f -path '*/fpm/pool.d/www.conf' | sort | head -n1 || true)"
if [[ -n "$pool_file" ]]; then
    # Save the exact pool path and bytes before changing the distribution's
    # pre-existing www pool. The uninstall planner restores it through these
    # fixed markers rather than deleting an entire PHP configuration tree.
    if [[ ! -e "$provenance_dir/php-pool.path" ]]; then
        printf '%s\n' "$pool_file" | "${SUDO[@]}" tee "$provenance_dir/php-pool.path" >/dev/null
    fi
    if [[ ! -f "$provenance_dir/php-pool.before" ]]; then
        "${SUDO[@]}" cp -- "$pool_file" "$provenance_dir/php-pool.before"
    fi
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
    restart_service caddy || true
    printf 'WSL dependencies ready: PHP %s, PHP-FPM socket /run/php/php-fpm.sock, Composer and Caddy\n' "$php_minor"
else
    printf 'WSL dependencies ready: PHP %s, PHP-FPM socket /run/php/php-fpm.sock and Composer\n' "$php_minor"
fi

for package in "${provision_candidates[@]}" ca-certificates curl gnupg debian-keyring debian-archive-keyring apt-transport-https lsb-release software-properties-common unzip git acl; do
    if [[ "$(dpkg-query -W -f='${db:Status-Abbrev}' "$package" 2>/dev/null || true)" == "ii " ]] && ! grep -Fxq "$package" "$before_packages"; then
        if ! grep -Fxq "$package" "$provenance_file" 2>/dev/null; then
            printf '%s\n' "$package" | "${SUDO[@]}" tee -a "$provenance_file" >/dev/null
        fi
    fi
done

find /etc/apt/sources.list.d /etc/apt/trusted.gpg.d -maxdepth 1 -type f -print 2>/dev/null | sort | while IFS= read -r file; do
    if [[ -n "$file" ]] && ! grep -Fxq "$file" "$before_sources" && ! grep -Fxq "$file" "$provenance_files" 2>/dev/null; then
        printf '%s\n' "$file" | "${SUDO[@]}" tee -a "$provenance_files" >/dev/null
    fi
done
