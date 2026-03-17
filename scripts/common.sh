#!/bin/bash

fail_on_output() {
  tee /dev/stderr | not read
}

# not makes sure the command passed to it does not exit with a return code of 0.
not() {
  # This is required instead of the earlier (! $COMMAND) because subshells and
  # pipefail don't work the same on Darwin as in Linux.
  ! "$@"
}

# noret_grep will return 0 if zero or more lines were selected, and >1 if an
# error occurred. Suppresses grep's return code of 1 when there are no matches
# (for eg, empty file).
noret_grep() {
  grep "$@" || [[ $? == 1 ]]
}

die() {
  echo "$@" >&2
  exit 1
}
