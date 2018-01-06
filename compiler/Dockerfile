FROM protoc-artifacts:latest

RUN scl enable devtoolset-1.1 'bash -c "cd /protobuf && \
    git fetch && \
    git checkout v3.5.1 && \
    ./autogen.sh && \
    CXXFLAGS=-m32 ./configure --disable-shared --prefix=/protobuf-32 && \
    make clean && make -j$(nproc) && make -j$(nproc) install"'

RUN scl enable devtoolset-1.1 'bash -c "cd /protobuf && \
    CXXFLAGS=-m64 ./configure --disable-shared --prefix=/protobuf-64 && \
    make clean && make -j$(nproc) && make -j$(nproc) install"'

ENV CXXFLAGS=-I/protobuf-32/include \
    LDFLAGS="-L/protobuf-32/lib -L/protobuf-64/lib"

RUN git clone --depth 1 https://github.com/grpc/grpc-java.git

# Start in devtoolset environment that uses GCC 4.7
CMD ["scl", "enable", "devtoolset-1.1", "bash"]
