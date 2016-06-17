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

import java.net.URI;
import java.util.List;

import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A pluggable component that resolves a target {@link URI} and return addresses to the caller.
 *
 * <p>A {@code NameResolver} uses the URI's scheme to determine whether it can resolve it, and uses
 * the components after the scheme for actual resolution.
 *
 * <p>The addresses and attributes of a target may be changed over time, thus the caller registers a
 * {@link Listener} to receive continuous updates.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
@ThreadSafe
public abstract class NameResolver {
  /**
   * Returns the authority, which is also the name of the service.
   *
   * <p>An implementation must generate it locally and <string>must</strong> keep it
   * unchanged. {@code NameResolver}s created from the same factory with the same argument must
   * return the same authority.
   */
  public abstract String getServiceAuthority();

  /**
   * Starts the resolution.
   *
   * @param listener used to receive updates on the target
   */
  public abstract void start(Listener listener);

  /**
   * Stops the resolution. Updates to the Listener will stop.
   */
  public abstract void shutdown();

  /**
   * Re-resolve the name.
   *
   * <p>Can only be called after {@link #start} has been called.
   *
   * <p>This is only a hint. Implementation takes it as a signal but may not start resolution
   * immediately. It should never throw.
   *
   * <p>The default implementation is no-op.
   */
  public void refresh() {}

  public abstract static class Factory {
    /**
     * The port number used in case the target or the underlying naming system doesn't provide a
     * port number.
     */
    public static final Attributes.Key<Integer> PARAMS_DEFAULT_PORT =
        Attributes.Key.of("params-default-port");

    /**
     * Creates a {@link NameResolver} for the given target URI, or {@code null} if the given URI
     * cannot be resolved by this factory. The decision should be solely based on the scheme of the
     * URI.
     *
     * @param targetUri the target URI to be resolved, whose scheme must not be {@code null}
     * @param params optional parameters. Canonical keys are defined as {@code PARAMS_*} fields in
     *               {@link Factory}.
     */
    @Nullable
    public abstract NameResolver newNameResolver(URI targetUri, Attributes params);

    /**
     * Returns the default scheme, which will be used to construct a URI when {@link
     * ManagedChannelBuilder#forTarget(String)} is given an authority string instead of a compliant
     * URI.
     */
    public abstract String getDefaultScheme();
  }

  /**
   * Receives address updates.
   *
   * <p>All methods are expected to return quickly.
   */
  @ThreadSafe
  public interface Listener {
    /**
     * Handles updates on resolved addresses and config.
     *
     * <p>Implementations will not modify the given {@code servers}.
     *
     * @param servers the resolved server addresses.  Sublists should be considered to be
     *                an {@link EquivalentAddressGroup}. An empty list or all sublists being empty
     *                will trigger {@link #onError}
     * @param config extra configuration data from naming system
     */
    void onUpdate(List<? extends List<ResolvedServerInfo>> servers, Attributes config);

    /**
     * Handles an error from the resolver.
     *
     * @param error a non-OK status
     */
    void onError(Status error);
  }
}
