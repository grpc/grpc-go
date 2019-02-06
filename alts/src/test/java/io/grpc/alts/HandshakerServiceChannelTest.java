/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts;

import static com.google.common.truth.Truth.assertThat;

import io.grpc.Channel;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.internal.SharedResourceHolder.Resource;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.GrpcCleanupRule;
import io.grpc.testing.protobuf.SimpleRequest;
import io.grpc.testing.protobuf.SimpleResponse;
import io.grpc.testing.protobuf.SimpleServiceGrpc;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class HandshakerServiceChannelTest {
  @Rule
  public final GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();
  private final Server server = grpcCleanup.register(
      ServerBuilder.forPort(0)
        .addService(new SimpleServiceGrpc.SimpleServiceImplBase() {
          @Override
          public void unaryRpc(SimpleRequest request, StreamObserver<SimpleResponse> so) {
            so.onNext(SimpleResponse.getDefaultInstance());
            so.onCompleted();
          }
        })
        .build());
  private Resource<Channel> resource;

  @Before
  public void setUp() throws Exception {
    server.start();
    resource =
        HandshakerServiceChannel.getHandshakerChannelForTesting("localhost:" + server.getPort());
  }

  @Test
  public void sharedChannel_authority() {
    resource = HandshakerServiceChannel.SHARED_HANDSHAKER_CHANNEL;
    Channel channel = resource.create();
    try {
      assertThat(channel.authority()).isEqualTo("metadata.google.internal.:8080");
    } finally {
      resource.close(channel);
    }
  }

  @Test
  public void resource_works() {
    Channel channel = resource.create();
    try {
      // Do an RPC to verify that the channel actually works
      doRpc(channel);
    } finally {
      resource.close(channel);
    }
  }

  @Test
  public void resource_lifecycleTwice() {
    Channel channel = resource.create();
    try {
      doRpc(channel);
    } finally {
      resource.close(channel);
    }
    channel = resource.create();
    try {
      doRpc(channel);
    } finally {
      resource.close(channel);
    }
  }

  private void doRpc(Channel channel) {
    SimpleServiceGrpc.newBlockingStub(channel).unaryRpc(SimpleRequest.getDefaultInstance());
  }
}
