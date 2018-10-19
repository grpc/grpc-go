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

import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import io.grpc.Status;
import io.netty.handler.codec.http2.Http2Headers;

/**
 * Command sent from the transport to the Netty channel to send response headers to the client.
 */
final class SendResponseHeadersCommand extends WriteQueue.AbstractQueuedCommand {
  private final StreamIdHolder stream;
  private final Http2Headers headers;
  private final Status status;

  private SendResponseHeadersCommand(StreamIdHolder stream, Http2Headers headers, Status status) {
    this.stream = Preconditions.checkNotNull(stream, "stream");
    this.headers = Preconditions.checkNotNull(headers, "headers");
    this.status = status;
  }

  static SendResponseHeadersCommand createHeaders(StreamIdHolder stream, Http2Headers headers) {
    return new SendResponseHeadersCommand(stream, headers, null);
  }

  static SendResponseHeadersCommand createTrailers(
      StreamIdHolder stream, Http2Headers headers, Status status) {
    return new SendResponseHeadersCommand(
        stream, headers, Preconditions.checkNotNull(status, "status"));
  }

  StreamIdHolder stream() {
    return stream;
  }

  Http2Headers headers() {
    return headers;
  }

  boolean endOfStream() {
    return status != null;
  }

  Status status() {
    return status;
  }

  @Override
  public boolean equals(Object that) {
    if (that == null || !that.getClass().equals(SendResponseHeadersCommand.class)) {
      return false;
    }
    SendResponseHeadersCommand thatCmd = (SendResponseHeadersCommand) that;
    return thatCmd.stream.equals(stream)
        && thatCmd.headers.equals(headers)
        && thatCmd.status.equals(status);
  }

  @Override
  public String toString() {
    return getClass().getSimpleName() + "(stream=" + stream.id() + ", headers=" + headers
        + ", status=" + status + ")";
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(stream, status);
  }
}
