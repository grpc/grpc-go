/*
 * Copyright 2017 The gRPC Authors
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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Objects;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.ClientStreamTracer;
import io.grpc.Compressor;
import io.grpc.Deadline;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.List;
import java.util.Random;
import java.util.concurrent.Executor;
import java.util.concurrent.Future;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/** A logical {@link ClientStream} that is retriable. */
abstract class RetriableStream<ReqT> implements ClientStream {
  @VisibleForTesting
  static final Metadata.Key<String> GRPC_PREVIOUS_RPC_ATTEMPTS =
      Metadata.Key.of("grpc-previous-rpc-attempts", Metadata.ASCII_STRING_MARSHALLER);

  @VisibleForTesting
  static final Metadata.Key<String> GRPC_RETRY_PUSHBACK_MS =
      Metadata.Key.of("grpc-retry-pushback-ms", Metadata.ASCII_STRING_MARSHALLER);

  private static final Status CANCELLED_BECAUSE_COMMITTED =
      Status.CANCELLED.withDescription("Stream thrown away because RetriableStream committed");

  private final MethodDescriptor<ReqT, ?> method;
  private final Executor callExecutor;
  private final ScheduledExecutorService scheduledExecutorService;
  // Must not modify it.
  private final Metadata headers;
  private final RetryPolicy.Provider retryPolicyProvider;
  private final HedgingPolicy.Provider hedgingPolicyProvider;
  private RetryPolicy retryPolicy;

  /** Must be held when updating state, accessing state.buffer, or certain substream attributes. */
  private final Object lock = new Object();

  private final ChannelBufferMeter channelBufferUsed;
  private final long perRpcBufferLimit;
  private final long channelBufferLimit;
  @Nullable
  private final Throttle throttle;

  private volatile State state = new State(
      new ArrayList<BufferEntry>(8), Collections.<Substream>emptyList(), null, false, false);

  /**
   * Either transparent retry happened or reached server's application logic.
   */
  private boolean noMoreTransparentRetry;

  // Used for recording the share of buffer used for the current call out of the channel buffer.
  // This field would not be necessary if there is no channel buffer limit.
  @GuardedBy("lock")
  private long perRpcBufferUsed;

  private ClientStreamListener masterListener;
  private Future<?> scheduledRetry;
  private long nextBackoffIntervalNanos;

  RetriableStream(
      MethodDescriptor<ReqT, ?> method, Metadata headers,
      ChannelBufferMeter channelBufferUsed, long perRpcBufferLimit, long channelBufferLimit,
      Executor callExecutor, ScheduledExecutorService scheduledExecutorService,
      RetryPolicy.Provider retryPolicyProvider, HedgingPolicy.Provider hedgingPolicyProvider,
      @Nullable Throttle throttle) {
    this.method = method;
    this.channelBufferUsed = channelBufferUsed;
    this.perRpcBufferLimit = perRpcBufferLimit;
    this.channelBufferLimit = channelBufferLimit;
    this.callExecutor = callExecutor;
    this.scheduledExecutorService = scheduledExecutorService;
    this.headers = headers;
    this.retryPolicyProvider = checkNotNull(retryPolicyProvider, "retryPolicyProvider");
    this.hedgingPolicyProvider = checkNotNull(hedgingPolicyProvider, "hedgingPolicyProvider");
    this.throttle = throttle;
  }

  @Nullable // null if already committed
  @CheckReturnValue
  private Runnable commit(final Substream winningSubstream) {
    synchronized (lock) {
      if (state.winningSubstream != null) {
        return null;
      }
      final Collection<Substream> savedDrainedSubstreams = state.drainedSubstreams;

      state = state.committed(winningSubstream);

      // subtract the share of this RPC from channelBufferUsed.
      channelBufferUsed.addAndGet(-perRpcBufferUsed);

      class CommitTask implements Runnable {
        @Override
        public void run() {
          // For hedging only, not needed for normal retry
          // TODO(zdapeng): also cancel all the scheduled hedges.
          for (Substream substream : savedDrainedSubstreams) {
            if (substream != winningSubstream) {
              substream.stream.cancel(CANCELLED_BECAUSE_COMMITTED);
            }
          }

          postCommit();
        }
      }

      return new CommitTask();
    }
  }

  abstract void postCommit();

  /**
   * Calls commit() and if successful runs the post commit task.
   */
  private void commitAndRun(Substream winningSubstream) {
    Runnable postCommitTask = commit(winningSubstream);

    if (postCommitTask != null) {
      postCommitTask.run();
    }
  }


  private Substream createSubstream(int previousAttempts) {
    Substream sub = new Substream(previousAttempts);
    // one tracer per substream
    final ClientStreamTracer bufferSizeTracer = new BufferSizeTracer(sub);
    ClientStreamTracer.Factory tracerFactory = new ClientStreamTracer.Factory() {
      @Override
      public ClientStreamTracer newClientStreamTracer(CallOptions callOptions, Metadata headers) {
        return bufferSizeTracer;
      }
    };

    Metadata newHeaders = updateHeaders(headers, previousAttempts);
    // NOTICE: This set _must_ be done before stream.start() and it actually is.
    sub.stream = newSubstream(tracerFactory, newHeaders);
    return sub;
  }

  /**
   * Creates a new physical ClientStream that represents a retry/hedging attempt. The returned
   * Client stream is not yet started.
   */
  abstract ClientStream newSubstream(
      ClientStreamTracer.Factory tracerFactory, Metadata headers);

  /** Adds grpc-previous-rpc-attempts in the headers of a retry/hedging RPC. */
  @VisibleForTesting
  final Metadata updateHeaders(
      Metadata originalHeaders, int previousAttempts) {
    Metadata newHeaders = new Metadata();
    newHeaders.merge(originalHeaders);
    if (previousAttempts > 0) {
      newHeaders.put(GRPC_PREVIOUS_RPC_ATTEMPTS, String.valueOf(previousAttempts));
    }
    return newHeaders;
  }

  private void drain(Substream substream) {
    int index = 0;
    int chunk = 0x80;
    List<BufferEntry> list = null;

    while (true) {
      State savedState;

      synchronized (lock) {
        savedState = state;
        if (savedState.winningSubstream != null && savedState.winningSubstream != substream) {
          // committed but not me
          break;
        }
        if (index == savedState.buffer.size()) { // I'm drained
          state = savedState.substreamDrained(substream);
          return;
        }

        if (substream.closed) {
          return;
        }

        int stop = Math.min(index + chunk, savedState.buffer.size());
        if (list == null) {
          list = new ArrayList<>(savedState.buffer.subList(index, stop));
        } else {
          list.clear();
          list.addAll(savedState.buffer.subList(index, stop));
        }
        index = stop;
      }

      for (BufferEntry bufferEntry : list) {
        savedState = state;
        if (savedState.winningSubstream != null && savedState.winningSubstream != substream) {
          // committed but not me
          break;
        }
        if (savedState.cancelled) {
          checkState(
              savedState.winningSubstream == substream,
              "substream should be CANCELLED_BECAUSE_COMMITTED already");
          return;
        }
        bufferEntry.runWith(substream);
      }
    }

    substream.stream.cancel(CANCELLED_BECAUSE_COMMITTED);
  }

  /**
   * Runs pre-start tasks. Returns the Status of shutdown if the channel is shutdown.
   */
  @CheckReturnValue
  @Nullable
  abstract Status prestart();

  /** Starts the first PRC attempt. */
  @Override
  public final void start(ClientStreamListener listener) {
    masterListener = listener;

    Status shutdownStatus = prestart();

    if (shutdownStatus != null) {
      cancel(shutdownStatus);
      return;
    }

    class StartEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.start(new Sublistener(substream));
      }
    }

    synchronized (lock) {
      state.buffer.add(new StartEntry());
    }

    Substream substream = createSubstream(0);
    drain(substream);

    // TODO(zdapeng): schedule hedging if needed
  }

  @Override
  public final void cancel(Status reason) {
    Substream noopSubstream = new Substream(0 /* previousAttempts doesn't matter here */);
    noopSubstream.stream = new NoopClientStream();
    Runnable runnable = commit(noopSubstream);

    if (runnable != null) {
      Future<?> savedScheduledRetry = scheduledRetry;
      if (savedScheduledRetry != null) {
        savedScheduledRetry.cancel(false);
        scheduledRetry = null;
      }
      masterListener.closed(reason, new Metadata());
      runnable.run();
      return;
    }

    state.winningSubstream.stream.cancel(reason);
    synchronized (lock) {
      // This is not required, but causes a short-circuit in the draining process.
      state = state.cancelled();
    }
  }

  private void delayOrExecute(BufferEntry bufferEntry) {
    Collection<Substream> savedDrainedSubstreams;
    synchronized (lock) {
      if (!state.passThrough) {
        state.buffer.add(bufferEntry);
      }
      savedDrainedSubstreams = state.drainedSubstreams;
    }

    for (Substream substream : savedDrainedSubstreams) {
      bufferEntry.runWith(substream);
    }
  }

  /**
   * Do not use it directly. Use {@link #sendMessage(ReqT)} instead because we don't use InputStream
   * for buffering.
   */
  @Override
  public final void writeMessage(InputStream message) {
    throw new IllegalStateException("RetriableStream.writeMessage() should not be called directly");
  }

  final void sendMessage(final ReqT message) {
    State savedState = state;
    if (savedState.passThrough) {
      savedState.winningSubstream.stream.writeMessage(method.streamRequest(message));
      return;
    }

    class SendMessageEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.writeMessage(method.streamRequest(message));
      }
    }

    delayOrExecute(new SendMessageEntry());
  }

  @Override
  public final void request(final int numMessages) {
    State savedState = state;
    if (savedState.passThrough) {
      savedState.winningSubstream.stream.request(numMessages);
      return;
    }

    class RequestEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.request(numMessages);
      }
    }

    delayOrExecute(new RequestEntry());
  }

  @Override
  public final void flush() {
    State savedState = state;
    if (savedState.passThrough) {
      savedState.winningSubstream.stream.flush();
      return;
    }

    class FlushEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.flush();
      }
    }

    delayOrExecute(new FlushEntry());
  }

  @Override
  public final boolean isReady() {
    for (Substream substream : state.drainedSubstreams) {
      if (substream.stream.isReady()) {
        return true;
      }
    }
    return false;
  }

  @Override
  public final void setCompressor(final Compressor compressor) {
    class CompressorEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.setCompressor(compressor);
      }
    }

    delayOrExecute(new CompressorEntry());
  }

  @Override
  public final void setFullStreamDecompression(final boolean fullStreamDecompression) {
    class FullStreamDecompressionEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.setFullStreamDecompression(fullStreamDecompression);
      }
    }

    delayOrExecute(new FullStreamDecompressionEntry());
  }

  @Override
  public final void setMessageCompression(final boolean enable) {
    class MessageCompressionEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.setMessageCompression(enable);
      }
    }

    delayOrExecute(new MessageCompressionEntry());
  }

  @Override
  public final void halfClose() {
    class HalfCloseEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.halfClose();
      }
    }

    delayOrExecute(new HalfCloseEntry());
  }

  @Override
  public final void setAuthority(final String authority) {
    class AuthorityEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.setAuthority(authority);
      }
    }

    delayOrExecute(new AuthorityEntry());
  }

  @Override
  public final void setDecompressorRegistry(final DecompressorRegistry decompressorRegistry) {
    class DecompressorRegistryEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.setDecompressorRegistry(decompressorRegistry);
      }
    }

    delayOrExecute(new DecompressorRegistryEntry());
  }

  @Override
  public final void setMaxInboundMessageSize(final int maxSize) {
    class MaxInboundMessageSizeEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.setMaxInboundMessageSize(maxSize);
      }
    }

    delayOrExecute(new MaxInboundMessageSizeEntry());
  }

  @Override
  public final void setMaxOutboundMessageSize(final int maxSize) {
    class MaxOutboundMessageSizeEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.setMaxOutboundMessageSize(maxSize);
      }
    }

    delayOrExecute(new MaxOutboundMessageSizeEntry());
  }

  @Override
  public final void setDeadline(final Deadline deadline) {
    class DeadlineEntry implements BufferEntry {
      @Override
      public void runWith(Substream substream) {
        substream.stream.setDeadline(deadline);
      }
    }

    delayOrExecute(new DeadlineEntry());
  }

  @Override
  public final Attributes getAttributes() {
    if (state.winningSubstream != null) {
      return state.winningSubstream.stream.getAttributes();
    }
    return Attributes.EMPTY;
  }

  private static Random random = new Random();

  @VisibleForTesting
  static void setRandom(Random random) {
    RetriableStream.random = random;
  }

  boolean hasHedging() {
    return false;
  }

  private interface BufferEntry {
    /** Replays the buffer entry with the given stream. */
    void runWith(Substream substream);
  }

  private final class Sublistener implements ClientStreamListener {
    final Substream substream;

    Sublistener(Substream substream) {
      this.substream = substream;
    }

    @Override
    public void headersRead(Metadata headers) {
      commitAndRun(substream);
      if (state.winningSubstream == substream) {
        masterListener.headersRead(headers);
        if (throttle != null) {
          throttle.onSuccess();
        }
      }
    }

    @Override
    public void closed(Status status, Metadata trailers) {
      closed(status, RpcProgress.PROCESSED, trailers);
    }

    @Override
    public void closed(Status status, RpcProgress rpcProgress, Metadata trailers) {
      synchronized (lock) {
        state = state.substreamClosed(substream);
      }

      // handle a race between buffer limit exceeded and closed, when setting
      // substream.bufferLimitExceeded = true happens before state.substreamClosed(substream).
      if (substream.bufferLimitExceeded) {
        commitAndRun(substream);
        if (state.winningSubstream == substream) {
          masterListener.closed(status, trailers);
        }
        return;
      }

      if (state.winningSubstream == null) {
        if (rpcProgress == RpcProgress.REFUSED && !noMoreTransparentRetry) {
          // TODO(zdapeng): in hedging case noMoreTransparentRetry might need be synchronized.
          noMoreTransparentRetry = true;
          callExecutor.execute(new Runnable() {
            @Override
            public void run() {
              // transparent retry
              Substream newSubstream = createSubstream(
                  substream.previousAttempts);
              drain(newSubstream);
            }
          });
          return;
        } else if (rpcProgress == RpcProgress.DROPPED) {
          // For normal retry, nothing need be done here, will just commit.
          // For hedging:
          // TODO(zdapeng): cancel all scheduled hedges (TBD)
        } else {
          noMoreTransparentRetry = true;

          if (retryPolicy == null) {
            retryPolicy = retryPolicyProvider.get();
            nextBackoffIntervalNanos = retryPolicy.initialBackoffNanos;
          }

          RetryPlan retryPlan = makeRetryDecision(retryPolicy, status, trailers);
          if (retryPlan.shouldRetry) {
            // The check state.winningSubstream == null, checking if is not already committed, is
            // racy, but is still safe b/c the retry will also handle committed/cancellation
            scheduledRetry = scheduledExecutorService.schedule(
                new Runnable() {
                  @Override
                  public void run() {
                    scheduledRetry = null;
                    callExecutor.execute(new Runnable() {
                      @Override
                      public void run() {
                        // retry
                        Substream newSubstream = createSubstream(substream.previousAttempts + 1);
                        drain(newSubstream);
                      }
                    });
                  }
                },
                retryPlan.backoffNanos,
                TimeUnit.NANOSECONDS);
            return;
          }
        }
      }

      if (!hasHedging()) {
        commitAndRun(substream);
        if (state.winningSubstream == substream) {
          masterListener.closed(status, trailers);
        }
      }

      // TODO(zdapeng): in hedge case, if this is a fatal status, cancel all the other attempts, and
      // close the masterListener.
    }

    /**
     * Decides in current situation whether or not the RPC should retry and if it should retry how
     * long the backoff should be. The decision does not take the commitment status into account, so
     * caller should check it separately.
     */
    // TODO(zdapeng): add HedgingPolicy as param
    private RetryPlan makeRetryDecision(RetryPolicy retryPolicy, Status status, Metadata trailer) {
      boolean shouldRetry = false;
      long backoffNanos = 0L;
      boolean isRetryableStatusCode = retryPolicy.retryableStatusCodes.contains(status.getCode());

      String pushbackStr = trailer.get(GRPC_RETRY_PUSHBACK_MS);
      Integer pushbackMillis = null;
      if (pushbackStr != null) {
        try {
          pushbackMillis = Integer.valueOf(pushbackStr);
        } catch (NumberFormatException e) {
          pushbackMillis = -1;
        }
      }

      boolean isThrottled = false;
      if (throttle != null) {
        if (isRetryableStatusCode || (pushbackMillis != null && pushbackMillis < 0)) {
          isThrottled = !throttle.onQualifiedFailureThenCheckIsAboveThreshold();
        }
      }

      if (retryPolicy.maxAttempts > substream.previousAttempts + 1 && !isThrottled) {
        if (pushbackMillis == null) {
          if (isRetryableStatusCode) {
            shouldRetry = true;
            backoffNanos = (long) (nextBackoffIntervalNanos * random.nextDouble());
            nextBackoffIntervalNanos = Math.min(
                (long) (nextBackoffIntervalNanos * retryPolicy.backoffMultiplier),
                retryPolicy.maxBackoffNanos);

          } // else no retry
        } else if (pushbackMillis >= 0) {
          shouldRetry = true;
          backoffNanos = TimeUnit.MILLISECONDS.toNanos(pushbackMillis);
          nextBackoffIntervalNanos = retryPolicy.initialBackoffNanos;
        } // else no retry
      } // else no retry

      // TODO(zdapeng): transparent retry
      // TODO(zdapeng): hedging
      return new RetryPlan(shouldRetry, backoffNanos);
    }

    @Override
    public void messagesAvailable(MessageProducer producer) {
      State savedState = state;
      checkState(
          savedState.winningSubstream != null, "Headers should be received prior to messages.");
      if (savedState.winningSubstream != substream) {
        return;
      }
      masterListener.messagesAvailable(producer);
    }

    @Override
    public void onReady() {
      // TODO(zdapeng): the more correct way to handle onReady
      if (state.drainedSubstreams.contains(substream)) {
        masterListener.onReady();
      }
    }
  }

  private static final class State {
    /** Committed and the winning substream drained. */
    final boolean passThrough;

    /** A list of buffered ClientStream runnables. Set to Null once passThrough. */
    @Nullable final List<BufferEntry> buffer;

    /**
     * Unmodifiable collection of all the substreams that are drained. Exceptional cases: Singleton
     * once passThrough; Empty if committed but not passTrough.
     */
    final Collection<Substream> drainedSubstreams;

    /** Null until committed. */
    @Nullable final Substream winningSubstream;

    /** Not required to set to true when cancelled, but can short-circuit the draining process. */
    final boolean cancelled;

    State(
        @Nullable List<BufferEntry> buffer,
        Collection<Substream> drainedSubstreams,
        @Nullable Substream winningSubstream,
        boolean cancelled,
        boolean passThrough) {
      this.buffer = buffer;
      this.drainedSubstreams =
          checkNotNull(drainedSubstreams, "drainedSubstreams");
      this.winningSubstream = winningSubstream;
      this.cancelled = cancelled;
      this.passThrough = passThrough;

      checkState(!passThrough || buffer == null, "passThrough should imply buffer is null");
      checkState(
          !passThrough || winningSubstream != null,
          "passThrough should imply winningSubstream != null");
      checkState(
          !passThrough
              || (drainedSubstreams.size() == 1 && drainedSubstreams.contains(winningSubstream))
              || (drainedSubstreams.size() == 0 && winningSubstream.closed),
          "passThrough should imply winningSubstream is drained");
      checkState(!cancelled || winningSubstream != null, "cancelled should imply committed");
    }

    @CheckReturnValue
    // GuardedBy RetriableStream.lock
    State cancelled() {
      return new State(buffer, drainedSubstreams, winningSubstream, true, passThrough);
    }

    /** The given substream is drained. */
    @CheckReturnValue
    // GuardedBy RetriableStream.lock
    State substreamDrained(Substream substream) {
      checkState(!passThrough, "Already passThrough");

      Collection<Substream> drainedSubstreams;
      
      if (substream.closed) {
        drainedSubstreams = this.drainedSubstreams;
      } else if (this.drainedSubstreams.isEmpty()) {
        // optimize for 0-retry, which is most of the cases.
        drainedSubstreams = Collections.singletonList(substream);
      } else {
        drainedSubstreams = new ArrayList<>(this.drainedSubstreams);
        drainedSubstreams.add(substream);
        drainedSubstreams = Collections.unmodifiableCollection(drainedSubstreams);
      }

      boolean passThrough = winningSubstream != null;

      List<BufferEntry> buffer = this.buffer;
      if (passThrough) {
        checkState(
            winningSubstream == substream, "Another RPC attempt has already committed");
        buffer = null;
      }

      return new State(buffer, drainedSubstreams, winningSubstream, cancelled, passThrough);
    }

    /** The given substream is closed. */
    @CheckReturnValue
    // GuardedBy RetriableStream.lock
    State substreamClosed(Substream substream) {
      substream.closed = true;
      if (this.drainedSubstreams.contains(substream)) {
        Collection<Substream> drainedSubstreams = new ArrayList<>(this.drainedSubstreams);
        drainedSubstreams.remove(substream);
        drainedSubstreams = Collections.unmodifiableCollection(drainedSubstreams);
        return new State(buffer, drainedSubstreams, winningSubstream, cancelled, passThrough);
      } else {
        return this;
      }
    }

    @CheckReturnValue
    // GuardedBy RetriableStream.lock
    State committed(Substream winningSubstream) {
      checkState(this.winningSubstream == null, "Already committed");

      boolean passThrough = false;
      List<BufferEntry> buffer = this.buffer;
      Collection<Substream> drainedSubstreams;

      if (this.drainedSubstreams.contains(winningSubstream)) {
        passThrough = true;
        buffer = null;
        drainedSubstreams = Collections.singleton(winningSubstream);
      } else {
        drainedSubstreams = Collections.emptyList();
      }

      return new State(buffer, drainedSubstreams, winningSubstream, cancelled, passThrough);
    }
  }

  /**
   * A wrapper of a physical stream of a retry/hedging attempt, that comes with some useful
   *  attributes.
   */
  private static final class Substream {
    ClientStream stream;

    // GuardedBy RetriableStream.lock
    boolean closed;

    // setting to true must be GuardedBy RetriableStream.lock
    boolean bufferLimitExceeded;

    final int previousAttempts;

    Substream(int previousAttempts) {
      this.previousAttempts = previousAttempts;
    }
  }


  /**
   * Traces the buffer used by a substream.
   */
  class BufferSizeTracer extends ClientStreamTracer {
    // Each buffer size tracer is dedicated to one specific substream.
    private final Substream substream;

    @GuardedBy("lock")
    long bufferNeeded;

    BufferSizeTracer(Substream substream) {
      this.substream = substream;
    }

    /**
     * A message is sent to the wire, so its reference would be released if no retry or
     * hedging were involved. So at this point we have to hold the reference of the message longer
     * for retry, and we need to increment {@code substream.bufferNeeded}.
     */
    @Override
    public void outboundWireSize(long bytes) {
      if (state.winningSubstream != null) {
        return;
      }

      Runnable postCommitTask = null;

      // TODO(zdapeng): avoid using the same lock for both in-bound and out-bound.
      synchronized (lock) {
        if (state.winningSubstream != null || substream.closed) {
          return;
        }
        bufferNeeded += bytes;
        if (bufferNeeded <= perRpcBufferUsed) {
          return;
        }

        if (bufferNeeded > perRpcBufferLimit) {
          substream.bufferLimitExceeded = true;
        } else {
          // Only update channelBufferUsed when perRpcBufferUsed is not exceeding perRpcBufferLimit.
          long savedChannelBufferUsed =
              channelBufferUsed.addAndGet(bufferNeeded - perRpcBufferUsed);
          perRpcBufferUsed = bufferNeeded;

          if (savedChannelBufferUsed > channelBufferLimit) {
            substream.bufferLimitExceeded = true;
          }
        }

        if (substream.bufferLimitExceeded) {
          postCommitTask = commit(substream);
        }
      }

      if (postCommitTask != null) {
        postCommitTask.run();
      }
    }
  }

  /**
   *  Used to keep track of the total amount of memory used to buffer retryable or hedged RPCs for
   *  the Channel. There should be a single instance of it for each channel.
   */
  static final class ChannelBufferMeter {
    private final AtomicLong bufferUsed = new AtomicLong();

    @VisibleForTesting
    long addAndGet(long newBytesUsed) {
      return bufferUsed.addAndGet(newBytesUsed);
    }
  }

  /**
   * Used for retry throttling.
   */
  static final class Throttle {

    private static final int THREE_DECIMAL_PLACES_SCALE_UP = 1000;

    /**
     * 1000 times the maxTokens field of the retryThrottling policy in service config.
     * The number of tokens starts at maxTokens. The token_count will always be between 0 and
     * maxTokens.
     */
    final int maxTokens;

    /**
     * Half of {@code maxTokens}.
     */
    final int threshold;

    /**
     * 1000 times the tokenRatio field of the retryThrottling policy in service config.
     */
    final int tokenRatio;

    final AtomicInteger tokenCount = new AtomicInteger();

    Throttle(float maxTokens, float tokenRatio) {
      // tokenRatio is up to 3 decimal places
      this.tokenRatio = (int) (tokenRatio * THREE_DECIMAL_PLACES_SCALE_UP);
      this.maxTokens = (int) (maxTokens * THREE_DECIMAL_PLACES_SCALE_UP);
      this.threshold = this.maxTokens / 2;
      tokenCount.set(this.maxTokens);
    }

    @VisibleForTesting
    boolean isAboveThreshold() {
      return tokenCount.get() > threshold;
    }

    /**
     * Counts down the token on qualified failure and checks if it is above the threshold
     * atomically. Qualified failure is a failure with a retryable or non-fatal status code or with
     * a not-to-retry pushback.
     */
    @VisibleForTesting
    boolean onQualifiedFailureThenCheckIsAboveThreshold() {
      while (true) {
        int currentCount = tokenCount.get();
        if (currentCount == 0) {
          return false;
        }
        int decremented = currentCount - (1 * THREE_DECIMAL_PLACES_SCALE_UP);
        boolean updated = tokenCount.compareAndSet(currentCount, Math.max(decremented, 0));
        if (updated) {
          return decremented > threshold;
        }
      }
    }

    @VisibleForTesting
    void onSuccess() {
      while (true) {
        int currentCount = tokenCount.get();
        if (currentCount == maxTokens) {
          break;
        }
        int incremented = currentCount + tokenRatio;
        boolean updated = tokenCount.compareAndSet(currentCount, Math.min(incremented, maxTokens));
        if (updated) {
          break;
        }
      }
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) {
        return true;
      }
      if (!(o instanceof Throttle)) {
        return false;
      }
      Throttle that = (Throttle) o;
      return maxTokens == that.maxTokens && tokenRatio == that.tokenRatio;
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(maxTokens, tokenRatio);
    }
  }

  private static final class RetryPlan {
    final boolean shouldRetry;
    // TODO(zdapeng) boolean hasHedging
    final long backoffNanos;

    RetryPlan(boolean shouldRetry, long backoffNanos) {
      this.shouldRetry = shouldRetry;
      this.backoffNanos = backoffNanos;
    }
  }
}
