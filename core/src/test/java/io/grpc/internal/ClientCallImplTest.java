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

import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import com.google.common.collect.ImmutableSet;
import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Codec;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

import java.io.InputStream;
import java.util.Set;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;

/**
 * Test for {@link ClientCallImpl}.
 */
@RunWith(JUnit4.class)
public class ClientCallImplTest {

  private final SerializingExecutor executor =
      new SerializingExecutor(MoreExecutors.newDirectExecutorService());
  private final ScheduledExecutorService deadlineCancellationExecutor =
      Executors.newScheduledThreadPool(0);

  @Before
  public void setUp() {
    DecompressorRegistry.register(new Codec.Gzip(), true);
  }

  @Test
  public void advertisedEncodingsAreSent() {
    MethodDescriptor<Void, Void> descriptor = MethodDescriptor.create(
        MethodType.UNARY,
        "service/method",
        new TestMarshaller<Void>(),
        new TestMarshaller<Void>());
    final ClientTransport transport = mock(ClientTransport.class);
    ClientTransportProvider provider = new ClientTransportProvider() {
      @Override
      public ClientTransport get() {
        return transport;
      }
    };
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        descriptor,
        executor,
        CallOptions.DEFAULT,
        provider,
        deadlineCancellationExecutor);

    call.start(new TestClientCallListener<Void>(), new Metadata());

    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);
    verify(transport).newStream(
        eq(descriptor), metadataCaptor.capture(), isA(ClientStreamListener.class));
    Metadata actual = metadataCaptor.getValue();

    Set<String> acceptedEncodings =
        ImmutableSet.copyOf(actual.getAll(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY));
    assertEquals(DecompressorRegistry.getAdvertisedMessageEncodings(), acceptedEncodings);
  }

  private static class TestMarshaller<T> implements Marshaller<T> {
    @Override
    public InputStream stream(T value) {
      return null;
    }

    @Override
    public T parse(InputStream stream) {
      return null;
    }
  }

  private static class TestClientCallListener<T> extends ClientCall.Listener<T> {
  }
}

