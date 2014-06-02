package com.google.net.stubby.stub;

import com.google.common.collect.Lists;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;

import java.util.Collections;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import javax.annotation.Nullable;

/**
 * Utility implementation of {link StreamObserver} used in testing. Records all the observed
 * values produced by the stream as well as any errors.
 */
public class StreamRecorder<T> implements StreamObserver<T> {

  /**
   * Create a new recorder.
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
    results = Collections.synchronizedList(Lists.<T>newArrayList());
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
   * Wait for the stream to terminate.
   */
  public void awaitCompletion() throws Exception {
    latch.await();
  }

  /**
   * Wait a fixed timeout for the stream to terminate.
   */
  public boolean awaitCompletion(int timeout, TimeUnit unit) throws Exception {
    return latch.await(timeout, unit);
  }

  /**
   * Return the current set of received values.
   */
  public List<T> getValues() {
    return Collections.unmodifiableList(results);
  }

  /**
   * Return the stream terminating error.
   */
  @Nullable public Throwable getError() {
    return error;
  }

  /**
   * Return a {@link ListenableFuture} for the first value received from the stream. Useful
   * for testing unary call patterns.
   */
  public ListenableFuture<T> firstValue() {
    return firstValue;
  }
}
