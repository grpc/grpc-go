#!/bin/bash
set -e

cd "$(dirname "$0")/.."

tmpdir="$(mktemp -d)"
curl -Ls https://github.com/grpc/grpc-proto/archive/master.tar.gz | tar xz -C "$tmpdir"
base="$tmpdir/grpc-proto-master"

# Copy protos in 'src/main/proto' from grpc-proto for these projects
for project in alts grpclb services; do
  while read -r proto; do
    [ -f "$base/$proto" ] && cp "$base/$proto" "$project/src/main/proto/$proto"
    echo "$proto"
  done < <(cd "$project/src/main/proto" && find . -name "*.proto")
done | sort > "$tmpdir/grpc-java.lst"

(cd "$base" && find . -name "*.proto") | sort > "$tmpdir/base.lst"
echo "Files synced:"
comm -12 "$tmpdir/grpc-java.lst" "$tmpdir/base.lst"

echo
echo "Files in grpc-proto not synced:"
comm -13 "$tmpdir/grpc-java.lst" "$tmpdir/base.lst"

echo
echo "Files in grpc-java not synced:"
comm -23 "$tmpdir/grpc-java.lst" "$tmpdir/base.lst"

rm -r "$tmpdir"
