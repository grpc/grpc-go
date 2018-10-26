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

package io.grpc.services;

import static com.google.common.truth.Truth.assertWithMessage;
import static org.junit.Assert.assertEquals;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import io.grpc.InternalChannelz;
import io.grpc.Status;
import io.grpc.channelz.v1.GetChannelRequest;
import io.grpc.channelz.v1.GetChannelResponse;
import io.grpc.channelz.v1.GetServersRequest;
import io.grpc.channelz.v1.GetServersResponse;
import io.grpc.channelz.v1.GetSocketRequest;
import io.grpc.channelz.v1.GetSocketResponse;
import io.grpc.channelz.v1.GetSubchannelRequest;
import io.grpc.channelz.v1.GetSubchannelResponse;
import io.grpc.channelz.v1.GetTopChannelsRequest;
import io.grpc.channelz.v1.GetTopChannelsResponse;
import io.grpc.services.ChannelzTestHelper.TestChannel;
import io.grpc.services.ChannelzTestHelper.TestServer;
import io.grpc.services.ChannelzTestHelper.TestSocket;
import io.grpc.stub.StreamObserver;
import java.util.concurrent.ExecutionException;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

@RunWith(JUnit4.class)
public class ChannelzServiceTest {
  // small value to force pagination
  private static final int MAX_PAGE_SIZE = 1;

  private final InternalChannelz channelz = new InternalChannelz();
  private ChannelzService service = new ChannelzService(channelz, MAX_PAGE_SIZE);

  @Test
  public void getTopChannels_empty() {
    assertEquals(
        GetTopChannelsResponse.newBuilder().setEnd(true).build(),
        getTopChannelHelper(0));
  }

  @Test
  public void getTopChannels_onePage() throws Exception {
    TestChannel root = new TestChannel();
    channelz.addRootChannel(root);

    assertEquals(
        GetTopChannelsResponse
            .newBuilder()
            .addChannel(ChannelzProtoUtil.toChannel(root))
            .setEnd(true)
            .build(),
        getTopChannelHelper(0));
  }

  @Test
  public void getChannel() throws ExecutionException, InterruptedException {
    TestChannel root = new TestChannel();
    assertChannelNotFound(root.getLogId().getId());

    channelz.addRootChannel(root);
    assertEquals(
        GetChannelResponse
            .newBuilder()
            .setChannel(ChannelzProtoUtil.toChannel(root))
            .build(),
        getChannelHelper(root.getLogId().getId()));

    channelz.removeRootChannel(root);
    assertChannelNotFound(root.getLogId().getId());
  }

  @Test
  public void getSubchannel() throws Exception {
    TestChannel subchannel = new TestChannel();
    assertSubchannelNotFound(subchannel.getLogId().getId());

    channelz.addSubchannel(subchannel);
    assertEquals(
        GetSubchannelResponse
            .newBuilder()
            .setSubchannel(ChannelzProtoUtil.toSubchannel(subchannel))
            .build(),
        getSubchannelHelper(subchannel.getLogId().getId()));

    channelz.removeSubchannel(subchannel);
    assertSubchannelNotFound(subchannel.getLogId().getId());
  }

  @Test
  public void getServers_empty() {
    assertEquals(
        GetServersResponse.newBuilder().setEnd(true).build(),
        getServersHelper(0));
  }

  @Test
  public void getServers_onePage() throws Exception {
    TestServer server = new TestServer();
    channelz.addServer(server);

    assertEquals(
        GetServersResponse
            .newBuilder()
            .addServer(ChannelzProtoUtil.toServer(server))
            .setEnd(true)
            .build(),
        getServersHelper(0));
  }

  @Test
  public void getSocket() throws Exception {
    TestSocket socket = new TestSocket();
    assertSocketNotFound(socket.getLogId().getId());

    channelz.addClientSocket(socket);
    assertEquals(
        GetSocketResponse
            .newBuilder()
            .setSocket(ChannelzProtoUtil.toSocket(socket))
            .build(),
        getSocketHelper(socket.getLogId().getId()));

    channelz.removeClientSocket(socket);
    assertSocketNotFound(socket.getLogId().getId());
  }

  private GetTopChannelsResponse getTopChannelHelper(long startId) {
    @SuppressWarnings("unchecked")
    StreamObserver<GetTopChannelsResponse> observer = mock(StreamObserver.class);
    ArgumentCaptor<GetTopChannelsResponse> responseCaptor
        = ArgumentCaptor.forClass(GetTopChannelsResponse.class);
    service.getTopChannels(
        GetTopChannelsRequest.newBuilder().setStartChannelId(startId).build(),
        observer);
    verify(observer).onNext(responseCaptor.capture());
    verify(observer).onCompleted();
    return responseCaptor.getValue();
  }

  private GetChannelResponse getChannelHelper(long id) {
    @SuppressWarnings("unchecked")
    StreamObserver<GetChannelResponse> observer = mock(StreamObserver.class);
    ArgumentCaptor<GetChannelResponse> response
        = ArgumentCaptor.forClass(GetChannelResponse.class);
    service.getChannel(GetChannelRequest.newBuilder().setChannelId(id).build(), observer);
    verify(observer).onNext(response.capture());
    verify(observer).onCompleted();
    return response.getValue();
  }

  private void assertChannelNotFound(long id) {
    @SuppressWarnings("unchecked")
    StreamObserver<GetChannelResponse> observer = mock(StreamObserver.class);
    ArgumentCaptor<Exception> exceptionCaptor = ArgumentCaptor.forClass(Exception.class);
    service.getChannel(GetChannelRequest.newBuilder().setChannelId(id).build(), observer);
    verify(observer).onError(exceptionCaptor.capture());
    Status s = Status.fromThrowable(exceptionCaptor.getValue());
    assertWithMessage(s.toString()).that(s.getCode()).isEqualTo(Status.Code.NOT_FOUND);
  }

  private GetSubchannelResponse getSubchannelHelper(long id) {
    @SuppressWarnings("unchecked")
    StreamObserver<GetSubchannelResponse> observer = mock(StreamObserver.class);
    ArgumentCaptor<GetSubchannelResponse> response
        = ArgumentCaptor.forClass(GetSubchannelResponse.class);
    service.getSubchannel(GetSubchannelRequest.newBuilder().setSubchannelId(id).build(), observer);
    verify(observer).onNext(response.capture());
    verify(observer).onCompleted();
    return response.getValue();
  }

  private void assertSubchannelNotFound(long id) {
    @SuppressWarnings("unchecked")
    StreamObserver<GetSubchannelResponse> observer = mock(StreamObserver.class);
    ArgumentCaptor<Exception> exceptionCaptor = ArgumentCaptor.forClass(Exception.class);
    service.getSubchannel(GetSubchannelRequest.newBuilder().setSubchannelId(id).build(), observer);
    verify(observer).onError(exceptionCaptor.capture());
    Status s = Status.fromThrowable(exceptionCaptor.getValue());
    assertWithMessage(s.toString()).that(s.getCode()).isEqualTo(Status.Code.NOT_FOUND);
  }

  private GetServersResponse getServersHelper(long startId) {
    @SuppressWarnings("unchecked")
    StreamObserver<GetServersResponse> observer = mock(StreamObserver.class);
    ArgumentCaptor<GetServersResponse> responseCaptor
        = ArgumentCaptor.forClass(GetServersResponse.class);
    service.getServers(
        GetServersRequest.newBuilder().setStartServerId(startId).build(),
        observer);
    verify(observer).onNext(responseCaptor.capture());
    verify(observer).onCompleted();
    return responseCaptor.getValue();
  }

  private void assertSocketNotFound(long id) {
    @SuppressWarnings("unchecked")
    StreamObserver<GetSocketResponse> observer = mock(StreamObserver.class);
    ArgumentCaptor<Exception> exceptionCaptor = ArgumentCaptor.forClass(Exception.class);
    service.getSocket(GetSocketRequest.newBuilder().setSocketId(id).build(), observer);
    verify(observer).onError(exceptionCaptor.capture());
    Status s = Status.fromThrowable(exceptionCaptor.getValue());
    assertWithMessage(s.toString()).that(s.getCode()).isEqualTo(Status.Code.NOT_FOUND);
  }

  private GetSocketResponse getSocketHelper(long id) {
    @SuppressWarnings("unchecked")
    StreamObserver<GetSocketResponse> observer = mock(StreamObserver.class);
    ArgumentCaptor<GetSocketResponse> response
        = ArgumentCaptor.forClass(GetSocketResponse.class);
    service.getSocket(GetSocketRequest.newBuilder().setSocketId(id).build(), observer);
    verify(observer).onNext(response.capture());
    verify(observer).onCompleted();
    return response.getValue();
  }
}
