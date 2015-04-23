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

import static java.lang.Math.max;

import grpc.testing.Qpstest.PayloadType;
import grpc.testing.Qpstest.RpcType;
import io.grpc.transport.netty.NettyChannelBuilder;

class ClientConfiguration {

  boolean okhttp;
  boolean tls;
  boolean testca;
  boolean directExecutor;
  boolean dumpHistogram;
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
  String host = "127.0.0.1";
  String histogramFile = "latencies.hgrm";
  RpcType rpcType = RpcType.UNARY;
  PayloadType payloadType = PayloadType.COMPRESSABLE;

  private ClientConfiguration() {
  }

  static ClientConfiguration parseArgs(String[] args) {
    ClientConfiguration c = new ClientConfiguration();
    boolean hasPort = false;

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

      if ("help".equals(key)) {
        printUsage();
        return null;
      } else if ("port".equals(key)) {
        c.port = Integer.parseInt(value);
        hasPort = true;
      } else if ("host".equals(key)) {
        c.host = value;
      } else if ("channels".equals(key)) {
        c.channels = max(Integer.parseInt(value), 1);
      } else if ("outstanding_rpcs_per_channel".equals(key)) {
        c.outstandingRpcsPerChannel = max(Integer.parseInt(value), 1);
      } else if ("client_payload".equals(key)) {
        c.clientPayload = max(Integer.parseInt(value), 0);
      } else if ("server_payload".equals(key)) {
        c.serverPayload = max(Integer.parseInt(value), 0);
      } else if ("tls".equals(key)) {
        c.tls = true;
      } else if ("testca".equals(key)) {
        c.testca = true;
      } else if ("okhttp".equals(key)) {
        c.okhttp = true;
      } else if ("duration".equals(key)) {
        c.duration = parseDuration(value);
      } else if ("warmup_duration".equals(key)) {
        c.warmupDuration = parseDuration(value);
      } else if ("directexecutor".equals(key)) {
        c.directExecutor = true;
      } else if ("dump_histogram".equals(key)) {
        c.dumpHistogram = true;
        if (!value.isEmpty()) {
          c.histogramFile = value;
        }
      } else if ("streaming_rpcs".equals(key)) {
        c.rpcType = RpcType.STREAMING;
      } else if ("connection_window".equals(key)) {
        c.connectionWindow = Integer.parseInt(value);
      } else if ("stream_window".equals(key)) {
        c.streamWindow = Integer.parseInt(value);
      } else {
        throw new IllegalArgumentException("Unrecognized argument '" + key + "'.");
      }
    }

    if (!hasPort) {
      throw new IllegalArgumentException("'--port' was not specified.");
    }
    return c;
  }

  static void printUsage() {
    ClientConfiguration c = new ClientConfiguration();
    System.out.println(
        "Usage: [ARGS...]"
            + "\n"
            + "\n  --port=INT                           Port of the server. Required. No default."
            + "\n  --host=STR                           Hostname or IP of the server. Default "
            +                                           c.host
            + "\n  --channels=INT                       Number of channels. Default "
            +                                           c.channels
            + "\n  --outstanding_rpcs_per_channel=INT   Number of outstanding RPCs per channel. "
            + "\n                                       Default " + c.outstandingRpcsPerChannel
            + "\n  --client_payload=BYTES               Request payload size in bytes. Default "
            +                                           c.clientPayload
            + "\n  --server_payload=BYTES               Response payload size in bytes. Default "
            +                                           c.serverPayload
            + "\n  --tls                                Enable TLS. Default disabled."
            + "\n  --testca                             Use the provided test certificate for TLS."
            + "\n  --okhttp                             Use OkHttp as the transport. Default netty"
            + "\n  --duration=TIME                      Duration of the benchmark in either seconds"
            + "\n                                       or minutes."
            + "\n                                       For N seconds duration specify Ns and for"
            + "\n                                       minutes Nm. Default " + c.duration + "s."
            + "\n  --warmup_duration=TIME               How long to warmup."
            + "\n                                       Default " + c.warmupDuration + "s."
            + "\n  --directexecutor                     Use a direct executor i.e. execute all RPC"
            + "\n                                       calls directly in Netty's event loop"
            + "\n                                       without the overhead of a thread pool."
            + "\n  --dump_histogram=FILENAME            Write the histogram with the latency"
            + "\n                                       recordings to file. The default filename"
            + "\n                                       is 'latencies.hgrm'."
            + "\n  --streaming_rpcs                     Use streaming RPCs. Default unary RPCs."
            + "\n  --connection_window=BYTES            The HTTP/2 connection flow control window."
            + "\n                                       Default " + c.connectionWindow + " byte."
            + "\n  --stream_window=BYTES                The HTTP/2 per-stream flow control window."
            + "\n                                       Default " + c.streamWindow + " byte."
    );
  }

  private static int parseDuration(String value) {
    if (value == null || value.length() < 2) {
      throw new IllegalArgumentException("value must be a number followed by a unit.");
    }
    char last = value.charAt(value.length() - 1);
    int duration = Integer.parseInt(value.substring(0, value.length() - 1));
    if (last == 's') {
      return duration;
    } else if (last == 'm') {
      return duration * 60;
    } else {
      throw new IllegalArgumentException("Unknown unit " + last);
    }
  }
}
