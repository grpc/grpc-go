#!/bin/sh

cat <<EOF
#######################################
## digest auth test server starting ###
#######################################

You should be able to access http://localhost:12345/index.html w/o auth.

Accessing http://localhost:12345/auth-digest/index.html will require digest
authentication (user: "admin", pass: "secret").

You may also use localhost:12345 as a proxy (try with your web browser!). For
using the proxy you'lll need to authenticate via digest, using the above
credentials.

EOF

function set_or_append() {
    local ret="$1"
    local add="$2"

    if [ -z "$ret" ]; then
        ret="/usr/bin/tail -f $2"
    else
        ret="$ret $2"
    fi

    echo "$ret"
}

cmd=""
while [ $# -gt 0 ] ; do
    case "$1" in
        -t|--tail) cmd=$(set_or_append "$cmd" "/var/log/apache2/error.log");;
        -f|--forensic) cmd=$(set_or_append \
                                    "$cmd" "/var/log/apache2/forensic.log");;
    esac
    shift
done

docker ps | grep -q digest-test-apache || { 
    echo "Starting container."
    docker run -dit --net=host --name=digest-test-apache digest-test-apache; }

[ -n "$cmd" ] && { 
    docker exec -ti digest-test-apache \
        $cmd
        }
