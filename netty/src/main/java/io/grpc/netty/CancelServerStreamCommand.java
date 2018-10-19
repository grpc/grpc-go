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

package io.grpc.netty;

import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import io.grpc.Status;

/**
 * Command sent from a Netty server stream to the handler to cancel the stream.
 */
final class CancelServerStreamCommand extends WriteQueue.AbstractQueuedCommand {
  private final NettyServerStream.TransportState stream;
  private final Status reason;

  CancelServerStreamCommand(NettyServerStream.TransportState stream, Status reason) {
    this.stream = Preconditions.checkNotNull(stream, "stream");
    this.reason = Preconditions.checkNotNull(reason, "reason");
  }

  NettyServerStream.TransportState stream() {
    return stream;
  }

  Status reason() {
    return reason;
  }

  @Override
  public boolean equals(Object o) {
    if (this == o) {
      return true;
    }
    if (o == null || getClass() != o.getClass()) {
      return false;
    }

    CancelServerStreamCommand that = (CancelServerStreamCommand) o;

    return Objects.equal(this.stream, that.stream)
        && Objects.equal(this.reason, that.reason);
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(stream, reason);
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("stream", stream)
        .add("reason", reason)
        .toString();
  }
}
