#!/bin/sh
# Copy the baked-in Kura node package into the shared volume n8n scans via
# N8N_CUSTOM_EXTENSIONS. Intended for an initContainer with an emptyDir target.
set -eu

TARGET="${KURA_NODES_TARGET:-/opt/n8n/custom}"
DEST="${TARGET}/n8n-nodes-kura"

echo "kura-n8n-nodes: installing into ${DEST}"
mkdir -p "${DEST}"
cp -r /kura-nodes/. "${DEST}/"
chmod -R a+rX "${DEST}"
echo "kura-n8n-nodes: installed"
ls -la "${DEST}"
