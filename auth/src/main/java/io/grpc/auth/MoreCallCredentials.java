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

package io.grpc.auth;

import com.google.auth.Credentials;
import io.grpc.CallCredentials;

/**
 * A utility class that converts other types of credentials to {@link CallCredentials}.
 */
public final class MoreCallCredentials {
  /**
   * Converts a Google Auth Library {@link Credentials} to {@link CallCredentials}.
   *
   * <p>Although this is a stable API, note that the returned instance's API is not stable. You are
   * free to use the class name {@code CallCredentials} and pass the instance to other code, but the
   * instance can't be called directly from code expecting stable behavior. See {@link
   * CallCredentials}.
   */
  public static CallCredentials from(Credentials creds) {
    return new GoogleAuthLibraryCallCredentials(creds);
  }

  private MoreCallCredentials() {
  }
}
