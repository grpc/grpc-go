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

package io.grpc.testing.integration;

import static io.grpc.internal.GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientCall.Listener;
import io.grpc.ClientInterceptor;
import io.grpc.CompressorRegistry;
import io.grpc.DecompressorRegistry;
import io.grpc.ForwardingClientCall.SimpleForwardingClientCall;
import io.grpc.ForwardingClientCallListener.SimpleForwardingClientCallListener;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerInterceptors;
import io.grpc.testing.TestUtils;
import io.grpc.testing.integration.Messages.SimpleRequest;
import io.grpc.testing.integration.TestServiceGrpc.TestServiceBlockingStub;
import io.grpc.testing.integration.TransportCompressionTest.Fzip;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.Parameterized;
import org.junit.runners.Parameterized.Parameters;

import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;

/**
 * Tests for compression configurations.
 *
 * <p>Because of the asymmetry of clients and servers, clients will not know what decompression
 * methods the server supports.  In cases where the client is willing to encode, and the server
 * is willing to decode, a second RPC is sent to show that the client has learned and will use
 * the encoding.
 *
 * <p> In cases where compression is negotiated, but either the client or the server doesn't
 * actually want to encode, a dummy codec is used to record usage.  If compression is not enabled,
 * the codec will see no data pass through.  This is checked on each test to ensure the code is
 * doing the right thing.
 */
@RunWith(Parameterized.class)
public class CompressionTest {
  private static final ScheduledExecutorService executor = Executors.newScheduledThreadPool(2);
  // Ensures that both the request and response messages are more than 0 bytes.  The framer/deframer
  // may not use the compressor if the message is empty.
  private static final SimpleRequest REQUEST = SimpleRequest.newBuilder()
      .setResponseSize(1)
      .build();

  private Fzip clientCodec = new Fzip();
  private Fzip serverCodec = new Fzip();
  private DecompressorRegistry clientDecompressors = DecompressorRegistry.newEmptyInstance();
  private DecompressorRegistry serverDecompressors = DecompressorRegistry.newEmptyInstance();
  private CompressorRegistry clientCompressors = CompressorRegistry.newEmptyInstance();
  private CompressorRegistry serverCompressors = CompressorRegistry.newEmptyInstance();

  /** The headers received by the server from the client. */
  private volatile Metadata serverResponseHeaders;
  /** The headers received by the client from the server. */
  private volatile Metadata clientResponseHeaders;

  // Params
  private final boolean enableClientMessageCompression;
  private final boolean enableServerMessageCompression;
  private final boolean clientAcceptEncoding;
  private final boolean clientEncoding;
  private final boolean serverAcceptEncoding;
  private final boolean serverEncoding;

  private Server server;
  private ManagedChannel channel;
  private TestServiceBlockingStub stub;

  /**
   * Auto called by test.
   */
  public CompressionTest(
      boolean enableClientMessageCompression,
      boolean clientAcceptEncoding,
      boolean clientEncoding,
      boolean enableServerMessageCompression,
      boolean serverAcceptEncoding,
      boolean serverEncoding) {
    this.enableClientMessageCompression = enableClientMessageCompression;
    this.clientAcceptEncoding = clientAcceptEncoding;
    this.clientEncoding = clientEncoding;
    this.enableServerMessageCompression = enableServerMessageCompression;
    this.serverAcceptEncoding = serverAcceptEncoding;
    this.serverEncoding = serverEncoding;
  }

  @Before
  public void setUp() throws Exception {
    int serverPort = TestUtils.pickUnusedPort();
    server = ServerBuilder.forPort(serverPort)
        .addService(ServerInterceptors.intercept(
            TestServiceGrpc.bindService(new TestServiceImpl(executor)),
            new ServerCompressorInterceptor()))
        .compressorRegistry(serverCompressors)
        .decompressorRegistry(serverDecompressors)
        .build()
        .start();

    channel = ManagedChannelBuilder.forAddress("localhost", serverPort)
        .decompressorRegistry(clientDecompressors)
        .compressorRegistry(clientCompressors)
        .intercept(new ClientCompressorInterceptor())
        .usePlaintext(true)
        .build();
    stub = TestServiceGrpc.newBlockingStub(channel);
  }

  @After
  public void tearDown() {
    channel.shutdownNow();
    server.shutdownNow();
    executor.shutdownNow();
  }

  /**
   * Parameters for test.
   */
  @Parameters
  public static Collection<Object[]> params() {
    Boolean[] bools = new Boolean[]{false, true};
    List<Object[]> combos = new ArrayList<Object[]>(64);
    for (boolean enableClientMessageCompression : bools) {
      for (boolean clientAcceptEncoding : bools) {
        for (boolean clientEncoding : bools) {
          for (boolean enableServerMessageCompression : bools) {
            for (boolean serverAcceptEncoding : bools) {
              for (boolean serverEncoding : bools) {
                combos.add(new Object[] {
                    enableClientMessageCompression, clientAcceptEncoding, clientEncoding,
                    enableServerMessageCompression, serverAcceptEncoding, serverEncoding});
              }
            }
          }
        }
      }
    }
    return combos;
  }

  @Test
  public void compression() {
    if (clientAcceptEncoding) {
      clientDecompressors.register(clientCodec, true);
    }
    if (clientEncoding) {
      clientCompressors.register(clientCodec);
    }
    if (serverAcceptEncoding) {
      serverDecompressors.register(serverCodec, true);
    }
    if (serverEncoding) {
      serverCompressors.register(serverCodec);
    }

    stub.unaryCall(REQUEST);

    if (clientAcceptEncoding && serverEncoding) {
      assertEquals("fzip", clientResponseHeaders.get(MESSAGE_ENCODING_KEY));
      if (enableServerMessageCompression) {
        assertTrue(clientCodec.anyRead);
        assertTrue(serverCodec.anyWritten);
      } else {
        assertFalse(clientCodec.anyRead);
        assertFalse(serverCodec.anyWritten);
      }
    } else {
      assertNull(clientResponseHeaders.get(MESSAGE_ENCODING_KEY));
      assertFalse(clientCodec.anyRead);
      assertFalse(serverCodec.anyWritten);
    }

    if (serverAcceptEncoding) {
      assertEquals("fzip", clientResponseHeaders.get(MESSAGE_ACCEPT_ENCODING_KEY));
    } else {
      assertNull(clientResponseHeaders.get(MESSAGE_ACCEPT_ENCODING_KEY));
    }

    if (clientAcceptEncoding) {
      assertEquals("fzip", serverResponseHeaders.get(MESSAGE_ACCEPT_ENCODING_KEY));
    } else {
      assertNull(serverResponseHeaders.get(MESSAGE_ACCEPT_ENCODING_KEY));
    }

    // Second call, once the client knows what the server supports.
    if (clientEncoding && serverAcceptEncoding) {
      assertEquals("fzip", serverResponseHeaders.get(MESSAGE_ENCODING_KEY));
      if (enableClientMessageCompression) {
        assertTrue(clientCodec.anyWritten);
        assertTrue(serverCodec.anyRead);
      } else {
        assertFalse(clientCodec.anyWritten);
        assertFalse(serverCodec.anyRead);
      }
    } else {
      assertNull(serverResponseHeaders.get(MESSAGE_ENCODING_KEY));
      assertFalse(clientCodec.anyWritten);
      assertFalse(serverCodec.anyRead);
    }
  }

  private class ServerCompressorInterceptor implements ServerInterceptor {
    @Override
    public <ReqT, RespT> io.grpc.ServerCall.Listener<ReqT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, ServerCall<RespT> call, Metadata headers,
        ServerCallHandler<ReqT, RespT> next) {
      call.setMessageCompression(enableServerMessageCompression);
      serverResponseHeaders = headers;
      return next.startCall(method, call, headers);
    }
  }

  private class ClientCompressorInterceptor implements ClientInterceptor {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      if (clientEncoding && serverAcceptEncoding) {
        callOptions = callOptions.withCompression("fzip");
      }
      ClientCall<ReqT, RespT> call = next.newCall(method, callOptions);

      return new ClientCompressor<ReqT, RespT>(call);
    }
  }

  private class ClientCompressor<ReqT, RespT> extends SimpleForwardingClientCall<ReqT, RespT> {
    protected ClientCompressor(ClientCall<ReqT, RespT> delegate) {
      super(delegate);
    }

    @Override
    public void start(io.grpc.ClientCall.Listener<RespT> responseListener, Metadata headers) {
      super.start(new ClientHeadersCapture<RespT>(responseListener), headers);
      setMessageCompression(enableClientMessageCompression);
    }
  }

  private class ClientHeadersCapture<RespT> extends SimpleForwardingClientCallListener<RespT> {
    private ClientHeadersCapture(Listener<RespT> delegate) {
      super(delegate);
    }

    @Override
    public void onHeaders(Metadata headers) {
      super.onHeaders(headers);
      clientResponseHeaders = headers;
    }
  }
}

