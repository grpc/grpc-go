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

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Internal;

/**
 * Controlled accessor to {@link NettyHandlerSettings}.
 */
@VisibleForTesting // Visible for tests in other packages.
@Internal
public final class InternalHandlerSettings {

  public static void enable(boolean enable) {
    NettyHandlerSettings.enable(enable);
  }

  public static synchronized void autoWindowOn(boolean autoFlowControl) {
    NettyHandlerSettings.autoWindowOn(autoFlowControl);
  }

  public static synchronized int getLatestClientWindow() {
    return NettyHandlerSettings.getLatestClientWindow();
  }

  public static synchronized int getLatestServerWindow() {
    return NettyHandlerSettings.getLatestServerWindow();
  }
}
