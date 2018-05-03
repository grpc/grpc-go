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

import static io.grpc.benchmarks.Utils.parseBoolean;
import static java.lang.Integer.parseInt;

import io.grpc.benchmarks.SocketAddressValidator;
import io.grpc.benchmarks.Utils;
import io.grpc.netty.NettyChannelBuilder;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collection;
import java.util.Collections;
import java.util.List;

/**
 * Configuration options for benchmark servers.
 */
class ServerConfiguration implements Configuration {
  private static final ServerConfiguration DEFAULT = new ServerConfiguration();

  Transport transport = Transport.NETTY_NIO;
  boolean tls;
  boolean directExecutor;
  SocketAddress address;
  int flowControlWindow = NettyChannelBuilder.DEFAULT_FLOW_CONTROL_WINDOW;

  private ServerConfiguration() {
  }

  static Builder newBuilder() {
    return new Builder();
  }

  static class Builder extends AbstractConfigurationBuilder<ServerConfiguration> {
    private static final List<Param> PARAMS = supportedParams();

    private Builder() {
    }

    @Override
    protected ServerConfiguration newConfiguration() {
      return new ServerConfiguration();
    }

    @Override
    protected Collection<Param> getParams() {
      return PARAMS;
    }

    @Override
    protected ServerConfiguration build0(ServerConfiguration config) {
      if (config.tls && !config.transport.tlsSupported) {
        throw new IllegalArgumentException(
            "TLS unsupported with the " + config.transport.name().toLowerCase() + " transport");
      }

      // Verify that the address type is correct for the transport type.
      config.transport.validateSocketAddress(config.address);
      return config;
    }

    private static List<Param> supportedParams() {
      return Collections.unmodifiableList(new ArrayList<Param>(
          Arrays.asList(ServerParam.values())));
    }
  }

  /**
   * All of the supported transports.
   */
  public enum Transport {
    NETTY_NIO(true, "The Netty Java NIO transport. Using this with TLS requires "
        + "that the Java bootclasspath be configured with Jetty ALPN boot.",
        SocketAddressValidator.INET),
    NETTY_EPOLL(true, "The Netty native EPOLL transport. Using this with TLS requires that "
        + "OpenSSL be installed and configured as described in "
        + "http://netty.io/wiki/forked-tomcat-native.html. Only supported on Linux.",
        SocketAddressValidator.INET),
    NETTY_UNIX_DOMAIN_SOCKET(false, "The Netty Unix Domain Socket transport. This currently "
        + "does not support TLS.",
        SocketAddressValidator.UDS);

    private final boolean tlsSupported;
    private final String description;
    private final SocketAddressValidator socketAddressValidator;

    Transport(boolean tlsSupported, String description,
              SocketAddressValidator socketAddressValidator) {
      this.tlsSupported = tlsSupported;
      this.description = description;
      this.socketAddressValidator = socketAddressValidator;
    }

    /**
     * Validates the given address for this transport.
     *
     * @throws IllegalArgumentException if the given address is invalid for this transport.
     */
    void validateSocketAddress(SocketAddress address) {
      if (!socketAddressValidator.isValidSocketAddress(address)) {
        throw new IllegalArgumentException(
            "Invalid address " + address + " for transport " + this);
      }
    }

    static String getDescriptionString() {
      StringBuilder builder = new StringBuilder("Select the transport to use. Options:\n");
      boolean first = true;
      for (Transport transport : Transport.values()) {
        if (!first) {
          builder.append("\n");
        }
        builder.append(transport.name().toLowerCase());
        builder.append(": ");
        builder.append(transport.description);
        first = false;
      }
      return builder.toString();
    }
  }

  enum ServerParam implements AbstractConfigurationBuilder.Param {
    ADDRESS("STR", "Socket address (host:port) or Unix Domain Socket file name "
        + "(unix:///path/to/file), depending on the transport selected.", null, true) {
      @Override
      protected void setServerValue(ServerConfiguration config, String value) {
        SocketAddress address = Utils.parseSocketAddress(value);
        if (address instanceof InetSocketAddress) {
          InetSocketAddress addr = (InetSocketAddress) address;
          int port = addr.getPort() == 0 ? Utils.pickUnusedPort() : addr.getPort();
          // Re-create the address so that the server is available on all local addresses.
          address = new InetSocketAddress(port);
        }
        config.address = address;
      }
    },
    TLS("", "Enable TLS.", "" + DEFAULT.tls) {
      @Override
      protected void setServerValue(ServerConfiguration config, String value) {
        config.tls = parseBoolean(value);
      }
    },
    TRANSPORT("STR", Transport.getDescriptionString(), DEFAULT.transport.name().toLowerCase()) {
      @Override
      protected void setServerValue(ServerConfiguration config, String value) {
        config.transport = Transport.valueOf(value.toUpperCase());
      }
    },
    DIRECTEXECUTOR("", "Don't use a threadpool for RPC calls, instead execute calls directly "
        + "in the transport thread.", "" + DEFAULT.directExecutor) {
      @Override
      protected void setServerValue(ServerConfiguration config, String value) {
        config.directExecutor = parseBoolean(value);
      }
    },
    FLOW_CONTROL_WINDOW("BYTES", "The HTTP/2 flow control window.",
        "" + DEFAULT.flowControlWindow) {
      @Override
      protected void setServerValue(ServerConfiguration config, String value) {
        config.flowControlWindow = parseInt(value);
      }
    };

    private final String type;
    private final String description;
    private final String defaultValue;
    private final boolean required;

    ServerParam(String type, String description, String defaultValue) {
      this(type, description, defaultValue, false);
    }

    ServerParam(String type, String description, String defaultValue, boolean required) {
      this.type = type;
      this.description = description;
      this.defaultValue = defaultValue;
      this.required = required;
    }

    @Override
    public String getName() {
      return name().toLowerCase();
    }

    @Override
    public String getType() {
      return type;
    }

    @Override
    public String getDescription() {
      return description;
    }

    @Override
    public String getDefaultValue() {
      return defaultValue;
    }

    @Override
    public boolean isRequired() {
      return required;
    }

    @Override
    public void setValue(Configuration config, String value) {
      setServerValue((ServerConfiguration) config, value);
    }

    protected abstract void setServerValue(ServerConfiguration config, String value);
  }
}
