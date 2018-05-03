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

package io.grpc.internal;

import com.google.common.base.Preconditions;

/**
 * An object pool that always returns the same instance and does nothing when returning the object.
 */
public final class FixedObjectPool<T> implements ObjectPool<T> {
  private final T object;

  public FixedObjectPool(T object) {
    this.object = Preconditions.checkNotNull(object, "object");
  }

  @Override
  public T getObject() {
    return object;
  }

  @Override
  public T returnObject(Object returned) {
    return null;
  }
}
