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

package io.grpc.grpclb;

import io.grpc.ExperimentalApi;
import io.grpc.Metadata;

/**
 * Constants for the GRPCLB load-balancer.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1782")
public final class GrpclbConstants {

  /**
   * The opaque token given by the remote balancer for each returned server address.  The client
   * will send this token with any requests sent to the associated server.
   */
  public static final Metadata.Key<String> TOKEN_METADATA_KEY =
      Metadata.Key.of("lb-token", Metadata.ASCII_STRING_MARSHALLER);

  private GrpclbConstants() { }
}
