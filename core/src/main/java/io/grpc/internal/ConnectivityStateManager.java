/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

import io.grpc.ConnectivityState;
import java.util.ArrayList;
import java.util.concurrent.Executor;
import javax.annotation.concurrent.NotThreadSafe;

/**
 * Book-keeps the connectivity state and callbacks.
 */
@NotThreadSafe
class ConnectivityStateManager {
  private ArrayList<StateCallbackEntry> callbacks;

  private ConnectivityState state;

  ConnectivityStateManager(ConnectivityState initialState) {
    state = initialState;
  }

  void notifyWhenStateChanged(Runnable callback, Executor executor, ConnectivityState source) {
    StateCallbackEntry callbackEntry = new StateCallbackEntry(callback, executor);
    if (state != source) {
      callbackEntry.runInExecutor();
    } else {
      if (callbacks == null) {
        callbacks = new ArrayList<StateCallbackEntry>();
      }
      callbacks.add(callbackEntry);
    }
  }

  // TODO(zhangkun83): return a runnable in order to escape transport set lock, in case direct
  // executor is used?
  void gotoState(ConnectivityState newState) {
    if (state != newState) {
      if (state == ConnectivityState.SHUTDOWN) {
        throw new IllegalStateException("Cannot transition out of SHUTDOWN to " + newState);
      }
      state = newState;
      if (callbacks == null) {
        return;
      }
      // Swap out callback list before calling them, because a callback may register new callbacks,
      // if run in direct executor, can cause ConcurrentModificationException.
      ArrayList<StateCallbackEntry> savedCallbacks = callbacks;
      callbacks = null;
      for (StateCallbackEntry callback : savedCallbacks) {
        callback.runInExecutor();
      }
    }
  }

  ConnectivityState getState() {
    return state;
  }

  private static class StateCallbackEntry {
    final Runnable callback;
    final Executor executor;

    StateCallbackEntry(Runnable callback, Executor executor) {
      this.callback = callback;
      this.executor = executor;
    }

    void runInExecutor() {
      executor.execute(callback);
    }
  }
}
