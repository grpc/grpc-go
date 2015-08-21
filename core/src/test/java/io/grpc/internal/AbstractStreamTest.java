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

import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static org.junit.Assert.fail;
import static org.mockito.Mockito.verify;

import com.google.common.collect.ImmutableMultimap;
import com.google.common.collect.Multimap;

import io.grpc.internal.AbstractStream.Phase;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.InputStream;

import javax.annotation.Nullable;

@RunWith(JUnit4.class)
public class AbstractStreamTest {
  @Mock private StreamListener streamListener;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
  }

  @Test
  public void onStreamAllocated_shouldNotifyReady() {
    AbstractStream<Object> stream = new AbstractStreamBase<Object>(null);

    stream.onStreamAllocated();

    verify(streamListener).onReady();
  }

  @Test
  public void validPhaseTransitions() {
    AbstractStream<Object> stream = new AbstractStreamBase<Object>(null);
    Multimap<Phase, Phase> validTransitions = ImmutableMultimap.<Phase, Phase>builder()
        .put(Phase.HEADERS, Phase.HEADERS)
        .put(Phase.HEADERS, Phase.MESSAGE)
        .put(Phase.HEADERS, Phase.STATUS)
        .put(Phase.MESSAGE, Phase.MESSAGE)
        .put(Phase.MESSAGE, Phase.STATUS)
        .put(Phase.STATUS, Phase.STATUS)
        .build();

    for (Phase startPhase : Phase.values()) {
      for (Phase endPhase : Phase.values()) {
        if (validTransitions.containsEntry(startPhase, endPhase)) {
          stream.verifyNextPhase(startPhase, endPhase);
        } else {
          try {
            stream.verifyNextPhase(startPhase, endPhase);
            fail();
          } catch (IllegalStateException expected) {
            // continue
          }
        }
      }
    }
  }

  /**
   * Base class for testing.
   */
  private class AbstractStreamBase<IdT> extends AbstractStream<IdT> {
    private AbstractStreamBase(WritableBufferAllocator bufferAllocator) {
      super(bufferAllocator, DEFAULT_MAX_MESSAGE_SIZE);
    }

    @Override
    public void request(int numMessages) {
      throw new UnsupportedOperationException();
    }

    @Override
    @Nullable
    public IdT id() {
      throw new UnsupportedOperationException();
    }

    @Override
    protected StreamListener listener() {
      return streamListener;
    }

    @Override
    protected void internalSendFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void receiveMessage(InputStream is) {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void inboundDeliveryPaused() {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void remoteEndClosed() {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void returnProcessedBytes(int processedBytes) {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void deframeFailed(Throwable cause) {
      throw new UnsupportedOperationException();
    }
  }
}

