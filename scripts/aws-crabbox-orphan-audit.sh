#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/aws-crabbox-orphan-audit.sh --profile <aws-profile> [--profile <aws-profile> ...] [--region <region> ...] [--terminate]

Audits AWS accounts for Crabbox-tagged EC2 instances that look orphaned:
  - tag crabbox=true
  - non-terminated EC2 state
  - no active coordinator lease by lease tag or instance id after grace, or expires_at is past grace

The default mode is read-only and prints JSON lines. Add --terminate to terminate
the matching EC2 instances after printing them.

Environment:
  CRABBOX_BIN                 crabbox binary to query active leases; default bin/crabbox or crabbox
  CRABBOX_LEASE_AUDIT_LIMIT   active lease query limit; default 1000
  CRABBOX_AWS_ORPHAN_AUDIT_GRACE_SECONDS
                             grace period for stale tags; default CRABBOX_AWS_ORPHAN_SWEEP_GRACE_SECONDS or 900
USAGE
}

profiles=()
regions=()
terminate=0

split_csv() {
  local value="$1"
  local item
  IFS=',' read -ra parts <<<"$value"
  for item in "${parts[@]}"; do
    item="${item//[[:space:]]/}"
    [[ -n "$item" ]] && printf '%s\n' "$item"
  done
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      profiles+=("${2:?missing value for --profile}")
      shift 2
      ;;
    --profiles)
      while IFS= read -r profile; do profiles+=("$profile"); done < <(split_csv "${2:?missing value for --profiles}")
      shift 2
      ;;
    --region)
      regions+=("${2:?missing value for --region}")
      shift 2
      ;;
    --regions)
      while IFS= read -r region; do regions+=("$region"); done < <(split_csv "${2:?missing value for --regions}")
      shift 2
      ;;
    --terminate)
      terminate=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ${#profiles[@]} -eq 0 ]]; then
  if [[ -n "${CRABBOX_AWS_AUDIT_PROFILES:-}" ]]; then
    while IFS= read -r profile; do profiles+=("$profile"); done < <(split_csv "$CRABBOX_AWS_AUDIT_PROFILES")
  elif [[ -n "${AWS_PROFILE:-}" ]]; then
    profiles+=("$AWS_PROFILE")
  else
    echo "provide --profile or CRABBOX_AWS_AUDIT_PROFILES" >&2
    exit 2
  fi
fi

if ! command -v aws >/dev/null 2>&1; then
  echo "aws CLI is required" >&2
  exit 127
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 127
fi

crabbox_bin="${CRABBOX_BIN:-}"
if [[ -z "$crabbox_bin" ]]; then
  if [[ -x bin/crabbox ]]; then
    crabbox_bin="bin/crabbox"
  else
    crabbox_bin="crabbox"
  fi
fi
lease_limit="${CRABBOX_LEASE_AUDIT_LIMIT:-1000}"
if [[ ! "$lease_limit" =~ ^[0-9]+$ || "$lease_limit" -eq 0 ]]; then
  echo "CRABBOX_LEASE_AUDIT_LIMIT must be a positive integer" >&2
  exit 2
fi
effective_lease_limit="$lease_limit"
if [[ "$effective_lease_limit" -gt 500 ]]; then
  effective_lease_limit=500
fi
grace_seconds="${CRABBOX_AWS_ORPHAN_AUDIT_GRACE_SECONDS:-${CRABBOX_AWS_ORPHAN_SWEEP_GRACE_SECONDS:-900}}"
if [[ ! "$grace_seconds" =~ ^[0-9]+$ ]]; then
  echo "CRABBOX_AWS_ORPHAN_AUDIT_GRACE_SECONDS must be a non-negative integer" >&2
  exit 2
fi

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT

active_json="$tmpdir/active-leases.json"
active_loaded=false
active_truncated=false
if "$crabbox_bin" admin leases --json -state active -limit "$lease_limit" >"$active_json" 2>"$tmpdir/active-leases.err"; then
  jq -r '.[].id' "$active_json" | jq -R -s 'split("\n") | map(select(length > 0))' >"$tmpdir/active-ids.json"
  jq -r '.[].cloudID // empty' "$active_json" | jq -R -s 'split("\n") | map(select(length > 0))' >"$tmpdir/active-cloud-ids.json"
  jq '[.[] | select(.id != null) | {key: .id, value: (.cloudID // "")}] | from_entries' "$active_json" >"$tmpdir/active-cloud-by-id.json"
  active_loaded=true
  active_count="$(jq 'length' "$active_json")"
  if [[ "$active_count" -ge "$effective_lease_limit" ]]; then
    active_truncated=true
    echo "warning: active coordinator lease list reached limit $effective_lease_limit; destructive audit disabled" >&2
  fi
else
  jq -n '[]' >"$tmpdir/active-ids.json"
  jq -n '[]' >"$tmpdir/active-cloud-ids.json"
  jq -n '{}' >"$tmpdir/active-cloud-by-id.json"
  echo "warning: could not load active coordinator leases; falling back to expires_at-only detection" >&2
  sed 's/^/warning: /' "$tmpdir/active-leases.err" >&2
fi

if [[ "$terminate" == 1 && "$active_loaded" != true ]]; then
  echo "refusing --terminate because active coordinator leases could not be loaded" >&2
  exit 1
fi
if [[ "$terminate" == 1 && "$active_truncated" == true ]]; then
  echo "refusing --terminate because active coordinator leases may be truncated" >&2
  exit 1
fi

now="$(date -u +%s)"
matches="$tmpdir/matches.jsonl"
: >"$matches"

for profile in "${profiles[@]}"; do
  identity="$(aws sts get-caller-identity --profile "$profile" --region us-east-1 --output json)"
  account="$(jq -r '.Account' <<<"$identity")"

  if [[ ${#regions[@]} -eq 0 ]]; then
    scan_regions=()
    while IFS= read -r region_name; do
      [[ -n "$region_name" ]] && scan_regions+=("$region_name")
    done < <(
      aws ec2 describe-regions --profile "$profile" --region us-east-1 --all-regions --output json |
        jq -r '.Regions[] | select(.OptInStatus == null or .OptInStatus == "opt-in-not-required" or .OptInStatus == "opted-in") | .RegionName' |
        sort
    )
  else
    scan_regions=("${regions[@]}")
  fi

  for region in "${scan_regions[@]}"; do
    aws ec2 describe-instances \
      --profile "$profile" \
      --region "$region" \
      --filters Name=tag:crabbox,Values=true Name=instance-state-name,Values=pending,running,stopping,stopped \
      --output json |
      jq -c \
        --arg profile "$profile" \
        --arg account "$account" \
        --arg region "$region" \
        --argjson now "$now" \
        --argjson graceSeconds "$grace_seconds" \
        --argjson activeLoaded "$active_loaded" \
        --slurpfile active "$tmpdir/active-ids.json" \
        --slurpfile activeCloud "$tmpdir/active-cloud-ids.json" \
        --slurpfile activeCloudByID "$tmpdir/active-cloud-by-id.json" '
          def tag($key): ((.Tags // []) | map(select(.Key == $key))[0].Value // "");
          def flag_enabled($value): (($value // "") | ascii_downcase | test("^(1|true|yes|on)$"));
          def epoch($value):
            ($value // "") as $raw
            | if ($raw | test("^[0-9]+$")) then ($raw | tonumber)
              else (($raw | fromdateiso8601?) // null)
              end;
          .Reservations[].Instances[]? as $instance
          | ($instance | tag("lease")) as $lease
          | ($instance.InstanceId // "") as $instanceId
          | (($instance | tag("keep")) | flag_enabled) as $keep
          | (($instance | tag("created_at")) | epoch) as $created
          | (($instance | tag("expires_at")) | epoch) as $expires
          | ($created != null and ($created + $graceSeconds) <= $now) as $oldEnough
          | ($expires != null and ($expires + $graceSeconds) <= $now) as $expired
          | ($lease != "" and (($active[0] | index($lease)) != null)) as $activeLeaseKnown
          | ($activeCloudByID[0][$lease] // "") as $activeLeaseCloudID
          | ($activeLeaseKnown and $activeLeaseCloudID == $instanceId) as $activeLeaseMatchesCloud
          | ($instanceId != "" and (($activeCloud[0] | index($instanceId)) != null)) as $activeCloudKnown
          | ($activeLoaded and $lease != "" and ($activeLeaseKnown | not) and $oldEnough) as $notActive
          | ($activeLoaded and $activeLeaseKnown and ($activeLeaseMatchesCloud | not) and $oldEnough) as $leaseCloudMismatch
          | ($activeLoaded and $lease == "" and $oldEnough) as $missingLease
          | select(($keep | not) and ($activeCloudKnown | not) and (($activeLeaseKnown | not) or $leaseCloudMismatch) and ($expired or $notActive or $missingLease or $leaseCloudMismatch))
          | {
              profile: $profile,
              account: $account,
              region: $region,
              instanceId: $instanceId,
              state: $instance.State.Name,
              instanceType: $instance.InstanceType,
              launchTime: $instance.LaunchTime,
              publicIp: ($instance.PublicIpAddress // null),
              name: ($instance | tag("Name")),
              lease: $lease,
              owner: ($instance | tag("owner")),
              createdAtEpoch: $created,
              expiresAtEpoch: $expires,
              expired: $expired,
              activeLeaseKnown: $activeLeaseKnown,
              activeCloudKnown: $activeCloudKnown,
              activeLeaseCloudID: $activeLeaseCloudID,
              reason: (if $leaseCloudMismatch then "lease-cloud-mismatch" elif $expired and ($notActive or $missingLease) then "expired-and-orphaned" elif $expired then "expired" elif $notActive then "not-active" else "missing-lease-label" end)
            }
        ' >>"$matches"
  done
done

cat "$matches"

if [[ "$terminate" == 1 ]]; then
  jq -r '[.profile, .region, .instanceId] | @tsv' "$matches" |
    while IFS=$'\t' read -r profile region instance_id; do
      [[ -n "$instance_id" ]] || continue
      aws ec2 terminate-instances --profile "$profile" --region "$region" --instance-ids "$instance_id" --output json |
        jq -c '.TerminatingInstances[] | {instanceId:.InstanceId, previous:.PreviousState.Name, current:.CurrentState.Name}'
    done
fi
