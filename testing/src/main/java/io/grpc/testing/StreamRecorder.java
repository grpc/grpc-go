/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc.testing;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.ExperimentalApi;
import io.grpc.stub.StreamObserver;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;

/**
 * Utility implementation of {@link StreamObserver} used in testing. Records all the observed
 * values produced by the stream as well as any errors.
 *
 * @deprecated Not for public use
 */
@Deprecated
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1791")
public class StreamRecorder<T> implements StreamObserver<T> {

  /**
   * Creates a new recorder.
   */
  public static <T> StreamRecorder<T> create() {
    return new StreamRecorder<>();
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
  public void onNext(T value) {
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
