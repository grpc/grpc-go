/*
 * Copyright 2015, Google Inc. All rights reserved.
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
      taskQueue = new ArrayDeque<Runnable>(4);
    }
    taskQueue.add(r);
  }
}
