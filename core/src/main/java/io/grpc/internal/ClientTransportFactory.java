/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc.internal;

import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import io.grpc.Attributes;
import io.grpc.ChannelLogger;
import io.grpc.HttpConnectProxiedSocketAddress;
import java.io.Closeable;
import java.net.SocketAddress;
import java.util.concurrent.ScheduledExecutorService;
import javax.annotation.Nullable;

/** Pre-configured factory for creating {@link ConnectionClientTransport} instances. */
public interface ClientTransportFactory extends Closeable {
  /**
   * Creates an unstarted transport for exclusive use. Ownership of {@code options} is passed to the
   * callee; the caller should not reuse or read from the options after this method is called.
   *
   * @param serverAddress the address that the transport is connected to
   * @param options additional configuration
   * @param channelLogger logger for the transport.
   */
  ConnectionClientTransport newClientTransport(
      SocketAddress serverAddress,
      ClientTransportOptions options,
      ChannelLogger channelLogger);

  /**
   * Returns an executor for scheduling provided by the transport. The service should be configured
   * to allow cancelled scheduled runnables to be GCed.
   *
   * <p>The executor should not be used after the factory has been closed. The caller should ensure
   * any outstanding tasks are cancelled before the factory is closed. However, it is a
   * <a href="https://github.com/grpc/grpc-java/issues/1981">known issue</a> that ClientCallImpl may
   * use this executor after close, so implementations should not go out of their way to prevent
   * usage.
   */
  ScheduledExecutorService getScheduledExecutorService();

  /**
   * Releases any resources.
   *
   * <p>After this method has been called, it's no longer valid to call
   * {@link #newClientTransport}. No guarantees about thread-safety are made.
   */
  @Override
  void close();

  /**
   * Options passed to {@link #newClientTransport(SocketAddress, ClientTransportOptions)}. Although
   * it is safe to save this object if received, it is generally expected that the useful fields are
   * copied and then the options object is discarded. This allows using {@code final} for those
   * fields as well as avoids retaining unused objects contained in the options.
   */
  final class ClientTransportOptions {
    private ChannelLogger channelLogger;
    private String authority = "unknown-authority";
    private Attributes eagAttributes = Attributes.EMPTY;
    @Nullable private String userAgent;
    @Nullable private HttpConnectProxiedSocketAddress connectProxiedSocketAddr;

    public ChannelLogger getChannelLogger() {
      return channelLogger;
    }

    public ClientTransportOptions setChannelLogger(ChannelLogger channelLogger) {
      this.channelLogger = channelLogger;
      return this;
    }

    public String getAuthority() {
      return authority;
    }

    /** Sets the non-null authority. */
    public ClientTransportOptions setAuthority(String authority) {
      this.authority = Preconditions.checkNotNull(authority, "authority");
      return this;
    }

    public Attributes getEagAttributes() {
      return eagAttributes;
    }

    /** Sets the non-null EquivalentAddressGroup's attributes. */
    public ClientTransportOptions setEagAttributes(Attributes eagAttributes) {
      Preconditions.checkNotNull(eagAttributes, "eagAttributes");
      this.eagAttributes = eagAttributes;
      return this;
    }

    @Nullable
    public String getUserAgent() {
      return userAgent;
    }

    public ClientTransportOptions setUserAgent(@Nullable String userAgent) {
      this.userAgent = userAgent;
      return this;
    }

    @Nullable
    public HttpConnectProxiedSocketAddress getHttpConnectProxiedSocketAddress() {
      return connectProxiedSocketAddr;
    }

    public ClientTransportOptions setHttpConnectProxiedSocketAddress(
        @Nullable HttpConnectProxiedSocketAddress connectProxiedSocketAddr) {
      this.connectProxiedSocketAddr = connectProxiedSocketAddr;
      return this;
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(authority, eagAttributes, userAgent, connectProxiedSocketAddr);
    }

    @Override
    public boolean equals(Object o) {
      if (!(o instanceof ClientTransportOptions)) {
        return false;
      }
      ClientTransportOptions that = (ClientTransportOptions) o;
      return this.authority.equals(that.authority)
          && this.eagAttributes.equals(that.eagAttributes)
          && Objects.equal(this.userAgent, that.userAgent)
          && Objects.equal(this.connectProxiedSocketAddr, that.connectProxiedSocketAddr);
    }
  }
}
