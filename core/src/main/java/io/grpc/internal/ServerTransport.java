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

import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalInstrumented;
import io.grpc.Status;
import java.util.concurrent.ScheduledExecutorService;

/** An inbound connection. */
public interface ServerTransport extends InternalInstrumented<SocketStats> {
  /**
   * Initiates an orderly shutdown of the transport. Existing streams continue, but new streams will
   * eventually begin failing. New streams "eventually" begin failing because shutdown may need to
   * be processed on a separate thread. May only be called once.
   */
  void shutdown();

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are closed. Existing calls
   * should be closed with the provided {@code reason}.
   */
  void shutdownNow(Status reason);

  /**
   * Returns an executor for scheduling provided by the transport. The service should be configured
   * to allow cancelled scheduled runnables to be GCed.
   *
   * <p>The executor may not be used after the transport terminates. The caller should ensure any
   * outstanding tasks are cancelled when the transport terminates.
   */
  ScheduledExecutorService getScheduledExecutorService();
}
