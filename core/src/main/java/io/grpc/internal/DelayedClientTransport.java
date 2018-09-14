/*
 * Copyright 2015 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.internal;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.CallOptions;
import io.grpc.Context;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalLogId;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.LinkedHashSet;
import java.util.concurrent.Executor;
import javax.annotation.Nonnull;
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
  private final InternalLogId lodId = InternalLogId.allocate(getClass().getName());

  private final Object lock = new Object();

  private final Executor defaultAppExecutor;
  private final ChannelExecutor channelExecutor;

  private Runnable reportTransportInUse;
  private Runnable reportTransportNotInUse;
  private Runnable reportTransportTerminated;
  private Listener listener;

  @Nonnull
  @GuardedBy("lock")
  private Collection<PendingStream> pendingStreams = new LinkedHashSet<PendingStream>();

  /**
   * When {@code shutdownStatus != null && !hasPendingStreams()}, then the transport is considered
   * terminated.
   */
  @GuardedBy("lock")
  private Status shutdownStatus;

  /**
   * The last picker that {@link #reprocess} has used. May be set to null when the channel has moved
   * to idle.
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
    this.listener = listener;
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
      SubchannelPicker picker;
      PickSubchannelArgs args = new PickSubchannelArgsImpl(method, headers, callOptions);
      long pickerVersion = -1;
      synchronized (lock) {
        if (shutdownStatus == null) {
          if (lastPicker == null) {
            return createPendingStream(args);
          }
          picker = lastPicker;
          pickerVersion = lastPickerVersion;
        } else {
          return new FailingClientStream(shutdownStatus);
        }
      }
      while (true) {
        PickResult pickResult = picker.pickSubchannel(args);
        ClientTransport transport = GrpcUtil.getTransportFromPickResult(pickResult,
            callOptions.isWaitForReady());
        if (transport != null) {
          return transport.newStream(
              args.getMethodDescriptor(), args.getHeaders(), args.getCallOptions());
        }
        // This picker's conclusion is "buffer".  If there hasn't been a newer picker set (possible
        // race with reprocess()), we will buffer it.  Otherwise, will try with the new picker.
        synchronized (lock) {
          if (shutdownStatus != null) {
            return new FailingClientStream(shutdownStatus);
          }
          if (pickerVersion == lastPickerVersion) {
            return createPendingStream(args);
          }
          picker = lastPicker;
          pickerVersion = lastPickerVersion;
        }
      }
    } finally {
      channelExecutor.drain();
    }
  }

  /**
   * Caller must call {@code channelExecutor.drain()} outside of lock because this method may
   * schedule tasks on channelExecutor.
   */
  @GuardedBy("lock")
  private PendingStream createPendingStream(PickSubchannelArgs args) {
    PendingStream pendingStream = new PendingStream(args);
    pendingStreams.add(pendingStream);
    if (getPendingStreamsCount() == 1) {
      channelExecutor.executeLater(reportTransportInUse);
    }
    return pendingStream;
  }

  @Override
  public final void ping(final PingCallback callback, Executor executor) {
    throw new UnsupportedOperationException("This method is not expected to be called");
  }

  @Override
  public ListenableFuture<SocketStats> getStats() {
    SettableFuture<SocketStats> ret = SettableFuture.create();
    ret.set(null);
    return ret;
  }

  /**
   * Prevents creating any new streams.  Buffered streams are not failed and may still proceed
   * when {@link #reprocess} is called.  The delayed transport will be terminated when there is no
   * more buffered streams.
   */
  @Override
  public final void shutdown(final Status status) {
    synchronized (lock) {
      if (shutdownStatus != null) {
        return;
      }
      shutdownStatus = status;
      channelExecutor.executeLater(new Runnable() {
          @Override
          public void run() {
            listener.transportShutdown(status);
          }
        });
      if (!hasPendingStreams() && reportTransportTerminated != null) {
        channelExecutor.executeLater(reportTransportTerminated);
        reportTransportTerminated = null;
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
    shutdown(status);
    Collection<PendingStream> savedPendingStreams;
    Runnable savedReportTransportTerminated;
    synchronized (lock) {
      savedPendingStreams = pendingStreams;
      savedReportTransportTerminated = reportTransportTerminated;
      reportTransportTerminated = null;
      if (!pendingStreams.isEmpty()) {
        pendingStreams = Collections.<PendingStream>emptyList();
      }
    }
    if (savedReportTransportTerminated != null) {
      for (PendingStream stream : savedPendingStreams) {
        stream.cancel(status);
      }
      channelExecutor.executeLater(savedReportTransportTerminated).drain();
    }
    // If savedReportTransportTerminated == null, transportTerminated() has already been called in
    // shutdown().
  }

  public final boolean hasPendingStreams() {
    synchronized (lock) {
      return !pendingStreams.isEmpty();
    }
  }

  @VisibleForTesting
  final int getPendingStreamsCount() {
    synchronized (lock) {
      return pendingStreams.size();
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
  final void reprocess(@Nullable SubchannelPicker picker) {
    ArrayList<PendingStream> toProcess;
    synchronized (lock) {
      lastPicker = picker;
      lastPickerVersion++;
      if (picker == null || !hasPendingStreams()) {
        return;
      }
      toProcess = new ArrayList<>(pendingStreams);
    }
    ArrayList<PendingStream> toRemove = new ArrayList<>();

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
      if (!hasPendingStreams()) {
        return;
      }
      pendingStreams.removeAll(toRemove);
      // Because delayed transport is long-lived, we take this opportunity to down-size the
      // hashmap.
      if (pendingStreams.isEmpty()) {
        pendingStreams = new LinkedHashSet<PendingStream>();
      }
      if (!hasPendingStreams()) {
        // There may be a brief gap between delayed transport clearing in-use state, and first real
        // transport starting streams and setting in-use state.  During the gap the whole channel's
        // in-use state may be false. However, it shouldn't cause spurious switching to idleness
        // (which would shutdown the transports and LoadBalancer) because the gap should be shorter
        // than IDLE_MODE_DEFAULT_TIMEOUT_MILLIS (1 second).
        channelExecutor.executeLater(reportTransportNotInUse);
        if (shutdownStatus != null && reportTransportTerminated != null) {
          channelExecutor.executeLater(reportTransportTerminated);
          reportTransportTerminated = null;
        }
      }
    }
    channelExecutor.drain();
  }

  // TODO(carl-mastrangelo): remove this once the Subchannel change is in.
  @Override
  public InternalLogId getLogId() {
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
        if (reportTransportTerminated != null) {
          boolean justRemovedAnElement = pendingStreams.remove(this);
          if (!hasPendingStreams() && justRemovedAnElement) {
            channelExecutor.executeLater(reportTransportNotInUse);
            if (shutdownStatus != null) {
              channelExecutor.executeLater(reportTransportTerminated);
              reportTransportTerminated = null;
            }
          }
        }
      }
      channelExecutor.drain();
    }
  }
}
