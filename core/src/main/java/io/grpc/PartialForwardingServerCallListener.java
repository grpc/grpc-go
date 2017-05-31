/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

/**
 * A {@link ServerCall.Listener} which forwards all of its methods to another {@link
 * ServerCall.Listener} which may have a different parameterized type than the
 * onMessage() message type.
 */
abstract class PartialForwardingServerCallListener<ReqT>
    extends ServerCall.Listener<ReqT> {
  /**
   * Returns the delegated {@code ServerCall.Listener}.
   */
  protected abstract ServerCall.Listener<?> delegate();

  @Override
  public void onHalfClose() {
    delegate().onHalfClose();
  }

  @Override
  public void onCancel() {
    delegate().onCancel();
  }

  @Override
  public void onComplete() {
    delegate().onComplete();
  }

  @Override
  public void onReady() {
    delegate().onReady();
  }
}
