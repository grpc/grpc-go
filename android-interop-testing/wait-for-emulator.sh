#!/bin/bash
echo "Waiting for emulator to start..."

bootanim=""
failcounter=0
until [[ "$bootanim" =~ "stopped" ]]; do
   bootanim=`adb -e shell getprop init.svc.bootanim 2>&1`
   let "failcounter += 1"
   # Timeout after 5 minutes.
   if [[ $failcounter -gt 300 ]]; then
      echo "Can not find device after 5 minutes..."
      exit 1
   fi
   sleep 1
done

