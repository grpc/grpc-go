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

package io.grpc.internal.testing;

import io.grpc.Status;
import io.grpc.StreamTracer;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

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
   * Returns how many times {@link StreamTracer#inboundMessage} has been called.
   */
  int getInboundMessageCount();

  /**
   * Returns how many times {@link StreamTracer#outboundMessage} has been called.
   */
  int getOutboundMessageCount();

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
   * A {@link StreamTracer} suitable for testing.
   */
  public static class TestBaseStreamTracer extends StreamTracer implements TestStreamTracer {

    protected final AtomicLong outboundWireSize = new AtomicLong();
    protected final AtomicLong inboundWireSize = new AtomicLong();
    protected final AtomicLong outboundUncompressedSize = new AtomicLong();
    protected final AtomicLong inboundUncompressedSize = new AtomicLong();
    protected final AtomicInteger inboundMessageCount = new AtomicInteger();
    protected final AtomicInteger outboundMessageCount = new AtomicInteger();
    protected final AtomicReference<Status> streamClosedStatus = new AtomicReference<Status>();
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
    public int getInboundMessageCount() {
      return inboundMessageCount.get();
    }

    @Override
    public int getOutboundMessageCount() {
      return outboundMessageCount.get();
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
      if (!streamClosedStatus.compareAndSet(null, status)) {
        if (failDuplicateCallbacks.get()) {
          throw new AssertionError("streamClosed called more than once");
        }
      } else {
        streamClosed.countDown();
      }
    }

    @Override
    public void inboundMessage() {
      inboundMessageCount.incrementAndGet();
    }

    @Override
    public void outboundMessage() {
      outboundMessageCount.incrementAndGet();
    }
  }
}
