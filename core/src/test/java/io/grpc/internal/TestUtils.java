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

import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.net.SocketAddress;
import java.util.LinkedList;

/**
 * Common utility methods for tests.
 */
final class TestUtils {

  /**
   * Stub the given mock {@link ClientTransportFactory} by returning mock {@link ClientTransport}s
   * which saves their listeners to a list which is returned by this method.
   */
  static LinkedList<ClientTransport.Listener> captureListeners(
      ClientTransportFactory mockTransportFactory) {
    final LinkedList<ClientTransport.Listener> listeners =
        new LinkedList<ClientTransport.Listener>();

    doAnswer(new Answer<ClientTransport>() {
      @Override
      public ClientTransport answer(InvocationOnMock invocation) throws Throwable {
        ClientTransport mockTransport = mock(ClientTransport.class);
        // Save the listener
        doAnswer(new Answer<Void>() {
          @Override
          public Void answer(InvocationOnMock invocation) throws Throwable {
            listeners.add((ClientTransport.Listener) invocation.getArguments()[0]);
            return null;
          }
        }).when(mockTransport).start(any(ClientTransport.Listener.class));
        return mockTransport;
      }
    }).when(mockTransportFactory).newClientTransport(any(SocketAddress.class), any(String.class));

    return listeners;
  }
}
