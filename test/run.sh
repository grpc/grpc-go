#!/bin/bash

for i in {1..1000}
do
  go test -cpu 4
  echo "$i done"
done
