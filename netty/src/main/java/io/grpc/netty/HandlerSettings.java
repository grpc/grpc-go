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

package io.grpc.netty;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.Internal;

/**
 * Allows autoFlowControl to be turned on and off from interop testing and flow control windows to
 * be accessed. For internal use only.
 */
@VisibleForTesting // Visible for tests in other packages.
@Internal
public final class HandlerSettings {

  private static volatile boolean enabled;

  private static boolean autoFlowControlOn;
  // These will be the most recently created handlers created using NettyClientTransport and
  // NettyServerTransport
  private static AbstractNettyHandler clientHandler;
  private static AbstractNettyHandler serverHandler;

  static void setAutoWindow(AbstractNettyHandler handler) {
    if (!enabled) {
      return;
    }
    synchronized (HandlerSettings.class) {
      handler.setAutoTuneFlowControl(autoFlowControlOn);
      if (handler instanceof NettyClientHandler) {
        clientHandler = handler;
      } else if (handler instanceof NettyServerHandler) {
        serverHandler = handler;
      } else {
        throw new RuntimeException("Expecting NettyClientHandler or NettyServerHandler");
      }
    }
  }

  public static void enable(boolean enable) {
    enabled = enable;
  }

  public static synchronized void autoWindowOn(boolean autoFlowControl) {
    autoFlowControlOn = autoFlowControl;
  }

  public static synchronized int getLatestClientWindow() {
    return getLatestWindow(clientHandler);
  }

  public static synchronized int getLatestServerWindow() {
    return getLatestWindow(serverHandler);
  }

  private static synchronized int getLatestWindow(AbstractNettyHandler handler) {
    Preconditions.checkNotNull(handler);
    return handler.decoder().flowController()
        .initialWindowSize(handler.connection().connectionStream());
  }
}
