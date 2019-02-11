/*
 * Copyright 2019 The gRPC Authors
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

import java.net.SocketAddress;

/**
 * An address that contains the information about making a connection via a proxy.  It is created by
 * a {@link ProxyDetector}.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/5279")
public abstract class ProxiedSocketAddress extends SocketAddress {
  private static final long serialVersionUID = 0L;
}
