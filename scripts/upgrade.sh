#!/bin/bash

set -euo pipefail

BUILDCONFIGS="patchman-engine-app patchman-engine-database"
DEPLOYCONFIGS="
patchman-engine-database-admin
patchman-engine-evaluator-recalc
patchman-engine-evaluator-upload
patchman-engine-listener
patchman-engine-manager
patchman-engine-vmaas-sync
patchman-engine-database
"

# Make new builds from a given git tag
function upgrade-builds() {
  VERSION=$1
  TAG=${2:-VERSION}

  for BC in $BUILDCONFIGS; do
    PATCH=$(
      envsubst <<EOF
{
  "spec": {
    "output": {
      "to": {
        "name": "$BC:$VERSION"
      }
    },
    "source": {
      "git": {
        "ref": "$TAG"
      }
    }
  }
}
EOF
    )
  oc patch bc $BC
  oc start-build $BC -w
  done -p "$PATCH"
}

function tag-images() {
  VERSION=$1
  for BC in $BUILDCONFIGS; do
    oc tag "$BC:$VERSION" "$BC:latest"
  done
}

# Deploy versioned images
function upgrade-services() {
  VERSION=$1
  for DC in $DEPLOYCONFIGS; do
    PATCH=$(
      envsubst <<EOF
{
  "spec": {
    "triggers": [
      {"type": "ConfigChange"},
      {
        "type": "ImageChange",
        "imageChangeParams": {
          "automatic": true,
          "containerNames": ["$DC"],
          "from": {
            "name": "$DC:$VERSION"
          }
        }
      }
    ]
  }
}
EOF
    )
    oc patch dc $DC -p "$PATCH"
  done
}

function help() {
  cat <<EOF

Commands:
  upgrade-builds VERSION [GIT_TAG]
    - Upgrades BUILDCONFIGS to take GIT_TAG and output images with VERSION tag,
     by default, the GIT_TAG is set to VERSION
  upgrade-services VERSION
    - Upgrades DEPLOYCONFIGS to use images with VERSION tag

EOF
}


if [ $# -lt 2 ]; then
  help
else
  $@
fi
