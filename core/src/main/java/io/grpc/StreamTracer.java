/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

package io.grpc;

import javax.annotation.concurrent.ThreadSafe;

/**
 * Listens to events on a stream to collect metrics.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2861")
@ThreadSafe
public abstract class StreamTracer {
  /**
   * Stream is closed.  This will be called exactly once.
   */
  public void streamClosed(Status status) {
  }

  /**
   * An outbound message has been passed to the stream.  This is called as soon as the stream knows
   * about the message, but doesn't have further guarantee such as whether the message is serialized
   * or not.
   */
  public void outboundMessage() {
  }

  /**
   * An inbound message has been received by the stream.  This is called as soon as the stream knows
   * about the message, but doesn't have further guarantee such as whether the message is
   * deserialized or not.
   */
  public void inboundMessage() {
  }

  /**
   * The wire size of some outbound data is revealed. This can only used to record the accumulative
   * outbound wire size. There is no guarantee wrt timing or granularity of this method.
   */
  public void outboundWireSize(long bytes) {
  }

  /**
   * The uncompressed size of some outbound data is revealed. This can only used to record the
   * accumulative outbound uncompressed size. There is no guarantee wrt timing or granularity of
   * this method.
   */
  public void outboundUncompressedSize(long bytes) {
  }

  /**
   * The wire size of some inbound data is revealed. This can only be used to record the
   * accumulative received wire size. There is no guarantee wrt timing or granularity of this
   * method.
   */
  public void inboundWireSize(long bytes) {
  }

  /**
   * The uncompressed size of some inbound data is revealed. This can only used to record the
   * accumulative received uncompressed size. There is no guarantee wrt timing or granularity of
   * this method.
   */
  public void inboundUncompressedSize(long bytes) {
  }
}
