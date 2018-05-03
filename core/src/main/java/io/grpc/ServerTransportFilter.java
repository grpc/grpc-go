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

/**
 * Listens on server transport life-cycle events, with the capability to read and/or change
 * transport attributes.  Attributes returned by this filter will be merged into {@link
 * ServerCall#getAttributes}.
 *
 * <p>Multiple filters maybe registered to a server, in which case the output of a filter is the
 * input of the next filter.  For example, what returned by {@link #transportReady} of a filter is
 * passed to the same method of the next filter, and the last filter's return value is the effective
 * transport attributes.
 *
 * <p>{@link Grpc} defines commonly used attributes.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2132")
public abstract class ServerTransportFilter {
  /**
   * Called when a transport is ready to process streams.  All necessary handshakes, e.g., TLS
   * handshake, are done at this point.
   *
   * <p>Note the implementation should always inherit the passed-in attributes using {@code
   * Attributes.newBuilder(transportAttrs)}, instead of creating one from scratch.
   *
   * @param transportAttrs current transport attributes
   *
   * @return new transport attributes. Default implementation returns the passed-in attributes
   *         intact.
   */
  public Attributes transportReady(Attributes transportAttrs) {
    return transportAttrs;
  }

  /**
   * Called when a transport is terminated.  Default implementation is no-op.
   *
   * @param transportAttrs the effective transport attributes, which is what returned by {@link
   * #transportReady} of the last executed filter.
   */
  public void transportTerminated(Attributes transportAttrs) {
  }
}
