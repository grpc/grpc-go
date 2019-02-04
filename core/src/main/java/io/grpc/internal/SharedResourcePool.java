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

/**
 * An ObjectPool backed by a {@link SharedResourceHolder.Resource}.
 */
public final class SharedResourcePool<T> implements ObjectPool<T> {
  private final SharedResourceHolder.Resource<T> resource;

  private SharedResourcePool(SharedResourceHolder.Resource<T> resource) {
    this.resource = resource;
  }

  public static <T> SharedResourcePool<T> forResource(SharedResourceHolder.Resource<T> resource) {
    return new SharedResourcePool<>(resource);
  }

  @Override
  public T getObject() {
    return SharedResourceHolder.get(resource);
  }

  @Override
  @SuppressWarnings("unchecked")
  public T returnObject(Object object) {
    SharedResourceHolder.release(resource, (T) object);
    return null;
  }
}
