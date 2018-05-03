/*
 * Copyright 2016 The gRPC Authors
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
 * Provides a callback method for a service to receive a reference to its server. The contract with
 * {@link ServerBuilder} is that this method will be called on all registered services implementing
 * the interface after build() has been called and before the {@link Server} instance is returned.
 */
@Internal
public interface InternalNotifyOnServerBuild {
  /** Notifies the service that the server has been built. */
  void notifyOnBuild(Server server);
}
