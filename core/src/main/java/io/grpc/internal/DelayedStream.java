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

import static com.google.common.base.Preconditions.checkState;

import com.google.common.annotations.VisibleForTesting;

import io.grpc.Compressor;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.Status;

import java.io.InputStream;
import java.util.LinkedList;
import java.util.List;

import javax.annotation.concurrent.GuardedBy;

/**
 * A stream that queues requests before the transport is available, and delegates to a real stream
 * implementation when the transport is available.
 *
 * <p>{@code ClientStream} itself doesn't require thread-safety. However, the state of {@code
 * DelayedStream} may be internally altered by different threads, thus internal synchronization is
 * necessary.
 */
class DelayedStream implements ClientStream {
  private final ClientStreamListener listener;

  private final Object lock = new Object();

  // Volatile to be readable without synchronization in the fast path.
  // Writes are also done within synchronized(this).
  private volatile ClientStream realStream;

  @GuardedBy("lock")
  private Compressor compressor;
  // Can be either a Decompressor or a String
  @GuardedBy("lock")
  private Object decompressor;
  @GuardedBy("lock")
  private DecompressorRegistry decompressionRegistry;
  @GuardedBy("lock")
  private final List<PendingMessage> pendingMessages = new LinkedList<PendingMessage>();
  private boolean messageCompressionEnabled;
  @GuardedBy("lock")
  private boolean pendingHalfClose;
  @GuardedBy("lock")
  private int pendingFlowControlRequests;
  @GuardedBy("lock")
  private boolean pendingFlush;

  static final class PendingMessage {
    final InputStream message;
    final boolean shouldBeCompressed;

    public PendingMessage(InputStream message, boolean shouldBeCompressed) {
      this.message = message;
      this.shouldBeCompressed = shouldBeCompressed;
    }
  }

  DelayedStream(ClientStreamListener listener) {
    this.listener = listener;
  }

  /**
   * Creates a stream on a presumably usable transport.
   */
  void setStream(ClientStream stream) {
    synchronized (lock) {
      if (realStream == NOOP_CLIENT_STREAM) {
        // Already cancelled
        return;
      }
      checkState(realStream == null, "Stream already created: %s", realStream);
      realStream = stream;
      if (compressor != null) {
        realStream.setCompressor(compressor);
      }
      if (this.decompressionRegistry != null) {
        realStream.setDecompressionRegistry(this.decompressionRegistry);
      }
      for (PendingMessage message : pendingMessages) {
        realStream.setMessageCompression(message.shouldBeCompressed);
        realStream.writeMessage(message.message);
      }
      // Set this again, incase no messages were sent.
      realStream.setMessageCompression(messageCompressionEnabled);
      pendingMessages.clear();
      if (pendingHalfClose) {
        realStream.halfClose();
        pendingHalfClose = false;
      }
      if (pendingFlowControlRequests > 0) {
        realStream.request(pendingFlowControlRequests);
        pendingFlowControlRequests = 0;
      }
      if (pendingFlush) {
        realStream.flush();
        pendingFlush = false;
      }
    }
  }

  void maybeClosePrematurely(final Status reason) {
    synchronized (lock) {
      if (realStream == null) {
        realStream = NOOP_CLIENT_STREAM;
        listener.closed(reason, new Metadata());
      }
    }
  }

  @Override
  public void writeMessage(InputStream message) {
    if (realStream == null) {
      synchronized (lock) {
        if (realStream == null) {
          pendingMessages.add(new PendingMessage(message, messageCompressionEnabled));
          return;
        }
      }
    }
    realStream.writeMessage(message);
  }

  @Override
  public void flush() {
    if (realStream == null) {
      synchronized (lock) {
        if (realStream == null) {
          pendingFlush = true;
          return;
        }
      }
    }
    realStream.flush();
  }

  @Override
  public void cancel(Status reason) {
    maybeClosePrematurely(reason);
    realStream.cancel(reason);
  }

  @Override
  public void halfClose() {
    if (realStream == null) {
      synchronized (lock) {
        if (realStream == null) {
          pendingHalfClose = true;
          return;
        }
      }
    }
    realStream.halfClose();
  }

  @Override
  public void request(int numMessages) {
    if (realStream == null) {
      synchronized (lock) {
        if (realStream == null) {
          pendingFlowControlRequests += numMessages;
          return;
        }
      }
    }
    realStream.request(numMessages);
  }

  @Override
  public void setCompressor(Compressor c) {
    synchronized (lock) {
      compressor = c;
      if (realStream != null) {
        realStream.setCompressor(c);
      }
    }
  }

  @Override
  public void setDecompressionRegistry(DecompressorRegistry registry) {
    synchronized (lock) {
      this.decompressionRegistry = registry;
      if (realStream != null) {
        realStream.setDecompressionRegistry(registry);
      }
    }
  }

  @Override
  public boolean isReady() {
    if (realStream == null) {
      synchronized (lock) {
        if (realStream == null) {
          return false;
        }
      }
    }
    return realStream.isReady();
  }

  @Override
  public void setMessageCompression(boolean enable) {
    synchronized (lock) {
      if (realStream != null) {
        realStream.setMessageCompression(enable);
      } else {
        messageCompressionEnabled = enable;
      }
    }
  }

  @VisibleForTesting
  static final ClientStream NOOP_CLIENT_STREAM = new ClientStream() {
    @Override public void writeMessage(InputStream message) {}

    @Override public void flush() {}

    @Override public void cancel(Status reason) {}

    @Override public void halfClose() {}

    @Override public void request(int numMessages) {}

    @Override public void setCompressor(Compressor c) {}

    @Override
    public void setMessageCompression(boolean enable) {
      // noop
    }

    /**
     * Always returns {@code false}, since this is only used when the startup of the {@link
     * ClientCall} fails (i.e. the {@link ClientCall} is closed).
     */
    @Override public boolean isReady() {
      return false;
    }

    @Override
    public void setDecompressionRegistry(DecompressorRegistry registry) {}

    @Override
    public String toString() {
      return "NOOP_CLIENT_STREAM";
    }
  };
}
