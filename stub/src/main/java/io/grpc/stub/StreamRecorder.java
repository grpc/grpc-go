/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc.stub;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import javax.annotation.Nullable;

/**
 * Utility implementation of {@link StreamObserver} used in testing. Records all the observed
 * values produced by the stream as well as any errors.
 */
public class StreamRecorder<T> implements StreamObserver<T> {

  /**
   * Creates a new recorder.
   */
  public static <T> StreamRecorder<T> create() {
    return new StreamRecorder<T>();
  }

  private final CountDownLatch latch;
  private final List<T> results;
  private Throwable error;
  private final SettableFuture<T> firstValue;

  private StreamRecorder() {
    firstValue = SettableFuture.create();
    latch = new CountDownLatch(1);
    results = Collections.synchronizedList(new ArrayList<T>());
  }

  @Override
  public void onValue(T value) {
    if (!firstValue.isDone()) {
      firstValue.set(value);
    }
    results.add(value);
  }

  @Override
  public void onError(Throwable t) {
    if (!firstValue.isDone()) {
      firstValue.setException(t);
    }
    error = t;
    latch.countDown();
  }

  @Override
  public void onCompleted() {
    if (!firstValue.isDone()) {
      firstValue.setException(new IllegalStateException("No first value provided"));
    }
    latch.countDown();
  }

  /**
   * Waits for the stream to terminate.
   */
  public void awaitCompletion() throws Exception {
    latch.await();
  }

  /**
   * Waits a fixed timeout for the stream to terminate.
   */
  public boolean awaitCompletion(int timeout, TimeUnit unit) throws Exception {
    return latch.await(timeout, unit);
  }

  /**
   * Returns the current set of received values.
   */
  public List<T> getValues() {
    return Collections.unmodifiableList(results);
  }

  /**
   * Returns the stream terminating error.
   */
  @Nullable public Throwable getError() {
    return error;
  }

  /**
   * Returns a {@link ListenableFuture} for the first value received from the stream. Useful
   * for testing unary call patterns.
   */
  public ListenableFuture<T> firstValue() {
    return firstValue;
  }
}
