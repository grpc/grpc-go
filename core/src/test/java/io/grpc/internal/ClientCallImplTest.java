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
import static org.junit.Assert.assertNull;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.base.Splitter;
import com.google.common.collect.ImmutableSet;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Codec;
import io.grpc.Decompressor;
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

import java.io.IOException;
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
  private final DecompressorRegistry decompressorRegistry =
      DecompressorRegistry.getDefaultInstance();

  @Before
  public void setUp() {
    decompressorRegistry.register(new Codec.Gzip(), true);
  }

  @Test
  public void advertisedEncodingsAreSent() {
    MethodDescriptor<Void, Void> descriptor = MethodDescriptor.create(
        MethodType.UNARY,
        "service/method",
        new TestMarshaller<Void>(),
        new TestMarshaller<Void>());
    final ClientTransport transport = mock(ClientTransport.class);
    final ClientStream stream = mock(ClientStream.class);
    ClientTransportProvider provider = new ClientTransportProvider() {
      @Override
      public ListenableFuture<ClientTransport> get(CallOptions callOptions) {
        return Futures.immediateFuture(transport);
      }
    };
    when(transport.newStream(any(MethodDescriptor.class), any(Metadata.class),
          any(ClientStreamListener.class))).thenReturn(stream);
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        descriptor,
        executor,
        CallOptions.DEFAULT,
        provider,
        deadlineCancellationExecutor)
            .setDecompressorRegistry(decompressorRegistry);

    call.start(new TestClientCallListener<Void>(), new Metadata());

    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);
    verify(transport).newStream(
        eq(descriptor), metadataCaptor.capture(), isA(ClientStreamListener.class));
    Metadata actual = metadataCaptor.getValue();

    Set<String> acceptedEncodings =
        ImmutableSet.copyOf(actual.getAll(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY));
    assertEquals(decompressorRegistry.getAdvertisedMessageEncodings(), acceptedEncodings);
  }

  @Test
  public void prepareHeaders_authorityAdded() {
    Metadata m = new Metadata();
    CallOptions callOptions = CallOptions.DEFAULT.withAuthority("auth");
    ClientCallImpl.prepareHeaders(
        m, callOptions, "user agent", DecompressorRegistry.getDefaultInstance());

    assertEquals(m.get(GrpcUtil.AUTHORITY_KEY), "auth");
  }

  @Test
  public void prepareHeaders_userAgentAdded() {
    Metadata m = new Metadata();
    ClientCallImpl.prepareHeaders(
        m, CallOptions.DEFAULT, "user agent", DecompressorRegistry.getDefaultInstance());

    assertEquals(m.get(GrpcUtil.USER_AGENT_KEY), "user agent");
  }

  @Test
  public void prepareHeaders_messageEncodingAdded() {
    Metadata m = new Metadata();
    CallOptions callOptions = CallOptions.DEFAULT.withCompressor(new Codec.Gzip());
    ClientCallImpl.prepareHeaders(
        m, callOptions, "user agent", DecompressorRegistry.getDefaultInstance());

    assertEquals(m.get(GrpcUtil.MESSAGE_ENCODING_KEY), new Codec.Gzip().getMessageEncoding());
  }

  @Test
  public void prepareHeaders_ignoreIdentityEncoding() {
    Metadata m = new Metadata();
    CallOptions callOptions = CallOptions.DEFAULT.withCompressor(Codec.Identity.NONE);
    ClientCallImpl.prepareHeaders(
        m, callOptions, "user agent", DecompressorRegistry.getDefaultInstance());

    assertNull(m.get(GrpcUtil.MESSAGE_ENCODING_KEY));
  }

  @Test
  public void prepareHeaders_acceptedEncodingsAdded() {
    Metadata m = new Metadata();
    DecompressorRegistry customRegistry = DecompressorRegistry.newEmptyInstance();
    customRegistry.register(new Decompressor() {
      @Override
      public String getMessageEncoding() {
        return "a";
      }

      @Override
      public InputStream decompress(InputStream is) throws IOException {
        return null;
      }
    }, true);
    customRegistry.register(new Decompressor() {
      @Override
      public String getMessageEncoding() {
        return "b";
      }

      @Override
      public InputStream decompress(InputStream is) throws IOException {
        return null;
      }
    }, true);
    customRegistry.register(new Decompressor() {
      @Override
      public String getMessageEncoding() {
        return "c";
      }

      @Override
      public InputStream decompress(InputStream is) throws IOException {
        return null;
      }
    }, false); // not advertised

    ClientCallImpl.prepareHeaders(m, CallOptions.DEFAULT, "user agent", customRegistry);

    Iterable<String> acceptedEncodings =
        Splitter.on(',').split(m.get(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY));

    // Order may be different, since decoder priorities have not yet been implemented.
    assertEquals(ImmutableSet.of("b", "a"), ImmutableSet.copyOf(acceptedEncodings));
  }

  @Test
  public void prepareHeaders_removeReservedHeaders() {
    Metadata m = new Metadata();
    m.put(GrpcUtil.AUTHORITY_KEY, "auth");
    m.put(GrpcUtil.USER_AGENT_KEY, "user agent");
    m.put(GrpcUtil.MESSAGE_ENCODING_KEY, "gzip");
    m.put(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY, "gzip");

    ClientCallImpl.prepareHeaders(
        m, CallOptions.DEFAULT, null, DecompressorRegistry.newEmptyInstance());

    assertNull(m.get(GrpcUtil.AUTHORITY_KEY));
    assertNull(m.get(GrpcUtil.USER_AGENT_KEY));
    assertNull(m.get(GrpcUtil.MESSAGE_ENCODING_KEY));
    assertNull(m.get(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY));
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

