#!/usr/bin/env bash


set -e

workdir=.cover
profile="$workdir/cover.out"
mode=set
end2endtest="github.com/lypnol/grpc-go/test"

generate_cover_data() {
    rm -rf "$workdir"
    mkdir "$workdir"

    for pkg in "$@"; do
        if [ $pkg == "github.com/lypnol/grpc-go" -o $pkg == "github.com/lypnol/grpc-go/transport" -o $pkg == "github.com/lypnol/grpc-go/metadata" -o $pkg == "github.com/lypnol/grpc-go/credentials" ]
            then
                f="$workdir/$(echo $pkg | tr / -)"
                go test -covermode="$mode" -coverprofile="$f.cover" "$pkg"
                go test -covermode="$mode" -coverpkg "$pkg" -coverprofile="$f.e2e.cover" "$end2endtest"
        fi
    done

    echo "mode: $mode" >"$profile"
    grep -h -v "^mode:" "$workdir"/*.cover >>"$profile"
}

show_cover_report() {
    go tool cover -${1}="$profile"
}

push_to_coveralls() {
    goveralls -coverprofile="$profile"
}

generate_cover_data $(go list ./...)
show_cover_report func
case "$1" in
"")
    ;;
--html)
    show_cover_report html ;;
--coveralls)
    push_to_coveralls ;;
*)
    echo >&2 "error: invalid option: $1" ;;
esac
rm -rf "$workdir"
