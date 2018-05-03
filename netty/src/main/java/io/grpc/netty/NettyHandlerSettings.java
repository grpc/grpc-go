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

package io.grpc.netty;

import com.google.common.base.Preconditions;

/**
 * Allows autoFlowControl to be turned on and off from interop testing and flow control windows to
 * be accessed.
 */
final class NettyHandlerSettings {

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
    synchronized (NettyHandlerSettings.class) {
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
