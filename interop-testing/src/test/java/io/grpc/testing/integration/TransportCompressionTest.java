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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import com.google.protobuf.ByteString;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.Codec;
import io.grpc.CompressorRegistry;
import io.grpc.DecompressorRegistry;
import io.grpc.ForwardingClientCall;
import io.grpc.ForwardingClientCallListener;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCall.Listener;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.internal.GrpcUtil;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.testing.integration.Messages.CompressionType;
import io.grpc.testing.integration.Messages.Payload;
import io.grpc.testing.integration.Messages.PayloadType;
import io.grpc.testing.integration.Messages.SimpleRequest;
import io.grpc.testing.integration.Messages.SimpleResponse;
import java.io.FilterInputStream;
import java.io.FilterOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import org.junit.AfterClass;
import org.junit.Before;
import org.junit.BeforeClass;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests that compression is turned on.
 */
@RunWith(JUnit4.class)
public class TransportCompressionTest extends AbstractInteropTest {

  // Masquerade as identity.
  private static final Fzip FZIPPER = new Fzip("gzip", new Codec.Gzip());
  private volatile boolean expectFzip;

  private static final DecompressorRegistry decompressors = DecompressorRegistry.emptyInstance()
      .with(Codec.Identity.NONE, false)
      .with(FZIPPER, true);
  private static final CompressorRegistry compressors = CompressorRegistry.newEmptyInstance();

  @Before
  public void beforeTests() {
    FZIPPER.anyRead = false;
    FZIPPER.anyWritten = false;
  }

  /** Start server. */
  @BeforeClass
  public static void startServer() {
    compressors.register(FZIPPER);
    compressors.register(Codec.Identity.NONE);
    startStaticServer(
        NettyServerBuilder.forPort(0)
            .maxMessageSize(AbstractInteropTest.MAX_MESSAGE_SIZE)
            .compressorRegistry(compressors)
            .decompressorRegistry(decompressors),
        new ServerInterceptor() {
          @Override
          public <ReqT, RespT> Listener<ReqT> interceptCall(ServerCall<ReqT, RespT> call,
              Metadata headers, ServerCallHandler<ReqT, RespT> next) {
            Listener<ReqT> listener = next.startCall(call, headers);
            // TODO(carl-mastrangelo): check that encoding was set.
            call.setMessageCompression(true);
            return listener;
          }
        });
  }

  /** Stop server. */
  @AfterClass
  public static void stopServer() {
    stopStaticServer();
  }

  @Test
  public void compresses() {
    expectFzip = true;
    final SimpleRequest request = SimpleRequest.newBuilder()
        .setResponseSize(314159)
        .setResponseCompression(CompressionType.GZIP)
        .setResponseType(PayloadType.COMPRESSABLE)
        .setPayload(Payload.newBuilder()
            .setBody(ByteString.copyFrom(new byte[271828])))
        .build();
    final SimpleResponse goldenResponse = SimpleResponse.newBuilder()
        .setPayload(Payload.newBuilder()
            .setType(PayloadType.COMPRESSABLE)
            .setBody(ByteString.copyFrom(new byte[314159])))
        .build();


    assertEquals(goldenResponse, blockingStub.unaryCall(request));
    // Assert that compression took place
    assertTrue(FZIPPER.anyRead);
    assertTrue(FZIPPER.anyWritten);
  }

  @Override
  protected ManagedChannel createChannel() {
    NettyChannelBuilder builder = NettyChannelBuilder.forAddress("localhost", getPort())
        .maxInboundMessageSize(AbstractInteropTest.MAX_MESSAGE_SIZE)
        .decompressorRegistry(decompressors)
        .compressorRegistry(compressors)
        .intercept(new ClientInterceptor() {
          @Override
          public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
              MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
            final ClientCall<ReqT, RespT> call = next.newCall(method, callOptions);
            return new ForwardingClientCall<ReqT, RespT>() {

              @Override
              protected ClientCall<ReqT, RespT> delegate() {
                return call;
              }

              @Override
              public void start(
                  final ClientCall.Listener<RespT> responseListener, Metadata headers) {
                ClientCall.Listener<RespT> listener = new ForwardingClientCallListener<RespT>() {

                  @Override
                  protected io.grpc.ClientCall.Listener<RespT> delegate() {
                    return responseListener;
                  }

                  @Override
                  public void onHeaders(Metadata headers) {
                    super.onHeaders(headers);
                    if (expectFzip) {
                      String encoding = headers.get(GrpcUtil.MESSAGE_ENCODING_KEY);
                      assertEquals(encoding, FZIPPER.getMessageEncoding());
                    }
                  }
                };
                super.start(listener, headers);
                setMessageCompression(true);
              }
            };
          }
        })
        .usePlaintext(true);
    io.grpc.internal.TestingAccessor.setStatsContextFactory(builder, getClientStatsFactory());
    return builder.build();
  }

  /**
   * Fzip is a custom compressor.
   */
  static class Fzip implements Codec {
    volatile boolean anyRead;
    volatile boolean anyWritten;
    volatile Codec delegate;

    private final String actualName;

    public Fzip(String actualName, Codec delegate) {
      this.actualName = actualName;
      this.delegate = delegate;
    }

    @Override
    public String getMessageEncoding() {
      return actualName;
    }

    @Override
    public OutputStream compress(OutputStream os) throws IOException {
      return new FilterOutputStream(delegate.compress(os)) {
        @Override
        public void write(int b) throws IOException {
          super.write(b);
          anyWritten = true;
        }
      };
    }

    @Override
    public InputStream decompress(InputStream is) throws IOException {
      return new FilterInputStream(delegate.decompress(is)) {
        @Override
        public int read() throws IOException {
          int val = super.read();
          anyRead = true;
          return val;
        }

        @Override
        public int read(byte[] b, int off, int len) throws IOException {
          int total = super.read(b, off, len);
          anyRead = true;
          return total;
        }
      };
    }
  }
}

