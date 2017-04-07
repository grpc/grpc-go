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

import com.google.common.annotations.VisibleForTesting;
import io.grpc.CallOptions;
import io.grpc.Context;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashSet;
import java.util.concurrent.Executor;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * A client transport that queues requests before a real transport is available. When {@link
 * #reprocess} is called, this class applies the provided {@link SubchannelPicker} to pick a
 * transport for each pending stream.
 *
 * <p>This transport owns every stream that it has created until a real transport has been picked
 * for that stream, at which point the ownership of the stream is transferred to the real transport,
 * thus the delayed transport stops owning the stream.
 */
final class DelayedClientTransport implements ManagedClientTransport {
  private final LogId lodId = LogId.allocate(getClass().getName());

  private final Object lock = new Object();

  private final Executor defaultAppExecutor;
  private final ChannelExecutor channelExecutor;

  private Runnable reportTransportInUse;
  private Runnable reportTransportNotInUse;
  private Runnable reportTransportShutdown;
  private Runnable reportTransportTerminated;

  @GuardedBy("lock")
  private Collection<PendingStream> pendingStreams = new LinkedHashSet<PendingStream>();

  /**
   * When shutdown == true and pendingStreams == null, then the transport is considered terminated.
   */
  @GuardedBy("lock")
  private boolean shutdown;

  /**
   * The last picker that {@link #reprocess} has used.
   */
  @GuardedBy("lock")
  @Nullable
  private SubchannelPicker lastPicker;

  @GuardedBy("lock")
  private long lastPickerVersion;

  /**
   * Creates a new delayed transport.
   *
   * @param defaultAppExecutor pending streams will create real streams and run bufferred operations
   *        in an application executor, which will be this executor, unless there is on provided in
   *        {@link CallOptions}.
   * @param channelExecutor all listener callbacks of the delayed transport will be run from this
   *        ChannelExecutor.
   */
  DelayedClientTransport(Executor defaultAppExecutor, ChannelExecutor channelExecutor) {
    this.defaultAppExecutor = defaultAppExecutor;
    this.channelExecutor = channelExecutor;
  }

  @Override
  public final Runnable start(final Listener listener) {
    reportTransportInUse = new Runnable() {
        @Override
        public void run() {
          listener.transportInUse(true);
        }
      };
    reportTransportNotInUse = new Runnable() {
        @Override
        public void run() {
          listener.transportInUse(false);
        }
      };
    reportTransportShutdown = new Runnable() {
        @Override
        public void run() {
          listener.transportShutdown(
              Status.UNAVAILABLE.withDescription("Channel requested transport to shut down"));
        }
      };
    reportTransportTerminated = new Runnable() {
        @Override
        public void run() {
          listener.transportTerminated();
        }
      };
    return null;
  }

  /**
   * If a {@link SubchannelPicker} is being, or has been provided via {@link #reprocess}, the last
   * picker will be consulted.
   *
   * <p>Otherwise, if the delayed transport is not shutdown, then a {@link PendingStream} is
   * returned; if the transport is shutdown, then a {@link FailingClientStream} is returned.
   */
  @Override
  public final ClientStream newStream(
      MethodDescriptor<?, ?> method, Metadata headers, CallOptions callOptions) {
    try {
      SubchannelPicker picker = null;
      PickSubchannelArgs args = new PickSubchannelArgsImpl(method, headers, callOptions);
      long pickerVersion = -1;
      synchronized (lock) {
        if (!shutdown) {
          if (lastPicker == null) {
            return createPendingStream(args);
          }
          picker = lastPicker;
          pickerVersion = lastPickerVersion;
        }
      }
      if (picker != null) {
        while (true) {
          PickResult pickResult = picker.pickSubchannel(args);
          ClientTransport transport = GrpcUtil.getTransportFromPickResult(pickResult,
              callOptions.isWaitForReady());
          if (transport != null) {
            return transport.newStream(
                args.getMethodDescriptor(), args.getHeaders(), args.getCallOptions());
          }
          // This picker's conclusion is "buffer".  If there hasn't been a newer picker set
          // (possible race with reprocess()), we will buffer it.  Otherwise, will try with the new
          // picker.
          synchronized (lock) {
            if (shutdown) {
              break;
            }
            if (pickerVersion == lastPickerVersion) {
              return createPendingStream(args);
            }
            picker = lastPicker;
            pickerVersion = lastPickerVersion;
          }
        }
      }
      return new FailingClientStream(Status.UNAVAILABLE.withDescription(
              "Channel has shutdown (reported by delayed transport)"));
    } finally {
      channelExecutor.drain();
    }
  }

  @Override
  public final ClientStream newStream(MethodDescriptor<?, ?> method, Metadata headers) {
    return newStream(method, headers, CallOptions.DEFAULT);
  }

  /**
   * Caller must call {@code channelExecutor.drain()} outside of lock because this method may
   * schedule tasks on channelExecutor.
   */
  @GuardedBy("lock")
  private PendingStream createPendingStream(PickSubchannelArgs args) {
    PendingStream pendingStream = new PendingStream(args);
    pendingStreams.add(pendingStream);
    if (pendingStreams.size() == 1) {
      channelExecutor.executeLater(reportTransportInUse);
    }
    return pendingStream;
  }

  @Override
  public final void ping(final PingCallback callback, Executor executor) {
    throw new UnsupportedOperationException("This method is not expected to be called");
  }

  /**
   * Prevents creating any new streams.  Buffered streams are not failed and may still proceed
   * when {@link #reprocess} is called.  The delayed transport will be terminated when there is no
   * more buffered streams.
   */
  @Override
  public final void shutdown() {
    synchronized (lock) {
      if (shutdown) {
        return;
      }
      shutdown = true;
      channelExecutor.executeLater(reportTransportShutdown);
      if (pendingStreams == null || pendingStreams.isEmpty()) {
        pendingStreams = null;
        channelExecutor.executeLater(reportTransportTerminated);
      }
    }
    channelExecutor.drain();
  }

  /**
   * Shuts down this transport and cancels all streams that it owns, hence immediately terminates
   * this transport.
   */
  @Override
  public final void shutdownNow(Status status) {
    shutdown();
    Collection<PendingStream> savedPendingStreams = null;
    synchronized (lock) {
      if (pendingStreams != null) {
        savedPendingStreams = pendingStreams;
        pendingStreams = null;
      }
    }
    if (savedPendingStreams != null) {
      for (PendingStream stream : savedPendingStreams) {
        stream.cancel(status);
      }
      channelExecutor.executeLater(reportTransportTerminated).drain();
    }
    // If savedPendingStreams == null, transportTerminated() has already been called in shutdown().
  }

  public final boolean hasPendingStreams() {
    synchronized (lock) {
      return pendingStreams != null && !pendingStreams.isEmpty();
    }
  }

  @VisibleForTesting
  final int getPendingStreamsCount() {
    synchronized (lock) {
      return pendingStreams == null ? 0 : pendingStreams.size();
    }
  }

  /**
   * Use the picker to try picking a transport for every pending stream, proceed the stream if the
   * pick is successful, otherwise keep it pending.
   *
   * <p>This method may be called concurrently with {@code newStream()}, and it's safe.  All pending
   * streams will be served by the latest picker (if a same picker is given more than once, they are
   * considered different pickers) as soon as possible.
   *
   * <p>This method <strong>must not</strong> be called concurrently with itself.
   */
  final void reprocess(SubchannelPicker picker) {
    ArrayList<PendingStream> toProcess;
    ArrayList<PendingStream> toRemove = new ArrayList<PendingStream>();
    synchronized (lock) {
      lastPicker = picker;
      lastPickerVersion++;
      if (pendingStreams == null || pendingStreams.isEmpty()) {
        return;
      }
      toProcess = new ArrayList<PendingStream>(pendingStreams);
    }

    for (final PendingStream stream : toProcess) {
      PickResult pickResult = picker.pickSubchannel(stream.args);
      CallOptions callOptions = stream.args.getCallOptions();
      final ClientTransport transport = GrpcUtil.getTransportFromPickResult(pickResult,
          callOptions.isWaitForReady());
      if (transport != null) {
        Executor executor = defaultAppExecutor;
        // createRealStream may be expensive. It will start real streams on the transport. If
        // there are pending requests, they will be serialized too, which may be expensive. Since
        // we are now on transport thread, we need to offload the work to an executor.
        if (callOptions.getExecutor() != null) {
          executor = callOptions.getExecutor();
        }
        executor.execute(new Runnable() {
            @Override
            public void run() {
              stream.createRealStream(transport);
            }
          });
        toRemove.add(stream);
      }  // else: stay pending
    }

    synchronized (lock) {
      // Between this synchronized and the previous one:
      //   - Streams may have been cancelled, which may turn pendingStreams into emptiness.
      //   - shutdown() may be called, which may turn pendingStreams into null.
      if (pendingStreams == null || pendingStreams.isEmpty()) {
        return;
      }
      pendingStreams.removeAll(toRemove);
      if (pendingStreams.isEmpty()) {
        // There may be a brief gap between delayed transport clearing in-use state, and first real
        // transport starting streams and setting in-use state.  During the gap the whole channel's
        // in-use state may be false. However, it shouldn't cause spurious switching to idleness
        // (which would shutdown the transports and LoadBalancer) because the gap should be shorter
        // than IDLE_MODE_DEFAULT_TIMEOUT_MILLIS (1 second).
        channelExecutor.executeLater(reportTransportNotInUse);
        if (shutdown) {
          pendingStreams = null;
          channelExecutor.executeLater(reportTransportTerminated);
        } else {
          // Because delayed transport is long-lived, we take this opportunity to down-size the
          // hashmap.
          pendingStreams = new LinkedHashSet<PendingStream>();
        }
      }
    }
    channelExecutor.drain();
  }

  // TODO(carl-mastrangelo): remove this once the Subchannel change is in.
  @Override
  public LogId getLogId() {
    return lodId;
  }

  private class PendingStream extends DelayedStream {
    private final PickSubchannelArgs args;
    private final Context context = Context.current();

    private PendingStream(PickSubchannelArgs args) {
      this.args = args;
    }

    private void createRealStream(ClientTransport transport) {
      ClientStream realStream;
      Context origContext = context.attach();
      try {
        realStream = transport.newStream(
            args.getMethodDescriptor(), args.getHeaders(), args.getCallOptions());
      } finally {
        context.detach(origContext);
      }
      setStream(realStream);
    }

    @Override
    public void cancel(Status reason) {
      super.cancel(reason);
      synchronized (lock) {
        if (pendingStreams != null) {
          boolean justRemovedAnElement = pendingStreams.remove(this);
          if (pendingStreams.isEmpty() && justRemovedAnElement) {
            channelExecutor.executeLater(reportTransportNotInUse);
            if (shutdown) {
              pendingStreams = null;
              channelExecutor.executeLater(reportTransportTerminated);
            }
          }
        }
      }
      channelExecutor.drain();
    }
  }
}
