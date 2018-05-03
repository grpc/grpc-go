/*
 * Copyright 2017 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.netty.shaded;

import static com.google.common.truth.Truth.assertThat;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.internal.testing.TestUtils;
import io.grpc.netty.shaded.io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.shaded.io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.shaded.io.grpc.netty.NettyServerBuilder;
import io.grpc.netty.shaded.io.netty.handler.ssl.SslContextBuilder;
import io.grpc.netty.shaded.io.netty.handler.ssl.SslProvider;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.protobuf.SimpleRequest;
import io.grpc.testing.protobuf.SimpleResponse;
import io.grpc.testing.protobuf.SimpleServiceGrpc;
import io.grpc.testing.protobuf.SimpleServiceGrpc.SimpleServiceBlockingStub;
import io.grpc.testing.protobuf.SimpleServiceGrpc.SimpleServiceImplBase;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link Shading}. */
@RunWith(JUnit4.class)
public final class ShadingTest {
  private ManagedChannel channel;
  private Server server;

  @After
  public void tearDown() throws Exception {
    if (channel != null) {
      channel.shutdownNow();
      channel.awaitTermination(1, TimeUnit.SECONDS);
    }
    if (server != null) {
      server.shutdownNow();
      server.awaitTermination(1, TimeUnit.SECONDS);
    }
  }

  /** Verify that normal Netty didn't leak into the test runtime. */
  @Test(expected = ClassNotFoundException.class)
  public void noNormalNetty() throws Exception {
    Class.forName("io.grpc.netty.NettyServerBuilder");
  }

  @Test
  public void serviceLoaderFindsNetty() throws Exception {
    assertThat(ServerBuilder.forPort(0)).isInstanceOf(NettyServerBuilder.class);
    assertThat(ManagedChannelBuilder.forAddress("localhost", 1234))
        .isInstanceOf(NettyChannelBuilder.class);
  }

  @Test
  public void basic() throws Exception {
    server = ServerBuilder.forPort(0)
        .addService(new SimpleServiceImpl())
        .build().start();
    channel = ManagedChannelBuilder
        .forAddress("localhost", server.getPort())
        .usePlaintext()
        .build();
    SimpleServiceBlockingStub stub = SimpleServiceGrpc.newBlockingStub(channel);
    assertThat(SimpleResponse.getDefaultInstance())
        .isEqualTo(stub.unaryRpc(SimpleRequest.getDefaultInstance()));
  }

  @Test
  public void tcnative() throws Exception {
    server = NettyServerBuilder.forPort(0)
        .useTransportSecurity(TestUtils.loadCert("server1.pem"), TestUtils.loadCert("server1.key"))
        .addService(new SimpleServiceImpl())
        .build().start();
    channel = NettyChannelBuilder
        .forAddress("localhost", server.getPort())
        .sslContext(
            GrpcSslContexts.configure(SslContextBuilder.forClient(), SslProvider.OPENSSL)
                .trustManager(TestUtils.loadCert("ca.pem")).build())
        .overrideAuthority("foo.test.google.fr")
        .build();
    SimpleServiceBlockingStub stub = SimpleServiceGrpc.newBlockingStub(channel);
    assertThat(SimpleResponse.getDefaultInstance())
        .isEqualTo(stub.unaryRpc(SimpleRequest.getDefaultInstance()));
  }

  private static class SimpleServiceImpl extends SimpleServiceImplBase {
    @Override public void unaryRpc(SimpleRequest req, StreamObserver<SimpleResponse> obs) {
      obs.onNext(SimpleResponse.getDefaultInstance());
      obs.onCompleted();
    }
  }
}
