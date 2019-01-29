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

package io.grpc.internal;

import io.grpc.ServerStreamTracer;
import java.util.List;

/** A hack to access protected methods from io.grpc.internal. */
public final class AccessProtectedHack {
  public static List<? extends InternalServer> serverBuilderBuildTransportServer(
      AbstractServerImplBuilder<?> builder,
      List<ServerStreamTracer.Factory> streamTracerFactories,
      TransportTracer.Factory transportTracerFactory) {
    builder.transportTracerFactory = transportTracerFactory;
    return builder.buildTransportServers(streamTracerFactories);
  }

  private AccessProtectedHack() {}
}
