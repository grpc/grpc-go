/*
 * Copyright 2015, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.internal;

import static org.mockito.Matchers.any;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import io.grpc.CallOptions;
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
        new LinkedBlockingQueue<MockClientTransportInfo>();

    doAnswer(new Answer<ConnectionClientTransport>() {
      @Override
      public ConnectionClientTransport answer(InvocationOnMock invocation) throws Throwable {
        final ConnectionClientTransport mockTransport = mock(ConnectionClientTransport.class);
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
        .newClientTransport(any(SocketAddress.class), any(String.class), any(String.class));

    return captor;
  }

  private TestUtils() {
  }
}
