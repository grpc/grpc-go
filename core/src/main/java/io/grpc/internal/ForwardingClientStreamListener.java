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

package io.grpc.internal;

import com.google.common.base.MoreObjects;
import io.grpc.Metadata;
import io.grpc.Status;

abstract class ForwardingClientStreamListener implements ClientStreamListener {

  protected abstract ClientStreamListener delegate();

  @Override
  public void headersRead(Metadata headers) {
    delegate().headersRead(headers);
  }

  @Override
  public void closed(Status status, Metadata trailers) {
    delegate().closed(status, trailers);
  }

  @Override
  public void closed(Status status, RpcProgress rpcProgress, Metadata trailers) {
    delegate().closed(status, rpcProgress, trailers);
  }

  @Override
  public void messagesAvailable(MessageProducer producer) {
    delegate().messagesAvailable(producer);
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
