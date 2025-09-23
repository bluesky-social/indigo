#!/usr/bin/env bash

set -e              # fail on error
set -u              # fail if variable not set in substitution
set -o pipefail     # fail if part of a '|' command fails

if test -z "${RELAY_ADMIN_KEY}"; then
    echo "RELAY_ADMIN_KEY secret is not defined"
    exit -1
fi

if test -z "${RELAY_HOST}"; then
    echo "RELAY_HOST config not defined"
    exit -1
fi

if test -z "$1"; then
    echo "expected PDS hostname as an argument"
    exit -1
fi

echo "POST resync $1"
http --ignore-stdin post https://${RELAY_HOST}/admin/pds/resync Authorization:"Bearer ${RELAY_ADMIN_KEY}" \
	host==$1
