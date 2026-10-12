#!/usr/bin/env bash
# Первичная настройка сервера (Ubuntu 24.04), от root. Идемпотентно: swap 2 ГБ, ufw 22/80/443, /opt/swapowl.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "запускайте от root" >&2; exit 1; }
DEPLOY_DIR="${DEPLOY_DIR:-/opt/swapowl}"

if ! swapon --show --noheadings | grep -q .; then
  if [ ! -f /swapfile ]; then
    fallocate -l 2G /swapfile || dd if=/dev/zero of=/swapfile bs=1M count=2048
    chmod 600 /swapfile
    mkswap /swapfile
  fi
  swapon /swapfile
fi
grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab

if command -v ufw >/dev/null; then
  ufw allow 22/tcp
  ufw allow 80/tcp
  ufw allow 443/tcp
  ufw --force enable
fi

mkdir -p "$DEPLOY_DIR/deploy"
chmod 755 "$DEPLOY_DIR"
echo "готово: swap и ufw настроены, каталог $DEPLOY_DIR создан"
