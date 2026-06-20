#!/usr/bin/env bash

set -euo pipefail

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "ERROR: required environment variable ${name} is not set" >&2
    exit 1
  fi
}

for required_var in \
  PROTON_BRIDGE_SMTP_PORT \
  PROTON_BRIDGE_IMAP_PORT \
  PROTON_BRIDGE_HOST \
  CONTAINER_SMTP_PORT \
  CONTAINER_IMAP_PORT
do
  require_env "${required_var}"
done

# A previously killed container can leave stale gpg lock files and agent sockets
# in the persisted GNUPGHOME. They make `gpg --list-keys` block until it times
# out, which we'd misread below as a missing key. Nothing gpg-related is running
# yet at this point, so clearing them is safe.
gnupg_home="${GNUPGHOME:-${HOME}/.gnupg}"
if [ -d "${gnupg_home}" ]; then
  find "${gnupg_home}" \( -name '*.lock' -o -type s \) -delete 2>/dev/null || true
fi

store_exists=false
if [ -d "${HOME}/.password-store/" ]; then
  store_exists=true
fi

key_exists=false
if gpg --batch --list-keys ProtonMailBridge >/dev/null 2>&1; then
  key_exists=true
fi

case "${store_exists}:${key_exists}" in
  false:false)
    gpg --generate-key --batch /app/GPGparams.txt
    pass init ProtonMailBridge
    ;;
  false:true)
    pass init ProtonMailBridge
    ;;
  true:false)
    echo "ERROR: password store exists but ProtonMailBridge GPG key is missing" >&2
    echo "Delete ${HOME}/.password-store or restore the matching GPG key material." >&2
    exit 1
    ;;
  true:true)
    ;;
esac

exec /usr/bin/s6-svscan /app/services
