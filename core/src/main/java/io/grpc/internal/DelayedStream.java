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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.common.base.Preconditions;

import io.grpc.Compressor;
import io.grpc.Decompressor;
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

  // set to non null once both listener and realStream are valid.  After this point it is safe
  // to call methods on startedRealStream.  Note: this may be true even after the delayed stream is
  // cancelled.  This should be okay.
  private volatile ClientStream startedRealStream;
  @GuardedBy("this")
  private String authority;
  @GuardedBy("this")
  private ClientStreamListener listener;
  @GuardedBy("this")
  private ClientStream realStream;
  @GuardedBy("this")
  private Status error;

  @GuardedBy("this")
  private final List<PendingMessage> pendingMessages = new LinkedList<PendingMessage>();
  private boolean messageCompressionEnabled;
  @GuardedBy("this")
  private boolean pendingHalfClose;
  @GuardedBy("this")
  private int pendingFlowControlRequests;
  @GuardedBy("this")
  private boolean pendingFlush;
  @GuardedBy("this")
  private Compressor compressor;
  @GuardedBy("this")
  private Decompressor decompressor;

  static final class PendingMessage {
    final InputStream message;
    final boolean shouldBeCompressed;

    public PendingMessage(InputStream message, boolean shouldBeCompressed) {
      this.message = message;
      this.shouldBeCompressed = shouldBeCompressed;
    }
  }

  @Override
  public synchronized void setAuthority(String authority) {
    checkState(listener == null, "must be called before start");
    checkNotNull(authority, "authority");
    if (realStream == null) {
      this.authority = authority;
    } else {
      realStream.setAuthority(authority);
    }
  }

  @Override
  public void start(ClientStreamListener listener) {
    synchronized (this) {
      // start may be called at most once.
      checkState(this.listener == null, "already started");
      this.listener = checkNotNull(listener, "listener");

      // Check error first rather than success.
      if (error != null) {
        listener.closed(error, new Metadata());
      }
      // In the event that an error happened, realStream will be a noop stream.  We still call
      // start stream in order to drain references to pending messages.
      if (realStream != null) {
        startStream();
      }
    }
  }

  @GuardedBy("this")
  private void startStream() {
    checkState(realStream != null, "realStream");
    checkState(listener != null, "listener");
    if (authority != null) {
      realStream.setAuthority(authority);
    }
    realStream.start(listener);

    if (decompressor != null) {
      realStream.setDecompressor(decompressor);
    }
    if (compressor != null) {
      realStream.setCompressor(compressor);
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
    // Ensures visibility.
    startedRealStream = realStream;
  }

  /**
   * Transfers all pending and future requests and mutations to the given stream.
   *
   * <p>No-op if either this method or {@link #cancel} have already been called.
   */
  final void setStream(ClientStream stream) {
    synchronized (this) {
      if (error != null || realStream != null) {
        return;
      }
      realStream = checkNotNull(stream, "stream");
      // listener can only be non-null if start has already been called.
      if (listener != null) {
        startStream();
      }
    }
  }

  @Override
  public void writeMessage(InputStream message) {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          pendingMessages.add(new PendingMessage(message, messageCompressionEnabled));
          return;
        }
      }
    }
    startedRealStream.writeMessage(message);
  }

  @Override
  public void flush() {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          pendingFlush = true;
          return;
        }
      }
    }
    startedRealStream.flush();
  }

  @Override
  public void cancel(Status reason) {
    // At least one of them is null.
    ClientStream streamToBeCancelled = startedRealStream;
    ClientStreamListener listenerToBeCalled = null;
    if (streamToBeCancelled == null) {
      synchronized (this) {
        if (realStream != null) {
          // realStream already set. Just cancel it.
          streamToBeCancelled = realStream;
        } else if (error == null) {
          // Neither realStream and error are set. Will set the error and call the listener if
          // it's set.
          error = checkNotNull(reason);
          realStream = NoopClientStream.INSTANCE;
          if (listener != null) {
            // call startStream anyways to drain pending messages.
            startStream();
            listenerToBeCalled = listener;
          }
        }  // else: error already set, do nothing.
      }
    }
    if (listenerToBeCalled != null) {
      Preconditions.checkState(streamToBeCancelled == null, "unexpected streamToBeCancelled");
      listenerToBeCalled.closed(reason, new Metadata());
    }
    if (streamToBeCancelled != null) {
      streamToBeCancelled.cancel(reason);
    }
  }

  @Override
  public void halfClose() {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          pendingHalfClose = true;
          return;
        }
      }
    }
    startedRealStream.halfClose();
  }

  @Override
  public void request(int numMessages) {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          pendingFlowControlRequests += numMessages;
          return;
        }
      }
    }
    startedRealStream.request(numMessages);
  }

  @Override
  public void setCompressor(Compressor compressor) {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          this.compressor = compressor;
          return;
        }
      }
    }
    startedRealStream.setCompressor(compressor);
  }

  @Override
  public void setDecompressor(Decompressor decompressor) {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          this.decompressor = decompressor;
          return;
        }
      }
    }
    startedRealStream.setDecompressor(decompressor);
  }

  @Override
  public boolean isReady() {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          return false;
        }
      }
    }
    return startedRealStream.isReady();
  }

  @Override
  public void setMessageCompression(boolean enable) {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          messageCompressionEnabled = enable;
          return;
        }
      }
    }
    startedRealStream.setMessageCompression(enable);
  }
}
