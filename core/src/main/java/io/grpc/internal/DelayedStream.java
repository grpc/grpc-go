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

import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
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

  // Volatile to be readable without synchronization in the fast path.
  // Writes are also done within synchronized(this).
  private volatile ClientStream realStream;

  @GuardedBy("this")
  private Compressor compressor;

  @GuardedBy("this")
  private Iterable<String> compressionMessageEncodings;
  // Can be either a Decompressor or a String
  @GuardedBy("this")
  private Object decompressor;
  @GuardedBy("this")
  private DecompressorRegistry decompressionRegistry;
  @GuardedBy("this")
  private final List<PendingMessage> pendingMessages = new LinkedList<PendingMessage>();
  private boolean messageCompressionEnabled;
  @GuardedBy("this")
  private boolean pendingHalfClose;
  @GuardedBy("this")
  private int pendingFlowControlRequests;
  @GuardedBy("this")
  private boolean pendingFlush;
  @GuardedBy("lock")
  private CompressorRegistry compressionRegistry;

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
   * Creates a stream on a presumably usable transport. Must not be called if {@link
   * #cancelledPrematurely}, as there is no way to close {@code stream} without double-calling
   * {@code listener}. Most callers of this method will need to acquire the intrinsic lock to check
   * {@code cancelledPrematurely} and this method atomically.
   */
  void setStream(ClientStream stream) {
    synchronized (this) {
      if (cancelledPrematurely()) {
        // Already cancelled
        throw new IllegalStateException("Can't set on cancelled stream");
      }
      checkState(realStream == null, "Stream already created: %s", realStream);
      realStream = stream;
      if (compressionMessageEncodings != null) {
        realStream.pickCompressor(compressionMessageEncodings);
      }
      if (this.decompressionRegistry != null) {
        realStream.setDecompressionRegistry(this.decompressionRegistry);
      }
      if (this.compressionRegistry != null) {
        realStream.setCompressionRegistry(this.compressionRegistry);
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
    synchronized (this) {
      if (realStream == null) {
        realStream = NoopClientStream.INSTANCE;
        listener.closed(reason, new Metadata());
      }
    }
  }

  public boolean cancelledPrematurely() {
    synchronized (this) {
      return realStream == NoopClientStream.INSTANCE;
    }
  }

  @Override
  public void writeMessage(InputStream message) {
    if (realStream == null) {
      synchronized (this) {
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
      synchronized (this) {
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
      synchronized (this) {
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
      synchronized (this) {
        if (realStream == null) {
          pendingFlowControlRequests += numMessages;
          return;
        }
      }
    }
    realStream.request(numMessages);
  }

  @Override
  public Compressor pickCompressor(Iterable<String> messageEncodings) {
    synchronized (this) {
      compressionMessageEncodings = messageEncodings;
      if (realStream != null) {
        return realStream.pickCompressor(messageEncodings);
      }
    }
    // ClientCall never uses this.  Since the stream doesn't exist yet, it can't say what
    // stream it would pick.  Eventually this will need a cleaner solution.
    // TODO(carl-mastrangelo): Remove this.
    return null;
  }

  @Override
  public void setCompressionRegistry(CompressorRegistry registry) {
    synchronized (this) {
      this.compressionRegistry = registry;
      if (realStream != null) {
        realStream.setCompressionRegistry(registry);
      }
    }
  }

  @Override
  public void setDecompressionRegistry(DecompressorRegistry registry) {
    synchronized (this) {
      this.decompressionRegistry = registry;
      if (realStream != null) {
        realStream.setDecompressionRegistry(registry);
      }
    }
  }

  @Override
  public boolean isReady() {
    if (realStream == null) {
      synchronized (this) {
        if (realStream == null) {
          return false;
        }
      }
    }
    return realStream.isReady();
  }

  @Override
  public void setMessageCompression(boolean enable) {
    synchronized (this) {
      if (realStream != null) {
        realStream.setMessageCompression(enable);
      } else {
        messageCompressionEnabled = enable;
      }
    }
  }
}
