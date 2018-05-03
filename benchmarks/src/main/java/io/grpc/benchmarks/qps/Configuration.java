/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.benchmarks.qps;

/**
 * Configuration for a benchmark application.
 */
public interface Configuration {
  /**
   * Builder for the {@link Configuration}.
   * @param <T> The type of {@link Configuration} that this builder creates.
   */
  interface Builder<T extends Configuration> {
    /**
     * Builds the {@link Configuration} from the given command-line arguments.
     * @throws IllegalArgumentException if unable to build the configuration for any reason.
     */
    T build(String[] args);

    /**
     * Prints the command-line usage for the application based on the options supported by this
     * builder.
     */
    void printUsage();
  }
}
