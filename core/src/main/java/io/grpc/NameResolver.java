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

package io.grpc;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import java.lang.annotation.Documented;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.net.URI;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
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
 *
 * <p>A {@code NameResolver} does not need to automatically re-resolve on failure. Instead, the
 * {@link Listener} is responsible for eventually (after an appropriate backoff period) invoking
 * {@link #refresh()}.
 *
 * <p>Implementations <strong>don't need to be thread-safe</strong>.  All methods are guaranteed to
 * be called sequentially.  Additionally, all methods that have side-effects, i.e.,
 * {@link #start(Observer)}, {@link #shutdown} and {@link #refresh} are called from the same
 * {@link SynchronizationContext} as returned by {@link Helper#getSynchronizationContext}.
 *
 * @since 1.0.0
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
public abstract class NameResolver {
  /**
   * Returns the authority used to authenticate connections to servers.  It <strong>must</strong> be
   * from a trusted source, because if the authority is tampered with, RPCs may be sent to the
   * attackers which may leak sensitive user data.
   *
   * <p>An implementation must generate it without blocking, typically in line, and
   * <strong>must</strong> keep it unchanged. {@code NameResolver}s created from the same factory
   * with the same argument must return the same authority.
   *
   * @since 1.0.0
   */
  public abstract String getServiceAuthority();

  /**
   * Starts the resolution.
   *
   * @param listener used to receive updates on the target
   * @deprecated override {@link #start(Observer)} instead.
   * @since 1.0.0
   */
  @Deprecated
  public void start(Listener listener) {
    throw new UnsupportedOperationException("Not implemented");
  }

  /**
   * Starts the resolution.  This method will become abstract in 1.21.0.
   *
   * @param observer used to receive updates on the target
   * @since 1.21.0
   */
  public void start(Observer observer) {
    start((Listener) observer);
  }

  /**
   * Stops the resolution. Updates to the Listener will stop.
   *
   * @since 1.0.0
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
   *
   * @since 1.0.0
   */
  public void refresh() {}

  /**
   * Factory that creates {@link NameResolver} instances.
   *
   * @since 1.0.0
   */
  public abstract static class Factory {
    /**
     * The port number used in case the target or the underlying naming system doesn't provide a
     * port number.
     *
     * @deprecated this will be deleted along with {@link #newNameResolver(URI, Attributes)} in
     *             a future release.
     *
     * @since 1.0.0
     */
    @Deprecated
    public static final Attributes.Key<Integer> PARAMS_DEFAULT_PORT =
        Attributes.Key.create("params-default-port");

    /**
     * If the NameResolver wants to support proxy, it should inquire this {@link ProxyDetector}.
     * See documentation on {@link ProxyDetector} about how proxies work in gRPC.
     *
     * @deprecated this will be deleted along with {@link #newNameResolver(URI, Attributes)} in
     *             a future release
     */
    @ExperimentalApi("https://github.com/grpc/grpc-java/issues/5113")
    @Deprecated
    public static final Attributes.Key<ProxyDetector> PARAMS_PROXY_DETECTOR =
        Attributes.Key.create("params-proxy-detector");

    /**
     * Creates a {@link NameResolver} for the given target URI, or {@code null} if the given URI
     * cannot be resolved by this factory. The decision should be solely based on the scheme of the
     * URI.
     *
     * @param targetUri the target URI to be resolved, whose scheme must not be {@code null}
     * @param params optional parameters. Canonical keys are defined as {@code PARAMS_*} fields in
     *               {@link Factory}.
     *
     * @deprecated Implement {@link #newNameResolver(URI, NameResolver.Helper)} instead.  This is
     *             going to be deleted in a future release.
     *
     * @since 1.0.0
     */
    @Nullable
    @Deprecated
    public NameResolver newNameResolver(URI targetUri, Attributes params) {
      throw new UnsupportedOperationException("This method is going to be deleted");
    }

    /**
     * Creates a {@link NameResolver} for the given target URI, or {@code null} if the given URI
     * cannot be resolved by this factory. The decision should be solely based on the scheme of the
     * URI.
     *
     * @param targetUri the target URI to be resolved, whose scheme must not be {@code null}
     * @param helper utility that may be used by the NameResolver implementation
     *
     * @since 1.19.0
     */
    // TODO(zhangkun83): make this abstract when the other override is deleted
    @Nullable
    public NameResolver newNameResolver(URI targetUri, Helper helper) {
      return newNameResolver(
          targetUri,
          Attributes.newBuilder()
              .set(PARAMS_DEFAULT_PORT, helper.getDefaultPort())
              .set(PARAMS_PROXY_DETECTOR, helper.getProxyDetector())
              .build());
    }

    /**
     * Returns the default scheme, which will be used to construct a URI when {@link
     * ManagedChannelBuilder#forTarget(String)} is given an authority string instead of a compliant
     * URI.
     *
     * @since 1.0.0
     */
    public abstract String getDefaultScheme();
  }

  /**
   * Receives address updates.
   *
   * <p>All methods are expected to return quickly.
   *
   * @deprecated use {@link Observer} instead.
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
  @ThreadSafe
  @Deprecated
  public interface Listener {
    /**
     * Handles updates on resolved addresses and attributes.
     *
     * <p>Implementations will not modify the given {@code servers}.
     *
     * @param servers the resolved server addresses. An empty list will trigger {@link #onError}
     * @param attributes extra information from naming system.
     * @since 1.3.0
     */
    void onAddresses(
        List<EquivalentAddressGroup> servers, @ResolutionResultAttr Attributes attributes);

    /**
     * Handles an error from the resolver. The listener is responsible for eventually invoking
     * {@link #refresh()} to re-attempt resolution.
     *
     * @param error a non-OK status
     * @since 1.0.0
     */
    void onError(Status error);
  }

  /**
   * Receives address updates.
   *
   * <p>All methods are expected to return quickly.
   *
   * @since 1.21.0
   */
  public abstract static class Observer implements Listener {
    /**
     * @deprecated This will be removed in 1.21.0
     */
    @Override
    @Deprecated
    public final void onAddresses(
        List<EquivalentAddressGroup> servers, @ResolutionResultAttr Attributes attributes) {
      onResult(ResolutionResult.newBuilder().setServers(servers).setAttributes(attributes).build());
    }

    /**
     * Handles updates on resolved addresses and attributes.  If
     * {@link ResolutionResult#getServers()} is empty, {@link #onError(Status)} will be called.
     *
     * @param resolutionResult the resolved server addresses, attributes, and Service Config.
     * @since 1.21.0
     */
    public abstract void onResult(ResolutionResult resolutionResult);

    /**
     * Handles an error from the resolver. The observer is responsible for eventually invoking
     * {@link NameResolver#refresh()} to re-attempt resolution.
     *
     * @param error a non-OK status
     * @since 1.21.0
     */
    @Override
    public abstract void onError(Status error);
  }

  /**
   * Annotation for name resolution result attributes. It follows the annotation semantics defined
   * by {@link Attributes}.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4972")
  @Retention(RetentionPolicy.SOURCE)
  @Documented
  public @interface ResolutionResultAttr {}

  /**
   * A utility object passed to {@link Factory#newNameResolver(URI, NameResolver.Helper)}.
   *
   * @since 1.19.0
   */
  public abstract static class Helper {
    /**
     * The port number used in case the target or the underlying naming system doesn't provide a
     * port number.
     *
     * @since 1.19.0
     */
    public abstract int getDefaultPort();

    /**
     * If the NameResolver wants to support proxy, it should inquire this {@link ProxyDetector}.
     * See documentation on {@link ProxyDetector} about how proxies work in gRPC.
     *
     * @since 1.19.0
     */
    public abstract ProxyDetector getProxyDetector();

    /**
     * Returns the {@link SynchronizationContext} where {@link #start(Observer)}, {@link #shutdown}
     * and {@link #refresh} are run from.
     *
     * @since 1.21.0
     */
    public SynchronizationContext getSynchronizationContext() {
      throw new UnsupportedOperationException("Not implemented");
    }

    /**
     * Parses and validates the service configuration chosen by the name resolver.  This will
     * return a {@link ConfigOrError} which contains either the successfully parsed config, or the
     * {@link Status} representing the failure to parse.  Implementations are expected to not throw
     * exceptions but return a Status representing the failure.  The value inside the
     * {@link ConfigOrError} should implement {@link Object#equals()} and {@link Object#hashCode()}.
     *
     * @param rawServiceConfig The {@link Map} representation of the service config
     * @return a tuple of the fully parsed and validated channel configuration, else the Status.
     * @since 1.20.0
     */
    public ConfigOrError parseServiceConfig(Map<String, ?> rawServiceConfig) {
      throw new UnsupportedOperationException("should have been implemented");
    }

    /**
     * Represents either a successfully parsed service config, containing all necessary parts to be
     * later applied by the channel, or a Status containing the error encountered while parsing.
     *
     * @since 1.20.0
     */
    public static final class ConfigOrError {

      private static final class UnknownConfig {

        UnknownConfig() {}

        @Override
        public String toString() {
          return "service config is unused";
        }
      }

      /**
       * A sentinel value indicating that service config is not supported.   This can be used to
       * indicate that parsing of the service config is neither right nor wrong, but doesn't have
       * any meaning.
       */
      public static final ConfigOrError UNKNOWN_CONFIG =
          ConfigOrError.fromConfig(new UnknownConfig());

      /**
       * Returns a {@link ConfigOrError} for the successfully parsed config.
       */
      public static ConfigOrError fromConfig(Object config) {
        return new ConfigOrError(config);
      }

      /**
       * Returns a {@link ConfigOrError} for the failure to parse the config.
       *
       * @param status a non-OK status
       */
      public static ConfigOrError fromError(Status status) {
        return new ConfigOrError(status);
      }

      private final Status status;
      private final Object config;

      private ConfigOrError(Object config) {
        this.config = checkNotNull(config, "config");
        this.status = null;
      }

      private ConfigOrError(Status status) {
        this.config = null;
        this.status = checkNotNull(status, "status");
        checkArgument(!status.isOk(), "cannot use OK status: %s", status);
      }

      /**
       * Returns config if exists, otherwise null.
       */
      @Nullable
      public Object getConfig() {
        return config;
      }

      /**
       * Returns error status if exists, otherwise null.
       */
      @Nullable
      public Status getError() {
        return status;
      }

      @Override
      public String toString() {
        if (config != null) {
          return MoreObjects.toStringHelper(this)
              .add("config", config)
              .toString();
        } else {
          assert status != null;
          return MoreObjects.toStringHelper(this)
              .add("error", status)
              .toString();
        }
      }
    }
  }

  /**
   * Represents the results from a Name Resolver.
   *
   * @since 1.21.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
  public static final class ResolutionResult {
    private final List<EquivalentAddressGroup> servers;
    @ResolutionResultAttr
    private final Attributes attributes;
    @Nullable
    private final Object serviceConfig;

    ResolutionResult(
        List<EquivalentAddressGroup> servers,
        @ResolutionResultAttr Attributes attributes,
        Object serviceConfig) {
      this.servers = Collections.unmodifiableList(new ArrayList<>(servers));
      this.attributes = checkNotNull(attributes, "attributes");
      this.serviceConfig = serviceConfig;
    }

    /**
     * Constructs a new builder of a name resolution result.
     *
     * @since 1.21.0
     */
    public static Builder newBuilder() {
      return new Builder();
    }

    /**
     * Converts these results back to a builder.
     *
     * @since 1.21.0
     */
    public Builder toBuilder() {
      return newBuilder()
          .setServers(servers)
          .setAttributes(attributes)
          .setServiceConfig(serviceConfig);
    }

    /**
     * Gets the servers resolved by name resolution.
     *
     * @since 1.21.0
     */
    public List<EquivalentAddressGroup> getServers() {
      return servers;
    }

    /**
     * Gets the attributes associated with the servers resolved by name resolution.  If there are
     * no attributes, {@link Attributes#EMPTY} will be returned.
     *
     * @since 1.21.0
     */
    @ResolutionResultAttr
    public Attributes getAttributes() {
      return attributes;
    }

    /**
     * Gets the Service Config parsed by {@link NameResolver.Helper#parseServiceConfig(Map)}.
     *
     * @since 1.21.0
     */
    @Nullable
    public Object getServiceConfig() {
      return serviceConfig;
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("servers", servers)
          .add("attributes", attributes)
          .add("serviceConfig", serviceConfig)
          .toString();
    }

    @Override
    public boolean equals(Object obj) {
      if (!(obj instanceof ResolutionResult)) {
        return false;
      }
      ResolutionResult that = (ResolutionResult) obj;
      return Objects.equal(this.servers, that.servers)
          && Objects.equal(this.attributes, that.attributes)
          && Objects.equal(this.serviceConfig, that.serviceConfig);
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(servers, attributes, serviceConfig);
    }

    /**
     * A builder for {@link ResolutionResult}.
     *
     * @since 1.21.0
     */
    public static final class Builder {
      private List<EquivalentAddressGroup> servers = Collections.emptyList();
      private Attributes attributes = Attributes.EMPTY;
      @Nullable
      private Object serviceConfig;
      //  Make sure to update #toBuilder above!

      Builder() {}

      /**
       * Sets the servers resolved by name resolution.  This field is required.
       *
       * @since 1.21.0
       */
      public Builder setServers(List<EquivalentAddressGroup> servers) {
        this.servers = servers;
        return this;
      }

      /**
       * Sets the attributes for the servers resolved by name resolution.  If unset,
       * {@link Attributes#EMPTY} will be used as a default.
       *
       * @since 1.21.0
       */
      public Builder setAttributes(Attributes attributes) {
        this.attributes = attributes;
        return this;
      }

      /**
       * Sets the Service Config parsed by {@link NameResolver.Helper#parseServiceConfig(Map)}.
       * This field is optional.
       *
       * @since 1.21.0
       */
      public Builder setServiceConfig(@Nullable Object serviceConfig) {
        this.serviceConfig = serviceConfig;
        return this;
      }

      /**
       * Constructs a new {@link ResolutionResult} from this builder.
       *
       * @since 1.21.0
       */
      public ResolutionResult build() {
        return new ResolutionResult(servers, attributes, serviceConfig);
      }
    }
  }
}
