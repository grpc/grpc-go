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

import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

import io.grpc.Codec;
import io.grpc.DecompressorRegistry;
import io.grpc.IntegerMarshaller;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;

import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.ByteArrayInputStream;
import java.io.InputStream;

/**
 * Tests for {@link DelayedStream}.  Most of the state checking is enforced by
 * {@link ClientCallImpl} so we don't check it here.
 */
@RunWith(JUnit4.class)
public class DelayedStreamTest {
  @Rule public final ExpectedException thrown = ExpectedException.none();

  @Mock private ClientStreamListener listener;
  @Mock private ClientTransport transport;
  @Mock private ClientStream realStream;
  @Captor private ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
  private DelayedStream stream;
  private Metadata headers = new Metadata();

  private MethodDescriptor<Integer, Integer> method = MethodDescriptor.create(
      MethodType.UNARY, "service/method", new IntegerMarshaller(), new IntegerMarshaller());

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    stream = new DelayedStream(listener);
  }

  @Test
  public void setStream_sendsAllMessages() {
    stream.setCompressor(Codec.Identity.NONE);

    DecompressorRegistry registry = DecompressorRegistry.newEmptyInstance();
    stream.setDecompressionRegistry(registry);

    stream.setMessageCompression(true);
    InputStream message = new ByteArrayInputStream(new byte[]{'a'});
    stream.writeMessage(message);
    stream.setMessageCompression(false);
    stream.writeMessage(message);

    stream.setStream(realStream);


    verify(realStream).setCompressor(Codec.Identity.NONE);
    verify(realStream).setDecompressionRegistry(registry);

    // Verify that the order was correct, even though they should be interleaved with the
    // writeMessage calls
    verify(realStream).setMessageCompression(true);
    verify(realStream, times(2)).setMessageCompression(false);

    verify(realStream, times(2)).writeMessage(message);
  }

  @Test
  public void setStream_halfClose() {
    stream.halfClose();
    stream.setStream(realStream);

    verify(realStream).halfClose();
  }

  @Test
  public void setStream_flush() {
    stream.flush();
    stream.setStream(realStream);

    verify(realStream).flush();
  }

  @Test
  public void setStream_flowControl() {
    stream.request(1);
    stream.request(2);

    stream.setStream(realStream);

    verify(realStream).request(3);
  }

  @Test
  public void setStream_cantCreateTwice() {
    // The first call will be a success
    stream.setStream(realStream);

    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("Stream already created");

    stream.setStream(realStream);
  }

  @Test
  public void streamCancelled() {
    stream.cancel(Status.CANCELLED);

    // Should be a no op, and not fail due to transport not returning a newStream
    stream.setStream(realStream);

    verify(listener).closed(eq(Status.CANCELLED), isA(Metadata.class));
  }
}
