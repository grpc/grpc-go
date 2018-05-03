/*
 * Copyright 2018 The gRPC Authors
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
import javax.annotation.Nullable;

/**
 * A {@link ClientCall} which forwards all of its methods to another {@link ClientCall} which
 * may have a different sendMessage() message type.
 */
abstract class PartialForwardingClientCall<ReqT, RespT> extends ClientCall<ReqT, RespT> {
  /**
   * Returns the delegated {@code ClientCall}.
   */
  protected abstract ClientCall<?, ?> delegate();

  @Override
  public void request(int numMessages) {
    delegate().request(numMessages);
  }

  @Override
  public void cancel(@Nullable String message, @Nullable Throwable cause) {
    delegate().cancel(message, cause);
  }

  @Override
  public void halfClose() {
    delegate().halfClose();
  }

  @Override
  public void setMessageCompression(boolean enabled) {
    delegate().setMessageCompression(enabled);
  }

  @Override
  public boolean isReady() {
    return delegate().isReady();
  }

  @Override
  public Attributes getAttributes() {
    return delegate().getAttributes();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate()).toString();
  }
}
