/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

import io.grpc.Attributes;
import io.grpc.Compressor;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/** A logical {@link ClientStream} that is retriable. */
abstract class RetriableStream<ReqT> implements ClientStream {
  private static final Status CANCELLED_BECAUSE_COMMITTED =
      Status.CANCELLED.withDescription("Stream thrown away because RetriableStream committed");

  private final MethodDescriptor<ReqT, ?> method;

  /** Must be held when updating state or accessing state.buffer. */
  private final Object lock = new Object();

  private volatile State state =
      new State(
          new ArrayList<BufferEntry>(), Collections.<ClientStream>emptySet(), null, false, false);

  private ClientStreamListener masterListener;

  RetriableStream(MethodDescriptor<ReqT, ?> method) {
    this.method = method;
  }

  private boolean commit(ClientStream winningSubstream) {
    if (commit0(winningSubstream)) {
      postCommit();
      return true;
    }
    return false;
  }

  private boolean commit0(ClientStream winningSubstream) {
    Collection<ClientStream> savedDrainedSubstreams;
    synchronized (lock) {
      if (state.winningSubstream != null) {
        return false;
      }

      savedDrainedSubstreams = state.drainedSubstreams;

      state = state.committed(winningSubstream);
    }

    // For hedging only, not needed for normal retry
    // TODO(zdapeng): also cancel all the scheduled hedges.
    for (ClientStream substream : savedDrainedSubstreams) {
      if (substream != winningSubstream) {
        substream.cancel(CANCELLED_BECAUSE_COMMITTED);
      }
    }
    return true;
  }

  abstract void postCommit();

  private void retry() {
    ClientStream substream = newSubstream();

    // TODO(zdapeng): update "grpc-retry-attempts" header

    drain(substream);
  }

  abstract ClientStream newSubstream();

  private void drain(ClientStream substream) {
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
          state = savedState.drained(substream);
          return;
        }

        int stop = Math.min(index + chunk, savedState.buffer.size());
        if (list == null) {
          list = new ArrayList<BufferEntry>(stop - index);
        }
        list.clear();
        list.addAll(savedState.buffer.subList(index, stop));
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

    substream.cancel(CANCELLED_BECAUSE_COMMITTED);
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
      public void runWith(ClientStream substream) {
        substream.start(new Sublistener(substream));
      }
    }

    synchronized (lock) {
      state.buffer.add(new StartEntry());
    }

    ClientStream substream = newSubstream();
    drain(substream);

    // TODO(zdapeng): schedule hedging if needed
  }

  @Override
  public final void cancel(Status reason) {
    if (commit0(new NoopClientStream())) {
      masterListener.closed(reason, new Metadata());
      postCommit();
      return;
    }

    state.winningSubstream.cancel(reason);
    synchronized (lock) {
      // This is not required, but causes a short-circuit in the draining process.
      state = state.cancelled();
    }
  }

  private void delayOrExecute(BufferEntry bufferEntry) {
    Collection<ClientStream> savedDrainedSubstreams;
    synchronized (lock) {
      if (!state.passThrough) {
        state.buffer.add(bufferEntry);
      }
      savedDrainedSubstreams = state.drainedSubstreams;
    }

    for (ClientStream substream : savedDrainedSubstreams) {
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
      savedState.winningSubstream.writeMessage(method.streamRequest(message));
      return;
    }

    class SendMessageEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.writeMessage(method.streamRequest(message));
      }
    }

    delayOrExecute(new SendMessageEntry());
  }

  @Override
  public final void request(final int numMessages) {
    State savedState = state;
    if (savedState.passThrough) {
      savedState.winningSubstream.request(numMessages);
      return;
    }

    class RequestEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.request(numMessages);
      }
    }

    delayOrExecute(new RequestEntry());
  }

  @Override
  public final void flush() {
    State savedState = state;
    if (savedState.passThrough) {
      savedState.winningSubstream.flush();
      return;
    }

    class FlushEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.flush();
      }
    }

    delayOrExecute(new FlushEntry());
  }

  @Override
  public final boolean isReady() {
    for (ClientStream substream : state.drainedSubstreams) {
      if (substream.isReady()) {
        return true;
      }
    }
    return false;
  }

  @Override
  public final void setCompressor(final Compressor compressor) {
    class CompressorEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.setCompressor(compressor);
      }
    }

    delayOrExecute(new CompressorEntry());
  }

  @Override
  public final void setFullStreamDecompression(final boolean fullStreamDecompression) {
    class FullStreamDecompressionEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.setFullStreamDecompression(fullStreamDecompression);
      }
    }

    delayOrExecute(new FullStreamDecompressionEntry());
  }

  @Override
  public final void setMessageCompression(final boolean enable) {
    class MessageCompressionEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.setMessageCompression(enable);
      }
    }

    delayOrExecute(new MessageCompressionEntry());
  }

  @Override
  public final void halfClose() {
    class HalfCloseEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.halfClose();
      }
    }

    delayOrExecute(new HalfCloseEntry());
  }

  @Override
  public final void setAuthority(final String authority) {
    class AuthorityEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.setAuthority(authority);
      }
    }

    delayOrExecute(new AuthorityEntry());
  }

  @Override
  public final void setDecompressorRegistry(final DecompressorRegistry decompressorRegistry) {
    class DecompressorRegistryEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.setDecompressorRegistry(decompressorRegistry);
      }
    }

    delayOrExecute(new DecompressorRegistryEntry());
  }

  @Override
  public final void setMaxInboundMessageSize(final int maxSize) {
    class MaxInboundMessageSizeEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.setMaxInboundMessageSize(maxSize);
      }
    }

    delayOrExecute(new MaxInboundMessageSizeEntry());
  }

  @Override
  public final void setMaxOutboundMessageSize(final int maxSize) {
    class MaxOutboundMessageSizeEntry implements BufferEntry {
      @Override
      public void runWith(ClientStream substream) {
        substream.setMaxOutboundMessageSize(maxSize);
      }
    }

    delayOrExecute(new MaxOutboundMessageSizeEntry());
  }

  @Override
  public final Attributes getAttributes() {
    if (state.winningSubstream != null) {
      return state.winningSubstream.getAttributes();
    }
    return Attributes.EMPTY;
  }

  // TODO(zdapeng): implement retry policy.
  // Retry policy is obtained from the combination of the name resolver plus channel builder, and
  // passed all the way down to this class.
  boolean shouldRetry() {
    return false;
  }

  boolean hasHedging() {
    return false;
  }

  private interface BufferEntry {
    /** Replays the buffer entry with the given stream. */
    void runWith(ClientStream substream);
  }

  private final class Sublistener implements ClientStreamListener {
    final ClientStream substream;

    Sublistener(ClientStream substream) {
      this.substream = substream;
    }

    @Override
    public void headersRead(Metadata headers) {
      if (commit(substream)) {
        masterListener.headersRead(headers);
      }
    }

    @Override
    public void closed(Status status, Metadata trailers) {
      if (state.winningSubstream == null && shouldRetry()) {
        // The check state.winningSubstream == null, checking if is not already committed, is racy,
        // but is still safe b/c the retry will also handle committed/cancellation
        // TODO(zdapeng): backoff and schedule; retry() should run in an executor
        retry();
      } else if (!hasHedging()) {
        commit(substream);
        if (state.winningSubstream == substream) {
          masterListener.closed(status, trailers);
        }
      }
      // TODO(zdapeng): in hedge case, if this is a fatal status, cancel all the other attempts, and
      // close the masterListener.
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
    final Collection<ClientStream> drainedSubstreams;

    /** Null until committed. */
    @Nullable final ClientStream winningSubstream;

    /** Not required to set to true when cancelled, but can short-circuit the draining process. */
    final boolean cancelled;

    State(
        @Nullable List<BufferEntry> buffer,
        Collection<ClientStream> drainedSubstreams,
        @Nullable ClientStream winningSubstream,
        boolean cancelled,
        boolean passThrough) {
      this.buffer = buffer;
      this.drainedSubstreams =
          Collections.unmodifiableCollection(checkNotNull(drainedSubstreams, "drainedSubstreams"));
      this.winningSubstream = winningSubstream;
      this.cancelled = cancelled;
      this.passThrough = passThrough;

      checkState(!passThrough || buffer == null, "passThrough should imply buffer is null");
      checkState(
          !passThrough || winningSubstream != null,
          "passThrough should imply winningSubstream != null");
      checkState(
          !passThrough
              || (drainedSubstreams.size() == 1 && drainedSubstreams.contains(winningSubstream)),
          "passThrough should imply winningSubstream is drained");
      checkState(!cancelled || winningSubstream != null, "cancelled should imply committed");
    }

    @CheckReturnValue
    @GuardedBy("lock")
    State cancelled() {
      return new State(buffer, drainedSubstreams, winningSubstream, true, passThrough);
    }

    /** The given substream is drained. */
    @CheckReturnValue
    @GuardedBy("lock")
    State drained(ClientStream substream) {
      checkState(!passThrough, "Already passThrough");

      Set<ClientStream> drainedSubstreams = new HashSet<ClientStream>();
      drainedSubstreams.addAll(this.drainedSubstreams);
      drainedSubstreams.add(substream);

      boolean passThrough = winningSubstream != null;

      List<BufferEntry> buffer = this.buffer;
      if (passThrough) {
        checkState(winningSubstream == substream, "Another RPC attempt has already committed");
        buffer = null;
      }

      return new State(buffer, drainedSubstreams, winningSubstream, cancelled, passThrough);
    }

    @CheckReturnValue
    @GuardedBy("lock")
    State committed(ClientStream winningSubstream) {
      checkState(this.winningSubstream == null, "Already committed");

      boolean passThrough = false;
      List<BufferEntry> buffer = this.buffer;
      Collection<ClientStream> drainedSubstreams = Collections.emptySet();

      if (this.drainedSubstreams.contains(winningSubstream)) {
        passThrough = true;
        buffer = null;
        drainedSubstreams = Collections.singleton(winningSubstream);
      }

      return new State(buffer, drainedSubstreams, winningSubstream, cancelled, passThrough);
    }
  }
}
