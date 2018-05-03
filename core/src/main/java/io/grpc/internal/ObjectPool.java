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

import javax.annotation.concurrent.ThreadSafe;

/**
 * An object pool.
 */
@ThreadSafe
public interface ObjectPool<T> {
  /**
   * Get an object from the pool.
   */
  T getObject();

  /**
   * Return the object to the pool.  The caller should not use the object beyond this point.
   *
   * @return always {@code null}
   */
  T returnObject(Object object);
}
