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

import static grpc.testing.Qpstest.RpcType.STREAMING;
import static grpc.testing.Qpstest.RpcType.UNARY;
import static io.grpc.benchmarks.qps.ClientConfiguration.Builder.Option.HELP;
import static java.lang.Integer.parseInt;
import static java.lang.Math.max;
import static java.util.Arrays.asList;

import com.google.common.base.Strings;

import grpc.testing.Qpstest.PayloadType;
import grpc.testing.Qpstest.RpcType;
import io.grpc.transport.netty.NettyChannelBuilder;

import java.util.HashSet;
import java.util.LinkedHashSet;
import java.util.Set;

/**
 * Configuration options for the client implementations.
 */
class ClientConfiguration {

  // The histogram can record values between 1 microsecond and 1 min.
  static final long HISTOGRAM_MAX_VALUE = 60000000L;
  // Value quantization will be no larger than 1/10^3 = 0.1%.
  static final int HISTOGRAM_PRECISION = 3;

  boolean okhttp;
  boolean tls;
  boolean testca;
  boolean directExecutor;
  int port;
  int channels = 4;
  int outstandingRpcsPerChannel = 10;
  int serverPayload;
  int clientPayload;
  int connectionWindow = NettyChannelBuilder.DEFAULT_CONNECTION_WINDOW_SIZE;
  int streamWindow = NettyChannelBuilder.DEFAULT_CONNECTION_WINDOW_SIZE;
  // seconds
  int duration = 60;
  // seconds
  int warmupDuration = 10;
  int targetQps;
  String host;
  String histogramFile;
  RpcType rpcType = UNARY;
  PayloadType payloadType = PayloadType.COMPRESSABLE;

  private ClientConfiguration() {
  }

  static Builder newBuilder() {
    return new Builder();
  }

  static class Builder {

    private final Set<Option> options;

    private Builder() {
      options = new LinkedHashSet<Option>();
      options.add(HELP);
    }

    Builder addOptions(Option... opts) {
      options.addAll(asList(opts));
      return this;
    }

    void printUsage() {
      System.out.println("Usage: [ARGS...]");
      int maxWidth = 0;
      for (Option option : options) {
        maxWidth = max(commandLineFlag(option).length(), maxWidth);
      }
      // padding
      maxWidth += 2;
      for (Option option : options) {
        StringBuilder sb = new StringBuilder();
        sb.append(commandLineFlag(option))
          .append(Strings.repeat(" ", maxWidth - sb.length()))
          .append(option.description)
          .append(option.required ? " Required." : "");
        System.out.println("  " + sb);
      }
      System.out.println();
    }

    ClientConfiguration build(String[] args) {
      ClientConfiguration config = new ClientConfiguration();
      Set<Option> appliedOptions = new HashSet<Option>();

      for (String arg : args) {
        if (!arg.startsWith("--")) {
          throw new IllegalArgumentException("All arguments must start with '--': " + arg);
        }
        String[] pair = arg.substring(2).split("=", 2);
        String key = pair[0];
        String value = "";
        if (pair.length == 2) {
          value = pair[1];
        }
        for (Option option : options) {
          if (key.equals(option.toString())) {
            if (option != HELP) {
              option.action.applyNew(config, value);
              appliedOptions.add(option);
            } else {
              throw new RuntimeException("");
            }
          }
        }
      }
      for (Option option : options) {
        if (option.required && !appliedOptions.contains(option)) {
          throw new IllegalArgumentException("Missing required option '--" + option + "'.");
        }
      }
      return config;
    }

    private static String commandLineFlag(Option option) {
      return "--" + option + (option.type != "" ? '=' + option.type : "");
    }

    private interface Action {
      void applyNew(ClientConfiguration config, String value);
    }

    enum Option {
      HELP("", "Print this text.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
        }
      }),
      PORT("INT", "Port of the Server.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.port = parseInt(value);
        }
      }, true),
      HOST("STR", "Hostname or IP Address of the Server.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.host = value;
        }
      }, true),
      CHANNELS("INT", "Number of Channels.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.channels = parseInt(value);
        }
      }),
      OUTSTANDING_RPCS("INT", "Number of outstanding RPCs per Channel.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.outstandingRpcsPerChannel = parseInt(value);
        }
      }),
      CLIENT_PAYLOAD("BYTES", "Payload Size of the Request.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.clientPayload = parseInt(value);
        }
      }),
      SERVER_PAYLOAD("BYTES", "Payload Size of the Response.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.serverPayload = parseInt(value);
        }
      }),
      TLS("", "Enable TLS.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.tls = true;
        }
      }),
      TESTCA("", "Use the provided Test Certificate for TLS.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.testca = true;
        }
      }),
      OKHTTP("", "Use OkHttp as the Transport.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.okhttp = true;
        }
      }),
      DURATION("SECONDS", "Duration of the benchmark.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.duration = parseInt(value);
        }
      }),
      WARMUP_DURATION("SECONDS", "Warmup Duration of the benchmark.",
          new Action() {
            @Override
            public void applyNew(ClientConfiguration config, String value) {
              config.warmupDuration = parseInt(value);
            }
          }),
      DIRECTEXECUTOR("", "Don't use a threadpool for RPC calls, instead execute calls directly "
                         + "in the transport thread.",
          new Action() {
            @Override
            public void applyNew(ClientConfiguration config, String value) {
              config.directExecutor = true;
            }
          }),
      SAVE_HISTOGRAM("FILE", "Write the histogram with the latency recordings to file.",
          new Action() {
            @Override
            public void applyNew(ClientConfiguration config, String value) {
              config.histogramFile = value;
            }
          }),
      STREAMING_RPCS("", "Use Streaming RPCs.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.rpcType = STREAMING;
        }
      }),
      CONNECTION_WINDOW("BYTES", "The HTTP/2 connection flow control window.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.connectionWindow = parseInt(value);
        }
      }),
      STREAM_WINDOW("BYTES", "The HTTP/2 per-stream flow control window.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.streamWindow = parseInt(value);
        }
      }),
      TARGET_QPS("INT", "Average number of QPS to shoot for.", new Action() {
        @Override
        public void applyNew(ClientConfiguration config, String value) {
          config.targetQps = parseInt(value);
        }
      }, true);

      private final String type;
      private final String description;
      private final Action action;
      private final boolean required;

      Option(String type, String description, Action action) {
        this(type, description, action, false);
      }

      Option(String type, String description, Action action, boolean required) {
        this.type = type;
        this.description = description;
        this.action = action;
        this.required = required;
      }

      @Override
      public String toString() {
        return name().toLowerCase();
      }
    }
  }
}
