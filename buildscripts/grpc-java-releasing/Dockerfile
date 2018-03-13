FROM protoc-artifacts:latest

COPY scl-enable-devtoolset.sh /

# Start in devtoolset environment that uses GCC 4.7
ENTRYPOINT ["/scl-enable-devtoolset.sh"]
