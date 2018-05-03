/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc.protobuf;

import com.google.protobuf.Descriptors.MethodDescriptor;
import javax.annotation.CheckReturnValue;

/**
 * Provides access to the underlying proto service method descriptor.
 *
 * @since 1.7.0
 */
public interface ProtoMethodDescriptorSupplier extends ProtoServiceDescriptorSupplier {
  /**
   * Returns method descriptor to the proto service method.
   */
  @CheckReturnValue
  MethodDescriptor getMethodDescriptor();
}
