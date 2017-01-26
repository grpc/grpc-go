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
