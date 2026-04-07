#!/usr/bin/env sh
set -eu

VERSION=""
BUMP=""
GOALS=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      VERSION="${2-}"
      shift 2
      ;;
    --bump)
      BUMP="${2-}"
      shift 2
      ;;
    --goals)
      GOALS="${2-}"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

for goal in $GOALS; do
  case "$goal" in
    bump-patch)
      goal_bump="patch"
      ;;
    bump-minor)
      goal_bump="minor"
      ;;
    bump-major)
      goal_bump="major"
      ;;
    *)
      goal_bump=""
      ;;
  esac

  if [ -z "$goal_bump" ]; then
    continue
  fi

  if [ -n "$BUMP" ] && [ "$BUMP" != "$goal_bump" ]; then
    echo "conflicting bump selectors: $BUMP and $goal_bump" >&2
    exit 1
  fi

  BUMP="$goal_bump"
done

if [ -n "$VERSION" ] && [ -n "$BUMP" ]; then
  echo "specify either VERSION=vX.Y.Z or one bump selector" >&2
  exit 1
fi

if [ -z "$VERSION" ] && [ -n "$BUMP" ]; then
  VERSION=$(CDPATH= cd -- "$(dirname "$0")" && ./next-cli-version.sh "$BUMP")
fi

if [ -z "$VERSION" ]; then
  echo "VERSION is required. Examples: make release-cli VERSION=v0.1.0 or make release-cli bump-patch" >&2
  exit 1
fi

printf '%s\n' "$VERSION"
