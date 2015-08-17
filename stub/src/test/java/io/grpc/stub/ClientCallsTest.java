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

package io.grpc.stub;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.verify;

import com.google.common.util.concurrent.ListenableFuture;

import io.grpc.ClientCall;
import io.grpc.Metadata;
import io.grpc.Status;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.util.concurrent.CancellationException;
import java.util.concurrent.ExecutionException;

/**
 * Unit tests for {@link ClientCalls}.
 */
@RunWith(JUnit4.class)
public class ClientCallsTest {

  @Mock private ClientCall<Integer, String> call;

  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
  }

  @Test public void unaryFutureCallSuccess() throws Exception {
    Integer req = 2;
    ListenableFuture<String> future = ClientCalls.futureUnaryCall(call, req);
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    ClientCall.Listener<String> listener = listenerCaptor.getValue();
    verify(call).sendMessage(req);
    verify(call).halfClose();
    listener.onMessage("bar");
    listener.onClose(Status.OK, new Metadata());
    assertEquals("bar", future.get());
  }

  @Test public void unaryFutureCallFailed() throws Exception {
    Integer req = 2;
    ListenableFuture<String> future = ClientCalls.futureUnaryCall(call, req);
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    ClientCall.Listener<String> listener = listenerCaptor.getValue();
    listener.onClose(Status.INVALID_ARGUMENT, new Metadata());
    try {
      future.get();
      fail("Should fail");
    } catch (ExecutionException e) {
      Status status = Status.fromThrowable(e.getCause());
      assertEquals(Status.INVALID_ARGUMENT, status);
    }
  }

  @Test public void unaryFutureCallCancelled() throws Exception {
    Integer req = 2;
    ListenableFuture<String> future = ClientCalls.futureUnaryCall(call, req);
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    ClientCall.Listener<String> listener = listenerCaptor.getValue();
    future.cancel(true);
    verify(call).cancel();
    listener.onMessage("bar");
    listener.onClose(Status.OK, new Metadata());
    try {
      future.get();
      fail("Should fail");
    } catch (CancellationException e) {
      // Exepcted
    }
  }
}
