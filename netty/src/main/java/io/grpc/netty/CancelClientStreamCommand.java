/*
 * Copyright 2014 The gRPC Authors
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
import io.grpc.Status;
import javax.annotation.Nullable;

/**
 * Command sent from a Netty client stream to the handler to cancel the stream.
 */
class CancelClientStreamCommand extends WriteQueue.AbstractQueuedCommand {
  private final NettyClientStream.TransportState stream;
  @Nullable private final Status reason;

  CancelClientStreamCommand(NettyClientStream.TransportState stream, Status reason) {
    this.stream = Preconditions.checkNotNull(stream, "stream");
    Preconditions.checkArgument(
        reason == null || !reason.isOk(), "Should not cancel with OK status");
    this.reason = reason;
  }

  NettyClientStream.TransportState stream() {
    return stream;
  }

  @Nullable
  Status reason() {
    return reason;
  }
}
