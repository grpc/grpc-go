/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.okhttp;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.internal.ClientStreamListener;
import io.grpc.okhttp.internal.framed.ErrorCode;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.io.InputStream;
import java.util.concurrent.atomic.AtomicReference;

@RunWith(JUnit4.class)
public class OkHttpClientStreamTest {
  private static final int MAX_MESSAGE_SIZE = 100;

  @Mock private MethodDescriptor.Marshaller<Void> marshaller;
  @Mock private AsyncFrameWriter frameWriter;
  @Mock private OkHttpClientTransport transport;
  @Mock private OutboundFlowController flowController;
  private final Object lock = new Object();

  private MethodDescriptor<?, ?> methodDescriptor;
  private OkHttpClientStream stream;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    methodDescriptor = MethodDescriptor.create(
        MethodType.UNARY, "/testService/test", marshaller, marshaller);
    stream = new OkHttpClientStream(methodDescriptor, new Metadata(), frameWriter, transport,
        flowController, lock, MAX_MESSAGE_SIZE, "localhost");
  }

  @Test
  public void getType() {
    assertEquals(MethodType.UNARY, stream.getType());
  }

  @Test
  public void sendCancel_notStarted() {
    final AtomicReference<Status> statusRef = new AtomicReference<Status>();
    stream.start(new BaseClientStreamListener() {
      @Override
      public void closed(Status status, Metadata trailers) {
        statusRef.set(status);
        assertTrue(Thread.holdsLock(lock));
      }
    });

    stream.sendCancel(Status.CANCELLED);

    assertEquals(Status.Code.CANCELLED, statusRef.get().getCode());
  }

  @Test
  public void sendCancel_started() {
    stream.start(new BaseClientStreamListener());
    stream.start(1234);
    Mockito.doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Throwable {
        assertTrue(Thread.holdsLock(lock));
        return null;
      }
    }).when(transport).finishStream(1234, Status.CANCELLED, ErrorCode.CANCEL);

    stream.sendCancel(Status.CANCELLED);

    verify(transport).finishStream(1234, Status.CANCELLED, ErrorCode.CANCEL);
  }

  @Test
  public void start_alreadyCancelled() {
    stream.start(new BaseClientStreamListener());
    stream.sendCancel(Status.CANCELLED);

    stream.start(1234);

    verifyNoMoreInteractions(frameWriter);
  }


  // TODO(carl-mastrangelo): extract this out into a testing/ directory and remove other defintions
  // of it.
  private static class BaseClientStreamListener implements ClientStreamListener {
    @Override
    public void onReady() {}

    @Override
    public void messageRead(InputStream message) {}

    @Override
    public void headersRead(Metadata headers) {}

    @Override
    public void closed(Status status, Metadata trailers) {}
  }
}

