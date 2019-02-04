/*
 * Copyright 2015 The gRPC Authors
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

import com.google.common.base.Preconditions;
import java.util.ArrayDeque;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Executes a task directly in the calling thread, unless it's a reentrant call in which case the
 * task is enqueued and executed once the calling task completes.
 *
 * <p>The {@code Executor} assumes that reentrant calls are rare and its fast path is thus
 * optimized for that - avoiding queuing and additional object creation altogether.
 *
 * <p>This class is not thread-safe.
 */
class SerializeReentrantCallsDirectExecutor implements Executor {

  private static final Logger log =
      Logger.getLogger(SerializeReentrantCallsDirectExecutor.class.getName());

  private boolean executing;
  // Lazily initialized if a reentrant call is detected.
  private ArrayDeque<Runnable> taskQueue;

  @Override
  public void execute(Runnable task) {
    Preconditions.checkNotNull(task, "'task' must not be null.");
    if (!executing) {
      executing = true;
      try {
        task.run();
      } catch (Throwable t) {
        log.log(Level.SEVERE, "Exception while executing runnable " + task, t);
      } finally {
        if (taskQueue != null) {
          completeQueuedTasks();
        }
        executing = false;
      }
    } else {
      enqueue(task);
    }
  }

  private void completeQueuedTasks() {
    Runnable task = null;
    while ((task = taskQueue.poll()) != null) {
      try {
        task.run();
      } catch (Throwable t) {
        // Log it and keep going
        log.log(Level.SEVERE, "Exception while executing runnable " + task, t);
      }
    }
  }

  private void enqueue(Runnable r) {
    if (taskQueue == null) {
      taskQueue = new ArrayDeque<>(4);
    }
    taskQueue.add(r);
  }
}
