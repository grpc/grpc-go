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

import io.grpc.ClientStreamTracer;
import io.grpc.Metadata;
import io.grpc.Status;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import javax.annotation.Nullable;

/**
 * A {@link ClientStreamTracer} suitable for testing.
 */
public class TestClientStreamTracer extends ClientStreamTracer implements TestStreamTracer {
  private final TestBaseStreamTracer delegate = new TestBaseStreamTracer();
  protected final CountDownLatch outboundHeadersLatch = new CountDownLatch(1);
  protected final AtomicReference<Throwable> outboundHeadersCalled =
      new AtomicReference<>();
  protected final AtomicReference<Throwable> inboundHeadersCalled =
      new AtomicReference<>();
  protected final AtomicReference<Metadata> inboundTrailers = new AtomicReference<>();

  @Override
  public void await() throws InterruptedException {
    delegate.await();
  }

  @Override
  public boolean await(long timeout, TimeUnit timeUnit) throws InterruptedException {
    return delegate.await(timeout, timeUnit);
  }

  /**
   * Returns if {@link ClientStreamTracer#inboundHeaders} has been called.
   */
  public boolean getInboundHeaders() {
    return inboundHeadersCalled.get() != null;
  }

  /**
   * Returns the inbound trailers if {@link ClientStreamTracer#inboundTrailers} has been called, or
   * {@code null}.
   */
  @Nullable
  public Metadata getInboundTrailers() {
    return inboundTrailers.get();
  }

  /**
   * Returns if {@link ClientStreamTracer#outboundHeaders} has been called.
   */
  public boolean getOutboundHeaders() {
    return outboundHeadersCalled.get() != null;
  }

  /**
   * Allow tests to await the outbound header event, which depending on the test case may be
   * necessary (e.g., if we test for a Netty client's outbound headers upon receiving the start of
   * stream on the server side, the tracer won't know that headers were sent until a channel future
   * executes).
   */
  public boolean awaitOutboundHeaders(int timeout, TimeUnit unit) throws Exception {
    return outboundHeadersLatch.await(timeout, unit);
  }

  @Override
  public Status getStatus() {
    return delegate.getStatus();
  }

  @Override
  public long getInboundWireSize() {
    return delegate.getInboundWireSize();
  }

  @Override
  public long getInboundUncompressedSize() {
    return delegate.getInboundUncompressedSize();
  }

  @Override
  public long getOutboundWireSize() {
    return delegate.getOutboundWireSize();
  }

  @Override
  public long getOutboundUncompressedSize() {
    return delegate.getOutboundUncompressedSize();
  }

  @Override
  public void setFailDuplicateCallbacks(boolean fail) {
    delegate.setFailDuplicateCallbacks(fail);
  }

  @Override
  public String nextOutboundEvent() {
    return delegate.nextOutboundEvent();
  }

  @Override
  public String nextInboundEvent() {
    return delegate.nextInboundEvent();
  }

  @Override
  public void outboundWireSize(long bytes) {
    delegate.outboundWireSize(bytes);
  }

  @Override
  public void inboundWireSize(long bytes) {
    delegate.inboundWireSize(bytes);
  }

  @Override
  public void outboundUncompressedSize(long bytes) {
    delegate.outboundUncompressedSize(bytes);
  }

  @Override
  public void inboundUncompressedSize(long bytes) {
    delegate.inboundUncompressedSize(bytes);
  }

  @Override
  public void streamClosed(Status status) {
    delegate.streamClosed(status);
  }

  @Override
  public void inboundMessage(int seqNo) {
    delegate.inboundMessage(seqNo);
  }

  @Override
  public void outboundMessage(int seqNo) {
    delegate.outboundMessage(seqNo);
  }

  @Override
  public void outboundMessageSent(int seqNo, long optionalWireSize, long optionalUncompressedSize) {
    delegate.outboundMessageSent(seqNo, optionalWireSize, optionalUncompressedSize);
  }

  @Override
  public void inboundMessageRead(int seqNo, long optionalWireSize, long optionalUncompressedSize) {
    delegate.inboundMessageRead(seqNo, optionalWireSize, optionalUncompressedSize);
  }

  @Override
  public void outboundHeaders() {
    if (!outboundHeadersCalled.compareAndSet(null, new Exception("first stack"))
        && delegate.failDuplicateCallbacks.get()) {
      throw new AssertionError(
          "outboundHeaders called more than once",
          new Exception("second stack", outboundHeadersCalled.get()));
    }
    outboundHeadersLatch.countDown();
  }

  @Override
  public void inboundHeaders() {
    if (!inboundHeadersCalled.compareAndSet(null, new Exception("first stack"))
        && delegate.failDuplicateCallbacks.get()) {
      throw new AssertionError(
          "inboundHeaders called more than once",
          new Exception("second stack", inboundHeadersCalled.get()));
    }
  }

  @Override
  public void inboundTrailers(Metadata trailers) {
    if (delegate.getStatus() != null) {
      throw new AssertionError(
          "stream has already been closed with " + delegate.getStatus(),
          delegate.streamClosedStack.get());
    }
    if (!inboundTrailers.compareAndSet(null, trailers) && delegate.failDuplicateCallbacks.get()) {
      throw new AssertionError("inboundTrailers called more than once");
    }
  }
}
