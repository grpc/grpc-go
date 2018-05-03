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

package io.grpc;

import com.google.common.base.MoreObjects;

/**
 * A {@link ServerCall} which forwards all of it's methods to another {@link ServerCall} which
 * may have a different onMessage() message type.
 */
abstract class PartialForwardingServerCall<ReqT, RespT> extends ServerCall<ReqT, RespT> {
  /**
   * Returns the delegated {@code ServerCall}.
   */
  protected abstract ServerCall<?, ?> delegate();

  @Override
  public void request(int numMessages) {
    delegate().request(numMessages);
  }

  @Override
  public void sendHeaders(Metadata headers) {
    delegate().sendHeaders(headers);
  }

  @Override
  public boolean isReady() {
    return delegate().isReady();
  }

  @Override
  public void close(Status status, Metadata trailers) {
    delegate().close(status, trailers);
  }

  @Override
  public boolean isCancelled() {
    return delegate().isCancelled();
  }

  @Override
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1703")
  public void setMessageCompression(boolean enabled) {
    delegate().setMessageCompression(enabled);
  }

  @Override
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public void setCompression(String compressor) {
    delegate().setCompression(compressor);
  }

  @Override
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1779")
  public Attributes getAttributes() {
    return delegate().getAttributes();
  }

  @Override
  public String getAuthority() {
    return delegate().getAuthority();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate()).toString();
  }
}
