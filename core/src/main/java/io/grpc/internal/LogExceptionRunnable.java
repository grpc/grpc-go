/*
 * Copyright 2016 The gRPC Authors
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

import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A simple wrapper for a {@link Runnable} that logs any exception thrown by it, before
 * re-throwing it.
 */
public final class LogExceptionRunnable implements Runnable {

  private static final Logger log = Logger.getLogger(LogExceptionRunnable.class.getName());

  private final Runnable task;

  public LogExceptionRunnable(Runnable task) {
    this.task = checkNotNull(task, "task");
  }

  @Override
  public void run() {
    try {
      task.run();
    } catch (Throwable t) {
      log.log(Level.SEVERE, "Exception while executing runnable " + task, t);
      MoreThrowables.throwIfUnchecked(t);
      throw new AssertionError(t);
    }
  }

  @Override
  public String toString() {
    return "LogExceptionRunnable(" + task + ")";
  }
}
