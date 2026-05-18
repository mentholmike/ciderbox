#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
lifecycle_script="${CRABBOX_MACOS_LIFECYCLE_SCRIPT:-$ROOT/scripts/macos-image-lifecycle-smoke.sh}"
prep_script="${CRABBOX_MACOS_SOURCE_PREP_SCRIPT:-$ROOT/scripts/install-macos-developer-tools.sh}"
image_name="${CRABBOX_MACOS_IMAGE_NAME:-crabbox-macos-devtools-$(date -u +%Y%m%d-%H%M)}"
run_existing="${CRABBOX_MACOS_RUN:-0}"
allocate="${CRABBOX_MACOS_ALLOCATE:-0}"
create_image="${CRABBOX_MACOS_CREATE_IMAGE:-1}"
promote="${CRABBOX_MACOS_PROMOTE:-1}"
checkpoint="${CRABBOX_MACOS_CHECKPOINT:-$create_image}"
open_webvnc="${CRABBOX_MACOS_OPEN_WEBVNC:-0}"
keep_lease="${CRABBOX_MACOS_KEEP_LEASE:-0}"
release_host="${CRABBOX_MACOS_RELEASE_HOST:-0}"

usage() {
  cat <<'USAGE'
Usage: scripts/mint-macos-devtools-image.sh [flags]

Thin maintainer wrapper around scripts/macos-image-lifecycle-smoke.sh for the
generic macOS developer-tools AMI.

By default this only runs no-spend preflight checks. Paid work requires one of:
  --use-existing   use an already available EC2 Mac Dedicated Host
  --allocate       allocate a paid EC2 Mac Dedicated Host when needed

Flags:
  --region REGION       set CRABBOX_MACOS_REGION
  --type TYPE           set CRABBOX_MACOS_TYPE, default mac2.metal
  --name NAME           set CRABBOX_MACOS_IMAGE_NAME
  --use-existing        continue with an available host
  --allocate            allow paid host allocation when no host is available
  --release-host        release the allocated host after proof
  --keep-lease          keep leases alive for debugging
  --open                open WebVNC during proof
  --no-promote          create and smoke the candidate AMI but do not promote it
  --no-checkpoint       skip checkpoint fork proof
  -h, --help            show this help

Useful env:
  CRABBOX_BIN
  CRABBOX_MACOS_REGIONS
  CRABBOX_MACOS_REGION_PREFLIGHT
  CRABBOX_MACOS_CREATE_IMAGE
  CRABBOX_MACOS_ARTIFACT_DIR
  CRABBOX_MACOS_HOST_WAIT_TIMEOUT
  CRABBOX_MACOS_RELEASE_EXISTING_HOST
USAGE
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --region)
      [[ "$#" -ge 2 ]] || { printf '%s requires a value\n' "$1" >&2; exit 2; }
      export CRABBOX_MACOS_REGION="$2"
      shift 2
      ;;
    --type)
      [[ "$#" -ge 2 ]] || { printf '%s requires a value\n' "$1" >&2; exit 2; }
      export CRABBOX_MACOS_TYPE="$2"
      shift 2
      ;;
    --name)
      [[ "$#" -ge 2 ]] || { printf '%s requires a value\n' "$1" >&2; exit 2; }
      image_name="$2"
      shift 2
      ;;
    --use-existing)
      run_existing=1
      shift
      ;;
    --allocate)
      allocate=1
      shift
      ;;
    --release-host)
      release_host=1
      shift
      ;;
    --keep-lease)
      keep_lease=1
      shift
      ;;
    --open)
      open_webvnc=1
      shift
      ;;
    --no-promote)
      promote=0
      shift
      ;;
    --no-checkpoint)
      checkpoint=0
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! -x "$lifecycle_script" ]]; then
  printf 'macOS lifecycle script is not executable: %s\n' "$lifecycle_script" >&2
  exit 2
fi
if [[ ! -f "$prep_script" ]]; then
  printf 'macOS developer-tools prep script not found: %s\n' "$prep_script" >&2
  exit 2
fi

cat >&2 <<EOF
macOS devtools image mint
  image: $image_name
  prep:  $prep_script
  paid:  use_existing=$run_existing allocate=$allocate release_host=$release_host
  proof: create_image=$create_image checkpoint=$checkpoint promote=$promote webvnc_open=$open_webvnc
EOF

export CRABBOX_MACOS_SOURCE_PREP_SCRIPT="$prep_script"
export CRABBOX_MACOS_IMAGE_NAME="$image_name"
export CRABBOX_MACOS_RUN="$run_existing"
export CRABBOX_MACOS_ALLOCATE="$allocate"
export CRABBOX_MACOS_CREATE_IMAGE="$create_image"
export CRABBOX_MACOS_PROMOTE="$promote"
export CRABBOX_MACOS_CHECKPOINT="$checkpoint"
export CRABBOX_MACOS_OPEN_WEBVNC="$open_webvnc"
export CRABBOX_MACOS_KEEP_LEASE="$keep_lease"
export CRABBOX_MACOS_RELEASE_HOST="$release_host"

exec "$lifecycle_script"
