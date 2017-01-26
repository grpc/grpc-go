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

import io.grpc.Attributes.Key;
import java.util.concurrent.Executor;

/**
 * Carries credential data that will be propagated to the server via request metadata for each RPC.
 *
 * <p>This is used by {@link CallOptions#withCallCredentials} and {@code withCallCredentials()} on
 * the generated stub, for example:
 * <pre>
 * FooGrpc.FooStub stub = FooGrpc.newStub(channel);
 * response = stub.withCallCredentials(creds).bar(request);
 * </pre>
 */
@ExperimentalApi("https//github.com/grpc/grpc-java/issues/1914")
public interface CallCredentials {
  /**
   * The security level of the transport. It is guaranteed to be present in the {@code attrs} passed
   * to {@link #applyRequestMetadata}. It is by default {@link SecurityLevel#NONE} but can be
   * overridden by the transport.
   */
  public static final Key<SecurityLevel> ATTR_SECURITY_LEVEL =
      Key.of("io.grpc.CallCredentials.securityLevel");

  /**
   * The authority string used to authenticate the server. Usually it's the server's host name. It
   * is guaranteed to be present in the {@code attrs} passed to {@link #applyRequestMetadata}. It is
   * by default from the channel, but can be overridden by the transport and {@link
   * io.grpc.CallOptions} with increasing precedence.
   */
  public static final Key<String> ATTR_AUTHORITY = Key.of("io.grpc.CallCredentials.authority");

  /**
   * Pass the credential data to the given {@link MetadataApplier}, which will propagate it to
   * the request metadata.
   *
   * <p>It is called for each individual RPC, within the {@link Context} of the call, before the
   * stream is about to be created on a transport. Implementations should not block in this
   * method. If metadata is not immediately available, e.g., needs to be fetched from network, the
   * implementation may give the {@code applier} to an asynchronous task which will eventually call
   * the {@code applier}. The RPC proceeds only after the {@code applier} is called.
   *
   * @param method The method descriptor of this RPC
   * @param attrs Additional attributes from the transport, along with the keys defined in this
   *        interface (i.e. the {@code ATTR_*} fields) which are guaranteed to be present.
   * @param appExecutor The application thread-pool. It is provided to the implementation in case it
   *        needs to perform blocking operations.
   * @param applier The outlet of the produced headers. It can be called either before or after this
   *        method returns.
   */
  void applyRequestMetadata(
      MethodDescriptor<?, ?> method, Attributes attrs,
      Executor appExecutor, MetadataApplier applier);

  /**
   * The outlet of the produced headers. Not thread-safe.
   *
   * <p>Exactly one of its methods must be called to make the RPC proceed.
   */
  public interface MetadataApplier {
    /**
     * Called when headers are successfully generated. They will be merged into the original
     * headers.
     */
    void apply(Metadata headers);

    /**
     * Called when there has been an error when preparing the headers. This will fail the RPC.
     */
    void fail(Status status);
  }
}
