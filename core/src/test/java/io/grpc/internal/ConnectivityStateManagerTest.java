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

import static org.junit.Assert.assertEquals;

import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.ConnectivityState;
import java.util.LinkedList;
import java.util.concurrent.Executor;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link ConnectivityStateManager}.
 */
@RunWith(JUnit4.class)
public class ConnectivityStateManagerTest {
  private final FakeClock executor = new FakeClock();
  private final ConnectivityStateManager state =
      new ConnectivityStateManager(ConnectivityState.IDLE);
  private final LinkedList<ConnectivityState> sink = new LinkedList<ConnectivityState>();

  @Test
  public void noCallback() {
    state.gotoState(ConnectivityState.CONNECTING);
    assertEquals(ConnectivityState.CONNECTING, state.getState());
    state.gotoState(ConnectivityState.TRANSIENT_FAILURE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, state.getState());
  }

  @Test
  public void registerCallbackBeforeStateChanged() {
    state.gotoState(ConnectivityState.CONNECTING);
    assertEquals(ConnectivityState.CONNECTING, state.getState());

    state.notifyWhenStateChanged(new Runnable() {
        @Override
        public void run() {
          sink.add(state.getState());
        }
      }, executor.getScheduledExecutorService(), ConnectivityState.CONNECTING);

    assertEquals(0, executor.numPendingTasks());
    state.gotoState(ConnectivityState.TRANSIENT_FAILURE);
    // Make sure the callback is run in the executor
    assertEquals(0, sink.size());
    assertEquals(1, executor.runDueTasks());
    assertEquals(0, executor.numPendingTasks());
    assertEquals(1, sink.size());
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, sink.poll());
  }
  
  @Test
  public void registerCallbackAfterStateChanged() {
    state.gotoState(ConnectivityState.CONNECTING);
    assertEquals(ConnectivityState.CONNECTING, state.getState());

    state.notifyWhenStateChanged(new Runnable() {
        @Override
        public void run() {
          sink.add(state.getState());
        }
      }, executor.getScheduledExecutorService(), ConnectivityState.IDLE);

    // Make sure the callback is run in the executor
    assertEquals(0, sink.size());
    assertEquals(1, executor.runDueTasks());
    assertEquals(0, executor.numPendingTasks());
    assertEquals(1, sink.size());
    assertEquals(ConnectivityState.CONNECTING, sink.poll());
  }

  @Test
  public void callbackOnlyCalledOnTransition() {
    state.notifyWhenStateChanged(new Runnable() {
        @Override
        public void run() {
          sink.add(state.getState());
        }
      }, executor.getScheduledExecutorService(), ConnectivityState.IDLE);

    state.gotoState(ConnectivityState.IDLE);
    assertEquals(0, executor.numPendingTasks());
    assertEquals(0, sink.size());
  }

  @Test
  public void callbacksAreOneShot() {
    Runnable callback = new Runnable() {
        @Override
        public void run() {
          sink.add(state.getState());
        }
      };

    state.notifyWhenStateChanged(callback, executor.getScheduledExecutorService(),
        ConnectivityState.IDLE);
    // First transition triggers the callback
    state.gotoState(ConnectivityState.CONNECTING);
    assertEquals(1, executor.runDueTasks());
    assertEquals(1, sink.size());
    assertEquals(ConnectivityState.CONNECTING, sink.poll());
    assertEquals(0, executor.numPendingTasks());

    // Second transition doesn't trigger the callback
    state.gotoState(ConnectivityState.TRANSIENT_FAILURE);
    assertEquals(0, sink.size());
    assertEquals(0, executor.numPendingTasks());

    // Register another callback
    state.notifyWhenStateChanged(callback, executor.getScheduledExecutorService(),
        ConnectivityState.TRANSIENT_FAILURE);

    state.gotoState(ConnectivityState.READY);
    state.gotoState(ConnectivityState.IDLE);
    // Callback loses the race with the second stage change
    assertEquals(1, executor.runDueTasks());
    assertEquals(1, sink.size());
    // It will see the second state
    assertEquals(ConnectivityState.IDLE, sink.poll());
    assertEquals(0, executor.numPendingTasks());
  }

  @Test
  public void multipleCallbacks() {
    final LinkedList<String> callbackRuns = new LinkedList<String>();
    state.notifyWhenStateChanged(new Runnable() {
        @Override
        public void run() {
          sink.add(state.getState());
          callbackRuns.add("callback1");
        }
      }, executor.getScheduledExecutorService(), ConnectivityState.IDLE);
    state.notifyWhenStateChanged(new Runnable() {
        @Override
        public void run() {
          sink.add(state.getState());
          callbackRuns.add("callback2");
        }
      }, executor.getScheduledExecutorService(), ConnectivityState.IDLE);
    state.notifyWhenStateChanged(new Runnable() {
        @Override
        public void run() {
          sink.add(state.getState());
          callbackRuns.add("callback3");
        }
      }, executor.getScheduledExecutorService(), ConnectivityState.READY);

    // callback3 is run immediately because the source state is already different from the current
    // state.
    assertEquals(1, executor.runDueTasks());
    assertEquals(1, callbackRuns.size());
    assertEquals("callback3", callbackRuns.poll());
    assertEquals(1, sink.size());
    assertEquals(ConnectivityState.IDLE, sink.poll());

    // Now change the state.
    state.gotoState(ConnectivityState.CONNECTING);
    assertEquals(2, executor.runDueTasks());
    assertEquals(2, callbackRuns.size());
    assertEquals("callback1", callbackRuns.poll());
    assertEquals("callback2", callbackRuns.poll());
    assertEquals(2, sink.size());
    assertEquals(ConnectivityState.CONNECTING, sink.poll());
    assertEquals(ConnectivityState.CONNECTING, sink.poll());

    assertEquals(0, executor.numPendingTasks());
  }

  private Runnable newRecursiveCallback(final Executor executor) {
    return new Runnable() {
      @Override
      public void run() {
        sink.add(state.getState());
        state.notifyWhenStateChanged(newRecursiveCallback(executor), executor, state.getState());
      }
    };
  }

  @Test
  public void registerCallbackFromCallback() {
    state.notifyWhenStateChanged(newRecursiveCallback(executor.getScheduledExecutorService()),
        executor.getScheduledExecutorService(), state.getState());

    state.gotoState(ConnectivityState.CONNECTING);
    assertEquals(1, executor.runDueTasks());
    assertEquals(0, executor.numPendingTasks());
    assertEquals(1, sink.size());
    assertEquals(ConnectivityState.CONNECTING, sink.poll());

    state.gotoState(ConnectivityState.READY);
    assertEquals(1, executor.runDueTasks());
    assertEquals(0, executor.numPendingTasks());
    assertEquals(1, sink.size());
    assertEquals(ConnectivityState.READY, sink.poll());
  }

  @Test
  public void registerCallbackFromCallbackDirectExecutor() {
    state.notifyWhenStateChanged(newRecursiveCallback(MoreExecutors.directExecutor()),
        MoreExecutors.directExecutor(), state.getState());

    state.gotoState(ConnectivityState.CONNECTING);
    assertEquals(1, sink.size());
    assertEquals(ConnectivityState.CONNECTING, sink.poll());

    state.gotoState(ConnectivityState.READY);
    assertEquals(1, sink.size());
    assertEquals(ConnectivityState.READY, sink.poll());
  }
}
