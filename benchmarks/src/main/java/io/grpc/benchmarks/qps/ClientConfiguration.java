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

package io.grpc.benchmarks.qps;

import static io.grpc.benchmarks.Utils.parseBoolean;
import static java.lang.Integer.parseInt;
import static java.util.Arrays.asList;

import io.grpc.ManagedChannel;
import io.grpc.benchmarks.Transport;
import io.grpc.benchmarks.Utils;
import io.grpc.benchmarks.proto.Control.RpcType;
import io.grpc.benchmarks.proto.Messages;
import io.grpc.benchmarks.proto.Messages.PayloadType;
import io.grpc.testing.TestUtils;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.Collection;
import java.util.Collections;
import java.util.LinkedHashSet;
import java.util.Set;

/**
 * Configuration options for benchmark clients.
 */
public class ClientConfiguration implements Configuration {
  private static final ClientConfiguration DEFAULT = new ClientConfiguration();

  Transport transport = Transport.NETTY_NIO;
  boolean tls;
  boolean testca;
  String authorityOverride = TestUtils.TEST_SERVER_HOST;
  boolean useDefaultCiphers;
  boolean directExecutor;
  SocketAddress address;
  int channels = 4;
  int outstandingRpcsPerChannel = 10;
  int serverPayload;
  int clientPayload;
  int flowControlWindow = Utils.DEFAULT_FLOW_CONTROL_WINDOW;
  // seconds
  int duration = 60;
  // seconds
  int warmupDuration = 10;
  int targetQps;
  String histogramFile;
  RpcType rpcType = RpcType.UNARY;
  PayloadType payloadType = PayloadType.COMPRESSABLE;

  private ClientConfiguration() {
  }

  public ManagedChannel newChannel() throws IOException {
    return Utils.newClientChannel(transport, address, tls, testca, authorityOverride,
        useDefaultCiphers, flowControlWindow, directExecutor);
  }

  public Messages.SimpleRequest newRequest() {
    return Utils.makeRequest(payloadType, clientPayload, serverPayload);
  }

  /**
   * Constructs a builder for configuring a client application with supported parameters. If no
   * parameters are provided, all parameters are assumed to be supported.
   */
  static Builder newBuilder(ClientParam... supportedParams) {
    return new Builder(supportedParams);
  }

  static final class Builder extends AbstractConfigurationBuilder<ClientConfiguration> {
    private final Collection<Param> supportedParams;

    private Builder(ClientParam... supportedParams) {
      this.supportedParams = supportedOptionsSet(supportedParams);
    }

    @Override
    protected ClientConfiguration newConfiguration() {
      return new ClientConfiguration();
    }

    @Override
    protected Collection<Param> getParams() {
      return supportedParams;
    }

    @Override
    protected ClientConfiguration build0(ClientConfiguration config) {
      if (config.tls) {
        if (!config.transport.tlsSupported) {
          throw new IllegalArgumentException(
              "Transport " + config.transport.name().toLowerCase() + " does not support TLS.");
        }

        if (config.transport != Transport.OK_HTTP
            && config.testca && config.address instanceof InetSocketAddress) {
          // Override the socket address with the host from the testca.
          InetSocketAddress address = (InetSocketAddress) config.address;
          config.address = TestUtils.testServerAddress(address.getHostName(),
                  address.getPort());
        }
      }

      // Verify that the address type is correct for the transport type.
      config.transport.validateSocketAddress(config.address);

      return config;
    }

    private static Set<Param> supportedOptionsSet(ClientParam... supportedParams) {
      if (supportedParams.length == 0) {
        // If no options are supplied, default to including all options.
        supportedParams = ClientParam.values();
      }
      return Collections.unmodifiableSet(new LinkedHashSet<Param>(asList(supportedParams)));
    }
  }

  enum ClientParam implements AbstractConfigurationBuilder.Param {
    ADDRESS("STR", "Socket address (host:port) or Unix Domain Socket file name "
        + "(unix:///path/to/file), depending on the transport selected.", null, true) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.address = Utils.parseSocketAddress(value);
      }
    },
    CHANNELS("INT", "Number of Channels.", "" + DEFAULT.channels) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.channels = parseInt(value);
      }
    },
    OUTSTANDING_RPCS("INT", "Number of outstanding RPCs per Channel.",
        "" + DEFAULT.outstandingRpcsPerChannel) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.outstandingRpcsPerChannel = parseInt(value);
      }
    },
    CLIENT_PAYLOAD("BYTES", "Payload Size of the Request.", "" + DEFAULT.clientPayload) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.clientPayload = parseInt(value);
      }
    },
    SERVER_PAYLOAD("BYTES", "Payload Size of the Response.", "" + DEFAULT.serverPayload) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.serverPayload = parseInt(value);
      }
    },
    TLS("", "Enable TLS.", "" + DEFAULT.tls) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.tls = parseBoolean(value);
      }
    },
    TESTCA("", "Use the provided Test Certificate for TLS.", "" + DEFAULT.testca) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.testca = parseBoolean(value);
      }
    },
    USE_DEFAULT_CIPHERS("", "Use the default JDK ciphers for TLS (Used to support Java 7).",
            "" + DEFAULT.useDefaultCiphers) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.useDefaultCiphers = parseBoolean(value);
      }
    },
    TRANSPORT("STR", Transport.getDescriptionString(), DEFAULT.transport.name().toLowerCase()) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.transport = Transport.valueOf(value.toUpperCase());
      }
    },
    DURATION("SECONDS", "Duration of the benchmark.", "" + DEFAULT.duration) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.duration = parseInt(value);
      }
    },
    WARMUP_DURATION("SECONDS", "Warmup Duration of the benchmark.", "" + DEFAULT.warmupDuration) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.warmupDuration = parseInt(value);
      }
    },
    DIRECTEXECUTOR("",
        "Don't use a threadpool for RPC calls, instead execute calls directly "
            + "in the transport thread.", "" + DEFAULT.directExecutor) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.directExecutor = parseBoolean(value);
      }
    },
    SAVE_HISTOGRAM("FILE", "Write the histogram with the latency recordings to file.", null) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.histogramFile = value;
      }
    },
    STREAMING_RPCS("", "Use Streaming RPCs.", "false") {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.rpcType = RpcType.STREAMING;
      }
    },
    FLOW_CONTROL_WINDOW("BYTES", "The HTTP/2 flow control window.",
        "" + DEFAULT.flowControlWindow) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.flowControlWindow = parseInt(value);
      }
    },
    TARGET_QPS("INT", "Average number of QPS to shoot for.", "" + DEFAULT.targetQps, true) {
      @Override
      protected void setClientValue(ClientConfiguration config, String value) {
        config.targetQps = parseInt(value);
      }
    };

    private final String type;
    private final String description;
    private final String defaultValue;
    private final boolean required;

    ClientParam(String type, String description, String defaultValue) {
      this(type, description, defaultValue, false);
    }

    ClientParam(String type, String description, String defaultValue, boolean required) {
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
      setClientValue((ClientConfiguration) config, value);
    }

    protected abstract void setClientValue(ClientConfiguration config, String value);
  }
}
