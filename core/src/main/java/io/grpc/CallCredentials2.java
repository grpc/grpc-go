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

import java.util.concurrent.Executor;

/**
 * The new interface for {@link CallCredentials}.
 *
 * <p>THIS CLASS NAME IS TEMPORARY and is part of a migration. This class will BE DELETED as it
 * replaces {@link CallCredentials} in short-term.  THIS CLASS IS ONLY REFERENCED BY IMPLEMENTIONS.
 * All consumers should be always referencing {@link CallCredentials}.
 *
 * @deprecated the new interface has been promoted into {@link CallCredentials}.  Implementations
 *             should switch back to "{@code extends CallCredentials}".
 */
@Deprecated
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4901")
public abstract class CallCredentials2 extends CallCredentials {
  /**
   * Pass the credential data to the given {@link MetadataApplier}, which will propagate it to the
   * request metadata.
   *
   * <p>It is called for each individual RPC, within the {@link Context} of the call, before the
   * stream is about to be created on a transport. Implementations should not block in this
   * method. If metadata is not immediately available, e.g., needs to be fetched from network, the
   * implementation may give the {@code applier} to an asynchronous task which will eventually call
   * the {@code applier}. The RPC proceeds only after the {@code applier} is called.
   *
   * @param requestInfo request-related information
   * @param appExecutor The application thread-pool. It is provided to the implementation in case it
   *        needs to perform blocking operations.
   * @param applier The outlet of the produced headers. It can be called either before or after this
   *        method returns.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1914")
  public abstract void applyRequestMetadata(
      RequestInfo requestInfo, Executor appExecutor, MetadataApplier applier);

  @Override
  public final void applyRequestMetadata(
      RequestInfo requestInfo, Executor appExecutor,
      final CallCredentials.MetadataApplier applier) {
    applyRequestMetadata(requestInfo, appExecutor, new MetadataApplier() {
        @Override
        public void apply(Metadata headers) {
          applier.apply(headers);
        }

        @Override
        public void fail(Status status) {
          applier.fail(status);
        }
      });
  }

  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1914")
  public abstract static class MetadataApplier extends CallCredentials.MetadataApplier {}
}
