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

package io.grpc.internal.testing;

import io.grpc.Status;
import io.grpc.StreamTracer;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import javax.annotation.Nullable;

/**
 * A {@link StreamTracer} suitable for testing.
 */
public interface TestStreamTracer {

  /**
   * Waits for the stream to be done.
   */
  void await() throws InterruptedException;

  /**
   * Waits for the stream to be done.
   */
  boolean await(long timeout, TimeUnit timeUnit) throws InterruptedException;

  /**
   * Returns the status passed to {@link StreamTracer#streamClosed}.
   */
  Status getStatus();

  /**
   * Returns to sum of all sizes passed to {@link StreamTracer#inboundWireSize}.
   */
  long getInboundWireSize();

  /**
   * Returns to sum of all sizes passed to {@link StreamTracer#inboundUncompressedSize}.
   */
  long getInboundUncompressedSize();

  /**
   * Returns to sum of all sizes passed to {@link StreamTracer#outboundWireSize}.
   */
  long getOutboundWireSize();

  /**
   * Returns to sum of al sizes passed to {@link StreamTracer#outboundUncompressedSize}.
   */
  long getOutboundUncompressedSize();

  /**
   * Sets whether to fail on unexpected duplicate calls to callback methods.
   */
  void setFailDuplicateCallbacks(boolean fail);

  /**
   * Returns the next captured outbound message event.
   */
  @Nullable
  String nextOutboundEvent();

  /**
   * Returns the next captured outbound message event.
   */
  String nextInboundEvent();

  /**
   * A {@link StreamTracer} suitable for testing.
   */
  public static class TestBaseStreamTracer extends StreamTracer implements TestStreamTracer {

    protected final AtomicLong outboundWireSize = new AtomicLong();
    protected final AtomicLong inboundWireSize = new AtomicLong();
    protected final AtomicLong outboundUncompressedSize = new AtomicLong();
    protected final AtomicLong inboundUncompressedSize = new AtomicLong();
    protected final LinkedBlockingQueue<String> outboundEvents = new LinkedBlockingQueue<>();
    protected final LinkedBlockingQueue<String> inboundEvents = new LinkedBlockingQueue<>();
    protected final AtomicReference<Status> streamClosedStatus = new AtomicReference<>();
    protected final AtomicReference<Throwable> streamClosedStack = new AtomicReference<>();
    protected final CountDownLatch streamClosed = new CountDownLatch(1);
    protected final AtomicBoolean failDuplicateCallbacks = new AtomicBoolean(true);

    @Override
    public void await() throws InterruptedException {
      streamClosed.await();
    }

    @Override
    public boolean await(long timeout, TimeUnit timeUnit) throws InterruptedException {
      return streamClosed.await(timeout, timeUnit);
    }

    @Override
    public Status getStatus() {
      return streamClosedStatus.get();
    }

    @Override
    public long getInboundWireSize() {
      return inboundWireSize.get();
    }

    @Override
    public long getInboundUncompressedSize() {
      return inboundUncompressedSize.get();
    }

    @Override
    public long getOutboundWireSize() {
      return outboundWireSize.get();
    }

    @Override
    public long getOutboundUncompressedSize() {
      return outboundUncompressedSize.get();
    }

    @Override
    public void outboundWireSize(long bytes) {
      outboundWireSize.addAndGet(bytes);
    }

    @Override
    public void inboundWireSize(long bytes) {
      inboundWireSize.addAndGet(bytes);
    }

    @Override
    public void outboundUncompressedSize(long bytes) {
      outboundUncompressedSize.addAndGet(bytes);
    }

    @Override
    public void inboundUncompressedSize(long bytes) {
      inboundUncompressedSize.addAndGet(bytes);
    }

    @Override
    public void streamClosed(Status status) {
      streamClosedStack.compareAndSet(null, new Throwable("first call"));
      if (!streamClosedStatus.compareAndSet(null, status)) {
        if (failDuplicateCallbacks.get()) {
          throw new AssertionError("streamClosed called more than once", streamClosedStack.get());
        }
      } else {
        streamClosed.countDown();
      }
    }

    @Override
    public void inboundMessage(int seqNo) {
      inboundEvents.add("inboundMessage(" + seqNo + ")");
    }

    @Override
    public void outboundMessage(int seqNo) {
      outboundEvents.add("outboundMessage(" + seqNo + ")");
    }

    @Override
    public void outboundMessageSent(
        int seqNo, long optionalWireSize, long optionalUncompressedSize) {
      outboundEvents.add(
          String.format(
              "outboundMessageSent(%d, %d, %d)",
              seqNo, optionalWireSize, optionalUncompressedSize));
    }

    @Override
    public void inboundMessageRead(
        int seqNo, long optionalWireSize, long optionalUncompressedSize) {
      inboundEvents.add(
          String.format(
              "inboundMessageRead(%d, %d, %d)", seqNo, optionalWireSize, optionalUncompressedSize));
    }

    @Override
    public void setFailDuplicateCallbacks(boolean fail) {
      failDuplicateCallbacks.set(fail);
    }

    @Override
    public String nextOutboundEvent() {
      return outboundEvents.poll();
    }

    @Override
    public String nextInboundEvent() {
      return inboundEvents.poll();
    }
  }
}
