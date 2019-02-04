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

package io.grpc.testing.integration;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.internal.GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import com.google.protobuf.ByteString;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientCall.Listener;
import io.grpc.ClientInterceptor;
import io.grpc.Codec;
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
import io.grpc.stub.StreamObserver;
import io.grpc.testing.integration.Messages.Payload;
import io.grpc.testing.integration.Messages.SimpleRequest;
import io.grpc.testing.integration.Messages.SimpleResponse;
import io.grpc.testing.integration.TestServiceGrpc.TestServiceBlockingStub;
import io.grpc.testing.integration.TransportCompressionTest.Fzip;
import java.nio.charset.Charset;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.Parameterized;
import org.junit.runners.Parameterized.Parameters;

/**
 * Tests for compression configurations.
 *
 * <p>Because of the asymmetry of clients and servers, clients will not know what decompression
 * methods the server supports.  In cases where the client is willing to encode, and the server
 * is willing to decode, a second RPC is sent to show that the client has learned and will use
 * the encoding.
 *
 * <p>In cases where compression is negotiated, but either the client or the server doesn't
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

  private Fzip clientCodec = new Fzip("fzip", Codec.Identity.NONE);
  private Fzip serverCodec = new Fzip("fzip", Codec.Identity.NONE);
  private DecompressorRegistry clientDecompressors = DecompressorRegistry.emptyInstance();
  private DecompressorRegistry serverDecompressors = DecompressorRegistry.emptyInstance();
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
    clientDecompressors = clientDecompressors.with(Codec.Identity.NONE, false);
    serverDecompressors = serverDecompressors.with(Codec.Identity.NONE, false);
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
    boolean[] bools = new boolean[]{false, true};
    List<Object[]> combos = new ArrayList<>(64);
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
  public void compression() throws Exception {
    if (clientAcceptEncoding) {
      clientDecompressors = clientDecompressors.with(clientCodec, true);
    }
    if (clientEncoding) {
      clientCompressors.register(clientCodec);
    }
    if (serverAcceptEncoding) {
      serverDecompressors = serverDecompressors.with(serverCodec, true);
    }
    if (serverEncoding) {
      serverCompressors.register(serverCodec);
    }

    server = ServerBuilder.forPort(0)
        .addService(
            ServerInterceptors.intercept(new LocalServer(), new ServerCompressorInterceptor()))
        .compressorRegistry(serverCompressors)
        .decompressorRegistry(serverDecompressors)
        .build()
        .start();

    channel = ManagedChannelBuilder.forAddress("localhost", server.getPort())
        .decompressorRegistry(clientDecompressors)
        .compressorRegistry(clientCompressors)
        .intercept(new ClientCompressorInterceptor())
        .usePlaintext()
        .build();
    stub = TestServiceGrpc.newBlockingStub(channel);

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
      // Either identity or null is accepted.
      assertThat(clientResponseHeaders.get(MESSAGE_ENCODING_KEY))
          .isAnyOf(Codec.Identity.NONE.getMessageEncoding(), null);
      assertFalse(clientCodec.anyRead);
      assertFalse(serverCodec.anyWritten);
    }

    if (serverAcceptEncoding) {
      assertEqualsString("fzip", clientResponseHeaders.get(MESSAGE_ACCEPT_ENCODING_KEY));
    } else {
      assertNull(clientResponseHeaders.get(MESSAGE_ACCEPT_ENCODING_KEY));
    }

    if (clientAcceptEncoding) {
      assertEqualsString("fzip", serverResponseHeaders.get(MESSAGE_ACCEPT_ENCODING_KEY));
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

  private static final class LocalServer extends TestServiceGrpc.TestServiceImplBase {
    @Override
    public void unaryCall(SimpleRequest request, StreamObserver<SimpleResponse> responseObserver) {
      responseObserver.onNext(SimpleResponse.newBuilder()
          .setPayload(Payload.newBuilder()
              .setBody(ByteString.copyFrom(new byte[]{127})))
          .build());
      responseObserver.onCompleted();
    }
  }

  private class ServerCompressorInterceptor implements ServerInterceptor {
    @Override
    public <ReqT, RespT> io.grpc.ServerCall.Listener<ReqT> interceptCall(
        ServerCall<ReqT, RespT> call, Metadata headers, ServerCallHandler<ReqT, RespT> next) {
      if (serverEncoding) {
        call.setCompression("fzip");
      }
      call.setMessageCompression(enableServerMessageCompression);
      Metadata headersCopy = new Metadata();
      headersCopy.merge(headers);
      serverResponseHeaders = headersCopy;
      return next.startCall(call, headers);
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

      return new ClientCompressor<>(call);
    }
  }

  private class ClientCompressor<ReqT, RespT> extends SimpleForwardingClientCall<ReqT, RespT> {
    protected ClientCompressor(ClientCall<ReqT, RespT> delegate) {
      super(delegate);
    }

    @Override
    public void start(io.grpc.ClientCall.Listener<RespT> responseListener, Metadata headers) {
      super.start(new ClientHeadersCapture<>(responseListener), headers);
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
      Metadata headersCopy = new Metadata();
      headersCopy.merge(headers);
      clientResponseHeaders = headersCopy;
    }
  }

  private static void assertEqualsString(String expected, byte[] actual) {
    assertEquals(expected, new String(actual, Charset.forName("US-ASCII")));
  }
}
