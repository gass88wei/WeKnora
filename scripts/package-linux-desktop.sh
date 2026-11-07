#!/usr/bin/env bash
set -euo pipefail

# Package the WeKnora Lite desktop build for Linux as a .deb.
#
# Usage: package-linux-desktop.sh <version> <binary-path>
#   version      release tag, with or without the leading "v" (e.g. v0.9.0)
#   binary-path  the Wails build output, normally "cmd/desktop/build/bin/WeKnora Lite"
#
# The package installs the executable next to config/, migrations/sqlite/ and
# licenses/. cmd/desktop walks up from the executable to find
# config/config.yaml and chdir's there, so migrations resolve relative to that
# same directory.
#
# DB_PATH and LOCAL_STORAGE_BASE_DIR point at $HOME, because /opt is never
# writable for the installed user. A wrapper script in /usr/bin exports them:
# godotenv does not expand variables and never overrides a variable that is
# already set, so the values have to come from the environment.

if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <version> <binary-path>" >&2
    exit 1
fi

# Keep the packaged tree world-readable: files copied out of the checkout would
# otherwise inherit the runner's umask and land as 0664 / 0775 inside the
# archive, which dpkg preserves.
umask 022

RAW_VERSION="$1"
BINARY_SRC="$2"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

VERSION="${RAW_VERSION#v}"
PKG_NAME="weknora-lite"
test -f "${BINARY_SRC}" || { echo "desktop binary not found: ${BINARY_SRC}" >&2; exit 1; }

# Guard against the runner's glibc drifting newer than the systems we target.
# github's ubuntu-latest currently builds a binary needing GLIBC_2.43 and
# CXXABI_1.3.15, which fails to start on Debian 12 / Deepin 25 (glibc 2.38).
# Fail here instead of publishing a package that cannot run.
max_glibc="$(
    strings "${BINARY_SRC}" 2>/dev/null |
        grep -oE '^GLIBC_2\.[0-9]+$' |
        sed 's/GLIBC_//' |
        sort -V |
        tail -1 || true
)"
if [ -n "${max_glibc}" ]; then
    echo ">> binary requires glibc ${max_glibc}"
    if [ "$(printf '%s\n%s\n' "${max_glibc}" "2.38" | sort -V | tail -1)" != "2.38" ]; then
        echo "ERROR: binary needs glibc ${max_glibc} but the oldest supported target is 2.38." >&2
        echo "       Build on an older runner (ubuntu-22.04) so the binary stays portable." >&2
        exit 1
    fi
fi

# Actually try to start the binary on the oldest supported userspace. The string
# scan above only proves what symbols are referenced; this proves the dynamic
# loader accepts them. This is a GUI app, so failing to find a display is the
# expected outcome on a headless container -- what must not appear is a
# GLIBC_/CXXABI_ version error.
if command -v docker >/dev/null 2>&1 && [ "${SKIP_SMOKE:-0}" != "1" ]; then
    echo ">> smoke test: launching on Debian 12 (glibc 2.36)"
    smoke_out="$(
        docker run --rm -v "$(cd "$(dirname "${BINARY_SRC}")" && pwd):/w:ro" \
            debian:12 "/w/$(basename "${BINARY_SRC}")" 2>&1 || true
    )"
    if printf '%s' "${smoke_out}" | grep -qE 'GLIBC_[0-9.]+. not found|CXXABI_[0-9.]+. not found'; then
        echo "ERROR: binary cannot start on Debian 12:" >&2
        printf '%s\n' "${smoke_out}" >&2
        exit 1
    fi
    echo ">> smoke test passed (loader accepted the binary)"
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

# ── Shared payload: executable + config + migrations + licenses + .env.lite ──
payload="${WORK_DIR}/payload"
mkdir -p "${payload}"
cp "${BINARY_SRC}" "${payload}/${PKG_NAME}"
chmod 0755 "${payload}/${PKG_NAME}"

cp .env.lite.example "${payload}/.env.lite"
cp docs/LITE.md "${payload}/README.md"
bash ./scripts/copy-licenses.sh "${payload}"
if [ -d config ]; then
    cp -r config "${payload}/config"
fi
if [ -d migrations/sqlite ]; then
    mkdir -p "${payload}/migrations/sqlite"
    cp -r migrations/sqlite/. "${payload}/migrations/sqlite/"
fi

# Files come out of the checkout carrying the build user's mode; normalise so
# the binary stays executable and everything else is plainly readable.
chmod -R a+rX "${payload}"
chmod 0755 "${payload}/${PKG_NAME}"

# ── .desktop entry and icon ──
ICON_SRC="cmd/desktop/build/appicon.png"
test -f "${ICON_SRC}" || { echo "app icon not found: ${ICON_SRC}" >&2; exit 1; }

desktop_entry() {
    cat <<'ENTRY'
[Desktop Entry]
Type=Application
Name=WeKnora Lite
GenericName=Knowledge Base
Comment=Local RAG knowledge base (desktop edition)
Comment[zh_CN]=本地知识库 RAG 桌面版
Exec=/usr/bin/weknora-lite %U
Icon=weknora-lite
Terminal=false
Categories=Office;Utility;
Keywords=RAG;Knowledge;AI;
StartupNotify=true
ENTRY
}

# ── 1. .deb ──
deb_root="${WORK_DIR}/deb"
install_dir="${deb_root}/opt/weknora-lite"
mkdir -p "${install_dir}"
cp -r "${payload}/." "${install_dir}/"

mkdir -p "${deb_root}/usr/bin" \
         "${deb_root}/usr/share/applications" \
         "${deb_root}/usr/share/icons/hicolor/256x256/apps" \
         "${deb_root}/DEBIAN"

desktop_entry > "${deb_root}/usr/share/applications/weknora-lite.desktop"
chmod 0644 "${deb_root}/usr/share/applications/weknora-lite.desktop"
cp "${ICON_SRC}" "${deb_root}/usr/share/icons/hicolor/256x256/apps/weknora-lite.png"
chmod 0644 "${deb_root}/usr/share/icons/hicolor/256x256/apps/weknora-lite.png"

# Wrapper owns the writable paths. The real binary lives in /opt so it can be
# found by desktopConfigRoot regardless of how the launcher starts it.
cat > "${deb_root}/usr/bin/weknora-lite" <<'WRAPPER'
#!/bin/sh
set -e
data_dir="${HOME}/.weknora/data"
mkdir -p "${data_dir}/files"
export DB_PATH="${data_dir}/weknora.db"
export LOCAL_STORAGE_BASE_DIR="${data_dir}/files"
exec /opt/weknora-lite/weknora-lite "$@"
WRAPPER
chmod 0755 "${deb_root}/usr/bin/weknora-lite"

cat > "${deb_root}/DEBIAN/control" <<CONTROL
Package: ${PKG_NAME}
Version: ${VERSION}
Architecture: ${DEB_ARCH:-amd64}
Maintainer: gass88wei <gass88wei@users.noreply.github.com>
Installed-Size: $(du -sk "${install_dir}" | cut -f1)
Depends: libc6 (>= 2.34), libgtk-3-0 | libgtk-3-0t64, libwebkit2gtk-4.1-0
Section: utils
Priority: optional
Homepage: https://github.com/Tencent/WeKnora
Description: WeKnora Lite desktop - local RAG knowledge base
 WeKnora Lite turns documents into a queryable knowledge base that runs
 entirely on this machine. This package ships the desktop (Wails) build,
 which serves its own embedded web UI and needs no database, cache or
 message queue. Document parse quality is limited to the built-in simple
 engine; cloud parsers can be configured in the app.
CONTROL
chmod 0644 "${deb_root}/DEBIAN/control"

cat > "${deb_root}/DEBIAN/postinst" <<'POSTINST'
#!/bin/sh
set -e
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database -q /usr/share/applications || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
fi
exit 0
POSTINST
chmod 0755 "${deb_root}/DEBIAN/postinst"

cat > "${deb_root}/DEBIAN/postrm" <<'POSTRM'
#!/bin/sh
set -e
if [ "$1" = "purge" ]; then
    echo "WeKnora Lite: user data was left in ~/.weknora and was not removed."
fi
exit 0
POSTRM
chmod 0755 "${deb_root}/DEBIAN/postrm"

DEB_FILE="WeKnora-Lite-App_${RAW_VERSION}_${DEB_ARCH:-amd64}.deb"
dpkg-deb --root-owner-group -Zxz --build "${deb_root}" "${DEB_FILE}"
sha256sum "${DEB_FILE}" > "${DEB_FILE}.sha256"

echo "Built:"
ls -la "${DEB_FILE}"
