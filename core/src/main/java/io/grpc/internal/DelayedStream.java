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

  // set to non null once both listener and realStream are valid.  After this point it is safe
  // to call methods on startedRealStream.  Note: this may be true even after the delayed stream is
  // cancelled.  This should be okay.
  private volatile ClientStream startedRealStream;
  @GuardedBy("this")
  private ClientStreamListener listener;
  @GuardedBy("this")
  private ClientStream realStream;
  @GuardedBy("this")
  private Status error;

  @GuardedBy("this")
  private Iterable<String> compressionMessageEncodings;
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
  @GuardedBy("this")
  private CompressorRegistry compressionRegistry;

  static final class PendingMessage {
    final InputStream message;
    final boolean shouldBeCompressed;

    public PendingMessage(InputStream message, boolean shouldBeCompressed) {
      this.message = message;
      this.shouldBeCompressed = shouldBeCompressed;
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
    realStream.start(listener);

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
    // Ensures visibility.
    startedRealStream = realStream;
  }

  void setStream(ClientStream stream) {
    synchronized (this) {
      if (error != null) {
        // If there is an error, unstartedStream will be a Noop.
        return;
      }
      checkState(realStream == null, "Stream already created: %s", realStream);
      realStream = checkNotNull(stream, "stream");
      // listener can only be non-null if start has already been called.
      if (listener != null) {
        startStream();
      }
    }
  }

  void setError(Status reason) {
    synchronized (this) {
      // If the client has already cancelled the stream don't bother keeping the next error.
      if (error == null) {
        error = checkNotNull(reason);
        realStream = NoopClientStream.INSTANCE;
        if (listener != null) {
          listener.closed(error, new Metadata());
          // call startStream anyways to drain pending messages.
          startStream();
        }
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
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          setError(reason);
          return;
        }
      }
    }
    startedRealStream.cancel(reason);
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
  public Compressor pickCompressor(Iterable<String> messageEncodings) {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          compressionMessageEncodings = messageEncodings;
          // ClientCall never uses this.  Since the stream doesn't exist yet, it can't say what
          // stream it would pick.  Eventually this will need a cleaner solution.
          // TODO(carl-mastrangelo): Remove this.
          return null;
        }
      }
    }
    return startedRealStream.pickCompressor(messageEncodings);
  }

  @Override
  public void setCompressionRegistry(CompressorRegistry registry) {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          compressionRegistry = registry;
          return;
        }
      }
    }
    startedRealStream.setCompressionRegistry(registry);
  }

  @Override
  public void setDecompressionRegistry(DecompressorRegistry registry) {
    if (startedRealStream == null) {
      synchronized (this) {
        if (startedRealStream == null) {
          decompressionRegistry = registry;
          return;
        }
      }
    }
    startedRealStream.setDecompressionRegistry(registry);
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
