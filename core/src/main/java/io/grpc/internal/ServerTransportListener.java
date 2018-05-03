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

package io.grpc.internal;

import io.grpc.Attributes;
import io.grpc.Metadata;

/**
 * A observer of a server-side transport for stream creation events. Notifications must occur from
 * the transport thread.
 */
public interface ServerTransportListener {
  /**
   * Called when a new stream was created by the remote client.
   *
   * @param stream the newly created stream.
   * @param method the fully qualified method name being called on the server.
   * @param headers containing metadata for the call.
   */
  void streamCreated(ServerStream stream, String method, Metadata headers);

  /**
   * The transport has finished all handshakes and is ready to process streams.
   *
   * @param attributes transport attributes
   *
   * @return the effective transport attributes that is used as the basis of call attributes
   */
  Attributes transportReady(Attributes attributes);

  /**
   * The transport completed shutting down. All resources have been released.
   */
  void transportTerminated();
}
