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
import io.netty.handler.codec.http2.Http2Headers;

/**
 * A command to create a new stream. This is created by {@link NettyClientStream} and passed to the
 * {@link NettyClientHandler} for processing in the Channel thread.
 */
class CreateStreamCommand extends WriteQueue.AbstractQueuedCommand {
  private final Http2Headers headers;
  private final NettyClientStream.TransportState stream;
  private final boolean shouldBeCountedForInUse;
  private final boolean get;

  CreateStreamCommand(
      Http2Headers headers,
      NettyClientStream.TransportState stream,
      boolean shouldBeCountedForInUse, boolean get) {
    this.stream = Preconditions.checkNotNull(stream, "stream");
    this.headers = Preconditions.checkNotNull(headers, "headers");
    this.shouldBeCountedForInUse = shouldBeCountedForInUse;
    this.get = get;
  }

  NettyClientStream.TransportState stream() {
    return stream;
  }

  Http2Headers headers() {
    return headers;
  }

  boolean shouldBeCountedForInUse() {
    return shouldBeCountedForInUse;
  }

  boolean isGet() {
    return get;
  }
}
