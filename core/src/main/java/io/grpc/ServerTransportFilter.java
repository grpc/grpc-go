/*
 * Copyright 2016, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
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
