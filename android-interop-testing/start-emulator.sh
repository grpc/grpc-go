#!/bin/bash

# The only argument is the AVD name.
# Note: This script will hang, you may want to run it in the background.

if [ $# -eq 0 ]
then
    echo "Please specify the AVD name"
    exit 1
fi

echo "[INFO] Starting emulator $1"
emulator64-arm -avd $1 -netfast -no-skin -no-audio -no-window -port 5554
