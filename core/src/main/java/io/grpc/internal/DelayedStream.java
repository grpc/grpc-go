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

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.Compressor;
import io.grpc.Decompressor;
import io.grpc.Metadata;
import io.grpc.Status;
import java.io.InputStream;
import java.util.ArrayList;
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
  /** {@code true} once realStream is valid and all pending calls have been drained. */
  private volatile boolean passThrough;
  /**
   * Non-{@code null} iff start has been called. Used to assert methods are called in appropriate
   * order, but also used if an error occurrs before {@code realStream} is set.
   */
  private ClientStreamListener listener;
  /** Must hold {@code this} lock when setting. */
  private ClientStream realStream;
  @GuardedBy("this")
  private Status error;
  @GuardedBy("this")
  private List<Runnable> pendingCalls = new ArrayList<Runnable>();
  @GuardedBy("this")
  private DelayedStreamListener delayedListener;

  @Override
  public void setMaxInboundMessageSize(final int maxSize) {
    if (passThrough) {
      realStream.setMaxInboundMessageSize(maxSize);
    } else {
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realStream.setMaxInboundMessageSize(maxSize);
        }
      });
    }
  }

  @Override
  public void setMaxOutboundMessageSize(final int maxSize) {
    if (passThrough) {
      realStream.setMaxOutboundMessageSize(maxSize);
    } else {
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realStream.setMaxOutboundMessageSize(maxSize);
        }
      });
    }
  }

  /**
   * Transfers all pending and future requests and mutations to the given stream.
   *
   * <p>No-op if either this method or {@link #cancel} have already been called.
   */
  // When this method returns, passThrough is guaranteed to be true
  final void setStream(ClientStream stream) {
    synchronized (this) {
      // If realStream != null, then either setStream() or cancel() has been called.
      if (realStream != null) {
        return;
      }
      realStream = checkNotNull(stream, "stream");
    }

    drainPendingCalls();
  }

  /**
   * Called to transition {@code passThrough} to {@code true}. This method is not safe to be called
   * multiple times; the caller must ensure it will only be called once, ever. {@code this} lock
   * should not be held when calling this method.
   */
  private void drainPendingCalls() {
    assert realStream != null;
    assert !passThrough;
    List<Runnable> toRun = new ArrayList<Runnable>();
    DelayedStreamListener delayedListener = null;
    while (true) {
      synchronized (this) {
        if (pendingCalls.isEmpty()) {
          pendingCalls = null;
          passThrough = true;
          delayedListener = this.delayedListener;
          break;
        }
        // Since there were pendingCalls, we need to process them. To maintain ordering we can't set
        // passThrough=true until we run all pendingCalls, but new Runnables may be added after we
        // drop the lock. So we will have to re-check pendingCalls.
        List<Runnable> tmp = toRun;
        toRun = pendingCalls;
        pendingCalls = tmp;
      }
      for (Runnable runnable : toRun) {
        // Must not call transport while lock is held to prevent deadlocks.
        // TODO(ejona): exception handling
        runnable.run();
      }
      toRun.clear();
    }
    if (delayedListener != null) {
      delayedListener.drainPendingCallbacks();
    }
  }

  /**
   * Enqueue the runnable or execute it now. Call sites that may be called many times may want avoid
   * this method if {@code passThrough == true}.
   *
   * <p>Note that this method is no more thread-safe than {@code runnable}. It is thread-safe if and
   * only if {@code runnable} is thread-safe.
   */
  private void delayOrExecute(Runnable runnable) {
    synchronized (this) {
      if (!passThrough) {
        pendingCalls.add(runnable);
        return;
      }
    }
    runnable.run();
  }

  @Override
  public void setAuthority(final String authority) {
    checkState(listener == null, "May only be called before start");
    checkNotNull(authority, "authority");
    delayOrExecute(new Runnable() {
      @Override
      public void run() {
        realStream.setAuthority(authority);
      }
    });
  }

  @Override
  public void start(ClientStreamListener listener) {
    checkState(this.listener == null, "already started");

    Status savedError;
    boolean savedPassThrough;
    synchronized (this) {
      this.listener = checkNotNull(listener, "listener");
      // If error != null, then cancel() has been called and was unable to close the listener
      savedError = error;
      savedPassThrough = passThrough;
      if (!savedPassThrough) {
        listener = delayedListener = new DelayedStreamListener(listener);
      }
    }
    if (savedError != null) {
      listener.closed(savedError, new Metadata());
      return;
    }

    if (savedPassThrough) {
      realStream.start(listener);
    } else {
      final ClientStreamListener finalListener = listener;
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realStream.start(finalListener);
        }
      });
    }
  }

  @Override
  public Attributes getAttributes() {
    checkState(passThrough, "Called getAttributes before attributes are ready");
    return realStream.getAttributes();
  }

  @Override
  public void writeMessage(final InputStream message) {
    checkNotNull(message, "message");
    if (passThrough) {
      realStream.writeMessage(message);
    } else {
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realStream.writeMessage(message);
        }
      });
    }
  }

  @Override
  public void flush() {
    if (passThrough) {
      realStream.flush();
    } else {
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realStream.flush();
        }
      });
    }
  }

  // When this method returns, passThrough is guaranteed to be true
  @Override
  public void cancel(final Status reason) {
    checkNotNull(reason, "reason");
    boolean delegateToRealStream = true;
    ClientStreamListener listenerToClose = null;
    synchronized (this) {
      // If realStream != null, then either setStream() or cancel() has been called
      if (realStream == null) {
        realStream = NoopClientStream.INSTANCE;
        delegateToRealStream = false;

        // If listener == null, then start() will later call listener with 'error'
        listenerToClose = listener;
        error = reason;
      }
    }
    if (delegateToRealStream) {
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realStream.cancel(reason);
        }
      });
    } else {
      if (listenerToClose != null) {
        listenerToClose.closed(reason, new Metadata());
      }
      drainPendingCalls();
    }
  }

  @Override
  public void halfClose() {
    delayOrExecute(new Runnable() {
      @Override
      public void run() {
        realStream.halfClose();
      }
    });
  }

  @Override
  public void request(final int numMessages) {
    if (passThrough) {
      realStream.request(numMessages);
    } else {
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realStream.request(numMessages);
        }
      });
    }
  }

  @Override
  public void setCompressor(final Compressor compressor) {
    checkNotNull(compressor, "compressor");
    delayOrExecute(new Runnable() {
      @Override
      public void run() {
        realStream.setCompressor(compressor);
      }
    });
  }

  @Override
  public void setDecompressor(Decompressor decompressor) {
    checkNotNull(decompressor, "decompressor");
    // This method being called only makes sense after setStream() has been called (but not
    // necessarily returned), but there is not necessarily a happens-before relationship. This
    // synchronized block creates one.
    synchronized (this) { }
    checkState(realStream != null, "How did we receive a reply before the request is sent?");
    // ClientStreamListenerImpl (in ClientCallImpl) requires setDecompressor to be set immediately,
    // since messages may be processed immediately after this method returns.
    realStream.setDecompressor(decompressor);
  }

  @Override
  public boolean isReady() {
    if (passThrough) {
      return realStream.isReady();
    } else {
      return false;
    }
  }

  @Override
  public void setMessageCompression(final boolean enable) {
    if (passThrough) {
      realStream.setMessageCompression(enable);
    } else {
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realStream.setMessageCompression(enable);
        }
      });
    }
  }

  @VisibleForTesting
  ClientStream getRealStream() {
    return realStream;
  }

  private static class DelayedStreamListener implements ClientStreamListener {
    private final ClientStreamListener realListener;
    private volatile boolean passThrough;
    @GuardedBy("this")
    private List<Runnable> pendingCallbacks = new ArrayList<Runnable>();

    public DelayedStreamListener(ClientStreamListener listener) {
      this.realListener = listener;
    }

    private void delayOrExecute(Runnable runnable) {
      synchronized (this) {
        if (!passThrough) {
          pendingCallbacks.add(runnable);
          return;
        }
      }
      runnable.run();
    }

    @Override
    public void messageRead(final InputStream message) {
      if (passThrough) {
        realListener.messageRead(message);
      } else {
        delayOrExecute(new Runnable() {
          @Override
          public void run() {
            realListener.messageRead(message);
          }
        });
      }
    }

    @Override
    public void onReady() {
      if (passThrough) {
        realListener.onReady();
      } else {
        delayOrExecute(new Runnable() {
          @Override
          public void run() {
            realListener.onReady();
          }
        });
      }
    }

    @Override
    public void headersRead(final Metadata headers) {
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realListener.headersRead(headers);
        }
      });
    }

    @Override
    public void closed(final Status status, final Metadata trailers) {
      delayOrExecute(new Runnable() {
        @Override
        public void run() {
          realListener.closed(status, trailers);
        }
      });
    }

    public void drainPendingCallbacks() {
      assert !passThrough;
      List<Runnable> toRun = new ArrayList<Runnable>();
      while (true) {
        synchronized (this) {
          if (pendingCallbacks.isEmpty()) {
            pendingCallbacks = null;
            passThrough = true;
            break;
          }
          // Since there were pendingCallbacks, we need to process them. To maintain ordering we
          // can't set passThrough=true until we run all pendingCallbacks, but new Runnables may be
          // added after we drop the lock. So we will have to re-check pendingCallbacks.
          List<Runnable> tmp = toRun;
          toRun = pendingCallbacks;
          pendingCallbacks = tmp;
        }
        for (Runnable runnable : toRun) {
          // Avoid calling listener while lock is held to prevent deadlocks.
          // TODO(ejona): exception handling
          runnable.run();
        }
        toRun.clear();
      }
    }
  }
}
