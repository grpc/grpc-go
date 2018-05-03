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

import java.io.IOException;
import java.net.URL;
import java.util.Enumeration;

/**
 * A ClassLoader to help test service providers.
 */
public class ReplacingClassLoader extends ClassLoader {
  private final String resource;
  private final String replacement;

  /**
   * Construct an instance where {@code replacement} is loaded instead of {@code resource}.
   */
  public ReplacingClassLoader(ClassLoader parent, String resource, String replacement) {
    super(parent);
    this.resource = resource;
    this.replacement = replacement;
  }

  @Override
  public URL getResource(String name) {
    if (resource.equals(name)) {
      return getParent().getResource(replacement);
    }
    return super.getResource(name);
  }

  @Override
  public Enumeration<URL> getResources(String name) throws IOException {
    if (resource.equals(name)) {
      return getParent().getResources(replacement);
    }
    return super.getResources(name);
  }
}

