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
 *
 * <p>The contents and nature of this interface (and whether it remains an interface) is
 * experimental, in that it can change. However, we are guaranteeing stability for the
 * <em>name</em>. That is, we are guaranteeing stability for code to be returned a reference and
 * pass that reference to gRPC for usage. However, code may not call or implement the {@code
 * CallCredentials} itself if it wishes to only use stable APIs.
 */
public interface CallCredentials {
  /**
   * The security level of the transport. It is guaranteed to be present in the {@code attrs} passed
   * to {@link #applyRequestMetadata}. It is by default {@link SecurityLevel#NONE} but can be
   * overridden by the transport.
   *
   * @deprecated transport implementations should use {@code
   * io.grpc.internal.GrpcAttributes.ATTR_SECURITY_LEVEL} instead.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1914")
  @Grpc.TransportAttr
  @Deprecated
  public static final Key<SecurityLevel> ATTR_SECURITY_LEVEL =
      Key.create("io.grpc.internal.GrpcAttributes.securityLevel");

  /**
   * The authority string used to authenticate the server. Usually it's the server's host name. It
   * is guaranteed to be present in the {@code attrs} passed to {@link #applyRequestMetadata}. It is
   * by default from the channel, but can be overridden by the transport and {@link
   * io.grpc.CallOptions} with increasing precedence.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1914")
  @Grpc.TransportAttr
  @Deprecated
  public static final Key<String> ATTR_AUTHORITY = Key.create("io.grpc.CallCredentials.authority");

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
   *
   * @deprecated implement {@link CallCredentials2} instead.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1914")
  @Deprecated
  void applyRequestMetadata(
      MethodDescriptor<?, ?> method, Attributes attrs,
      Executor appExecutor, MetadataApplier applier);

  /**
   * Should be a noop but never called; tries to make it clearer to implementors that they may break
   * in the future.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1914")
  void thisUsesUnstableApi();

  /**
   * The outlet of the produced headers. Not thread-safe.
   *
   * <p>Exactly one of its methods must be called to make the RPC proceed.
   */
  @Deprecated
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1914")
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

  /**
   * The request-related information passed to {@code CallCredentials2.applyRequestMetadata()}.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1914")
  public abstract static class RequestInfo {
    /**
     * The method descriptor of this RPC.
     */
    public abstract MethodDescriptor<?, ?> getMethodDescriptor();

    /**
     * The security level on the transport.
     */
    public abstract SecurityLevel getSecurityLevel();

    /**
     * Returns the authority string used to authenticate the server for this call.
     */
    public abstract String getAuthority();

    /**
     * Returns the transport attributes.
     */
    @Grpc.TransportAttr
    public abstract Attributes getTransportAttrs();
  }
}
