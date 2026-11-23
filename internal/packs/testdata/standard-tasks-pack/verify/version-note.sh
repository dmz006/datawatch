#!/usr/bin/env bash
# Verification for the release version note standard task.
# Note task: changes no files by design, so there is nothing for a
# script to verify. The note is a record for the operator: the release
# version is the next patch above main, resolved at merge time, and it
# never writes a file or touches a version field.
exit 0
