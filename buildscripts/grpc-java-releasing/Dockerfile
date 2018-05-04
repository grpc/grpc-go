FROM protoc-artifacts:latest

# protoc-artifacts base image is ancient and curl does
# not work properly. Ignore rpmdb checksum errors from update.
RUN yum -y update; yum clean all # Update to >= 4/24/2018
COPY scl-enable-devtoolset.sh /

# Start in devtoolset environment that uses GCC 4.7
ENTRYPOINT ["/scl-enable-devtoolset.sh"]
