/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts.internal;

import static com.google.common.base.Preconditions.checkState;

import io.grpc.Internal;
import io.netty.channel.Channel;
import io.netty.channel.ChannelPromise;
import io.netty.channel.DefaultChannelPromise;
import io.netty.util.concurrent.EventExecutor;
import java.util.ArrayList;
import java.util.List;

/**
 * Promise used when flushing the {@code pendingUnprotectedWrites} queue. It manages the many-to
 * many relationship between pending unprotected messages and the individual writes. Each protected
 * frame will be written using the same instance of this promise and it will accumulate the results.
 * Once all frames have been successfully written (or any failed), all of the promises for the
 * pending unprotected writes are notified.
 *
 * <p>NOTE: this code is based on code in Netty's {@code Http2CodecUtil}.
 */
@Internal
public final class ProtectedPromise extends DefaultChannelPromise {
  private final List<ChannelPromise> unprotectedPromises;
  private int expectedCount;
  private int successfulCount;
  private int failureCount;
  private boolean doneAllocating;

  public ProtectedPromise(Channel channel, EventExecutor executor, int numUnprotectedPromises) {
    super(channel, executor);
    unprotectedPromises = new ArrayList<>(numUnprotectedPromises);
  }

  /**
   * Adds a promise for a pending unprotected write. This will be notified after all of the writes
   * complete.
   */
  public void addUnprotectedPromise(ChannelPromise promise) {
    unprotectedPromises.add(promise);
  }

  /**
   * Allocate a new promise for the write of a protected frame. This will be used to aggregate the
   * overall success of the unprotected promises.
   *
   * @return {@code this} promise.
   */
  public ChannelPromise newPromise() {
    checkState(!doneAllocating, "Done allocating. No more promises can be allocated.");
    expectedCount++;
    return this;
  }

  /**
   * Signify that no more {@link #newPromise()} allocations will be made. The aggregation can not be
   * successful until this method is called.
   *
   * @return {@code this} promise.
   */
  public ChannelPromise doneAllocatingPromises() {
    if (!doneAllocating) {
      doneAllocating = true;
      if (successfulCount == expectedCount) {
        trySuccessInternal(null);
        return super.setSuccess(null);
      }
    }
    return this;
  }

  @Override
  public boolean tryFailure(Throwable cause) {
    if (awaitingPromises()) {
      ++failureCount;
      if (failureCount == 1) {
        tryFailureInternal(cause);
        return super.tryFailure(cause);
      }
      // TODO: We break the interface a bit here.
      // Multiple failure events can be processed without issue because this is an aggregation.
      return true;
    }
    return false;
  }

  /**
   * Fail this object if it has not already been failed.
   *
   * <p>This method will NOT throw an {@link IllegalStateException} if called multiple times because
   * that may be expected.
   */
  @Override
  public ChannelPromise setFailure(Throwable cause) {
    tryFailure(cause);
    return this;
  }

  private boolean awaitingPromises() {
    return successfulCount + failureCount < expectedCount;
  }

  @Override
  public ChannelPromise setSuccess(Void result) {
    trySuccess(result);
    return this;
  }

  @Override
  public boolean trySuccess(Void result) {
    if (awaitingPromises()) {
      ++successfulCount;
      if (successfulCount == expectedCount && doneAllocating) {
        trySuccessInternal(result);
        return super.trySuccess(result);
      }
      // TODO: We break the interface a bit here.
      // Multiple success events can be processed without issue because this is an aggregation.
      return true;
    }
    return false;
  }

  private void trySuccessInternal(Void result) {
    for (int i = 0; i < unprotectedPromises.size(); ++i) {
      unprotectedPromises.get(i).trySuccess(result);
    }
  }

  private void tryFailureInternal(Throwable cause) {
    for (int i = 0; i < unprotectedPromises.size(); ++i) {
      unprotectedPromises.get(i).tryFailure(cause);
    }
  }
}
