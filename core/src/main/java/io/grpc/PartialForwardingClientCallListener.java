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

/**
 * A {@link ClientCall.Listener} which forwards all of its methods to another {@link
 * ClientCall.Listener} which may have a different parameterized type than the
 * onMessage() message type.
 */
abstract class PartialForwardingClientCallListener<RespT> extends ClientCall.Listener<RespT> {
  /**
   * Returns the delegated {@code ClientCall.Listener}.
   */
  protected abstract ClientCall.Listener<?> delegate();

  @Override
  public void onHeaders(Metadata headers) {
    delegate().onHeaders(headers);
  }

  @Override
  public void onClose(Status status, Metadata trailers) {
    delegate().onClose(status, trailers);
  }

  @Override
  public void onReady() {
    delegate().onReady();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate()).toString();
  }
}
