#!/usr/bin/env bash
# Verification for the bounded-load-step standard task.
# Note task: changes no files by design, so there is nothing for a
# script to verify. The step is complete when the posture note
# (nice -n 19, a quarter of the cores, a hard timeout, a trap on
# EXIT INT TERM) is recorded in the story.
exit 0
