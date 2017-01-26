/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.internal;

import java.util.HashSet;
import javax.annotation.CheckReturnValue;
import javax.annotation.concurrent.GuardedBy;

/**
 * Aggregates the in-use state of a set of objects.
 */
abstract class InUseStateAggregator<T> {

  @GuardedBy("getLock()")
  private final HashSet<T> inUseObjects = new HashSet<T>();

  /**
   * Update the in-use state of an object. Initially no object is in use. When the return value is
   * non-{@code null}, the caller should execute the runnable after releasing locks.
   */
  @CheckReturnValue
  final Runnable updateObjectInUse(T object, boolean inUse) {
    Runnable runnable = null;
    synchronized (getLock()) {
      int origSize = inUseObjects.size();
      if (inUse) {
        inUseObjects.add(object);
        if (origSize == 0) {
          runnable = handleInUse();
        }
      } else {
        boolean removed = inUseObjects.remove(object);
        if (removed && origSize == 1) {
          handleNotInUse();
        }
      }
    }
    return runnable;
  }

  @CheckReturnValue
  final boolean isInUse() {
    synchronized (getLock()) {
      return !inUseObjects.isEmpty();
    }
  }

  abstract Object getLock();

  /**
   * Called when the aggregated in-use state has changed to true, which means at least one object is
   * in use. When the return value is non-{@code null}, then the runnable will be executed by the
   * caller of {@link #updateObjectInUse} after releasing locks.
   *
   * <p>This method is called under the lock returned by {@link #getLock}.
   */
  @GuardedBy("getLock()")
  abstract Runnable handleInUse();

  /**
   * Called when the aggregated in-use state has changed to false, which means no object is in use.
   *
   * <p>This method is called under the lock returned by {@link #getLock}.
   */
  @GuardedBy("getLock()")
  abstract void handleNotInUse();
}
