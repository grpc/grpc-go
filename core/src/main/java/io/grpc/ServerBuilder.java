/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import java.io.File;
import java.util.concurrent.Executor;

import javax.annotation.Nullable;

/**
 * A builder for {@link Server} instances.
 *
 * @param <T> The concrete type of this builder.
 */
public abstract class ServerBuilder<T extends ServerBuilder<T>> {

  public static ServerBuilder<?> forPort(int port) {
    return ServerProvider.provider().builderForPort(port);
  }

  /**
   * Provides a custom executor.
   *
   * <p>It's an optional parameter. If the user has not provided an executor when the server is
   * built, the builder will use a static cached thread pool.
   *
   * <p>The server won't take ownership of the given executor. It's caller's responsibility to
   * shut down the executor when it's desired.
   */
  public abstract T executor(@Nullable Executor executor);

  /**
   * Adds a service implementation to the handler registry.
   *
   * @throws UnsupportedOperationException if this builder does not support dynamically adding
   *                                       services.
   */
  public abstract T addService(ServerServiceDefinition service);

  /**
   * Makes the server use TLS.
   *
   * @param certChain file containing the full certificate chain
   * @param privateKey file containing the private key
   */
  public abstract T useTransportSecurity(File certChain, File privateKey);

  /**
   * Builds a server using the given parameters.
   *
   * <p>The returned service will not been started or be bound a port. You will need to start it
   * with {@link Server#start()}.
   */
  public abstract Server build();
}
