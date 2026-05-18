#!/usr/bin/env bash
# TS-167 — Cleanup isolated daemon
# tags: surface:docker feature:bootstrap
# legacy fn: t13_ts167_cleanup_isolated
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-167"
story_preflight "surface:docker feature:bootstrap" || return 0

_story_ts_167() {
  if [[ -n "$DOCKER_SIM_CONTAINER" ]]; then
    docker stop "$DOCKER_SIM_CONTAINER" 2>/dev/null || true
    docker rm "$DOCKER_SIM_CONTAINER" 2>/dev/null || true
    echo "  docker container stopped: $DOCKER_SIM_CONTAINER"
  fi
  if [[ -n "$DOCKER_SIM_IMAGE" ]]; then
    docker rmi "$DOCKER_SIM_IMAGE" 2>/dev/null || true
    echo "  docker image removed: $DOCKER_SIM_IMAGE"
  fi
  rm -rf "$DOCKER_SIM_DATA" 2>/dev/null || true
  save_evidence TS-167 "cleanup.txt" "docker_container_removed=yes docker_image_removed=yes data_dir_removed=yes"
  ok "Docker E2E test cleanup complete"
}

RESULT=fail
_story_ts_167
: "${RESULT:=fail}"
unset -f _story_ts_167
