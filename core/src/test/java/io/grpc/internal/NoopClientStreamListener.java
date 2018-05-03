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

package io.grpc.internal;

import io.grpc.Metadata;
import io.grpc.Status;

/**
 * No-op base class for testing.
 */
public class NoopClientStreamListener implements ClientStreamListener {
  @Override
  public void messagesAvailable(MessageProducer producer) {}

  @Override
  public void onReady() {}

  @Override
  public void headersRead(Metadata headers) {}

  @Override
  public void closed(Status status, Metadata trailers) {}

  @Override
  public void closed(Status status, RpcProgress rpcProgress, Metadata trailers) {}
}
