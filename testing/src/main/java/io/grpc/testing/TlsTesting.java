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

package io.grpc.testing;

import io.grpc.ExperimentalApi;
import java.io.InputStream;

/** Convenience utilities for using TLS in tests. */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1791")
public final class TlsTesting {
  /**
   * Retrieves the specified test certificate or key resource in src/main/resources/certs/ as an
   * {@code InputStream}.
   *
   * @param name name of a file in src/main/resources/certs/, e.g., {@code "ca.key"}.
   *
   * @since 1.8.0
   */
  public static InputStream loadCert(String name) {
    return TestUtils.class.getResourceAsStream("/certs/" + name);
  }

  private TlsTesting() {}
}
