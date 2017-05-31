/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

package io.grpc.protobuf.service;

import io.grpc.BindableService;
import io.grpc.ExperimentalApi;

/**
 * Do not use this class.
 *
 * @deprecated use {@link io.grpc.protobuf.services.ProtoReflectionService} instead.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2222")
@Deprecated
public final class ProtoReflectionService {

  /**
   * Do not use this method.
   *
   * @deprecated use {@link io.grpc.protobuf.services.ProtoReflectionService#newInstance()} instead.
   */
  @Deprecated
  public static BindableService getInstance() {
    return io.grpc.protobuf.services.ProtoReflectionService.newInstance();
  }
}
