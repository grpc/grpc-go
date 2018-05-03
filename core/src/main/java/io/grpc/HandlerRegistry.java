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

package io.grpc;

import java.util.Collections;
import java.util.List;
import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Registry of services and their methods used by servers to dispatching incoming calls.
 */
@ThreadSafe
public abstract class HandlerRegistry {

  /**
   * Returns the {@link ServerServiceDefinition}s provided by the registry, or an empty list if not
   * supported by the implementation.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2222")
  public List<ServerServiceDefinition> getServices() {
    return Collections.emptyList();
  }

  /**
   * Lookup a {@link ServerMethodDefinition} by its fully-qualified name.
   *
   * @param methodName to lookup {@link ServerMethodDefinition} for.
   * @param authority the authority for the desired method (to do virtual hosting). If {@code null}
   *        the first matching method will be returned.
   * @return the resolved method or {@code null} if no method for that name exists.
   */
  @Nullable
  public abstract ServerMethodDefinition<?, ?> lookupMethod(
      String methodName, @Nullable String authority);

  /**
   * Lookup a {@link ServerMethodDefinition} by its fully-qualified name.
   *
   * @param methodName to lookup {@link ServerMethodDefinition} for.
   * @return the resolved method or {@code null} if no method for that name exists.
   */
  @Nullable
  public final ServerMethodDefinition<?, ?> lookupMethod(String methodName) {
    return lookupMethod(methodName, null);
  }

}
