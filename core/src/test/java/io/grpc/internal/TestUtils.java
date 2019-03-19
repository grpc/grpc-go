/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.internal;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import io.grpc.CallOptions;
import io.grpc.ChannelLogger;
import io.grpc.InternalLogId;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import java.net.SocketAddress;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import javax.annotation.Nullable;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Common utility methods for tests.
 */
final class TestUtils {

  static class MockClientTransportInfo {
    /**
     * A mock transport created by the mock transport factory.
     */
    final ConnectionClientTransport transport;

    /**
     * The listener passed to the start() of the mock transport.
     */
    final ManagedClientTransport.Listener listener;

    MockClientTransportInfo(ConnectionClientTransport transport,
        ManagedClientTransport.Listener listener) {
      this.transport = transport;
      this.listener = listener;
    }
  }

  /**
   * Stub the given mock {@link ClientTransportFactory} by returning mock
   * {@link ManagedClientTransport}s which saves their listeners along with them. This method
   * returns a list of {@link MockClientTransportInfo}, each of which is a started mock transport
   * and its listener.
   */
  static BlockingQueue<MockClientTransportInfo> captureTransports(
      ClientTransportFactory mockTransportFactory) {
    return captureTransports(mockTransportFactory, null);
  }

  static BlockingQueue<MockClientTransportInfo> captureTransports(
      ClientTransportFactory mockTransportFactory, @Nullable final Runnable startRunnable) {
    final BlockingQueue<MockClientTransportInfo> captor =
        new LinkedBlockingQueue<>();

    doAnswer(new Answer<ConnectionClientTransport>() {
      @Override
      public ConnectionClientTransport answer(InvocationOnMock invocation) throws Throwable {
        final ConnectionClientTransport mockTransport = mock(ConnectionClientTransport.class);
        when(mockTransport.getLogId())
            .thenReturn(InternalLogId.allocate("mocktransport", /*details=*/ null));
        when(mockTransport.newStream(
                any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class)))
            .thenReturn(mock(ClientStream.class));
        // Save the listener
        doAnswer(new Answer<Runnable>() {
          @Override
          public Runnable answer(InvocationOnMock invocation) throws Throwable {
            captor.add(new MockClientTransportInfo(
                mockTransport, (ManagedClientTransport.Listener) invocation.getArguments()[0]));
            return startRunnable;
          }
        }).when(mockTransport).start(any(ManagedClientTransport.Listener.class));
        return mockTransport;
      }
    }).when(mockTransportFactory)
        .newClientTransport(
            any(SocketAddress.class),
            any(ClientTransportFactory.ClientTransportOptions.class),
            any(ChannelLogger.class));

    return captor;
  }

  private TestUtils() {
  }
}
