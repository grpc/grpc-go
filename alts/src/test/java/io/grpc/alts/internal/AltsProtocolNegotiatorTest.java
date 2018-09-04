/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts.internal;

import static com.google.common.truth.Truth.assertThat;
import static java.nio.charset.StandardCharsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

import io.grpc.Attributes;
import io.grpc.CallCredentials;
import io.grpc.Grpc;
import io.grpc.InternalChannelz;
import io.grpc.SecurityLevel;
import io.grpc.alts.internal.Handshaker.HandshakerResult;
import io.grpc.alts.internal.TsiFrameProtector.Consumer;
import io.grpc.alts.internal.TsiPeer.Property;
import io.grpc.netty.GrpcHttp2ConnectionHandler;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.buffer.CompositeByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.channel.ChannelDuplexHandler;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.channel.embedded.EmbeddedChannel;
import io.netty.handler.codec.http2.DefaultHttp2Connection;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionDecoder;
import io.netty.handler.codec.http2.DefaultHttp2ConnectionEncoder;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionDecoder;
import io.netty.handler.codec.http2.Http2ConnectionEncoder;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Settings;
import io.netty.util.ReferenceCountUtil;
import io.netty.util.ReferenceCounted;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.Future;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Tests for {@link AltsProtocolNegotiator}. */
@RunWith(JUnit4.class)
public class AltsProtocolNegotiatorTest {
  private final CapturingGrpcHttp2ConnectionHandler grpcHandler = capturingGrpcHandler();

  private final List<ReferenceCounted> references = new ArrayList<>();
  private final LinkedBlockingQueue<InterceptingProtector> protectors = new LinkedBlockingQueue<>();

  private EmbeddedChannel channel;
  private Throwable caughtException;

  private volatile TsiHandshakeHandler.TsiHandshakeCompletionEvent tsiEvent;
  private ChannelHandler handler;

  private TsiPeer mockedTsiPeer = new TsiPeer(Collections.<Property<?>>emptyList());
  private AltsAuthContext mockedAltsContext =
      new AltsAuthContext(
          HandshakerResult.newBuilder()
              .setPeerRpcVersions(RpcProtocolVersionsUtil.getRpcProtocolVersions())
              .build());
  private final TsiHandshaker mockHandshaker =
      new DelegatingTsiHandshaker(FakeTsiHandshaker.newFakeHandshakerServer()) {
        @Override
        public TsiPeer extractPeer() throws GeneralSecurityException {
          return mockedTsiPeer;
        }

        @Override
        public Object extractPeerObject() throws GeneralSecurityException {
          return mockedAltsContext;
        }
      };
  private final NettyTsiHandshaker serverHandshaker = new NettyTsiHandshaker(mockHandshaker);

  @Before
  public void setup() throws Exception {
    ChannelHandler userEventHandler =
        new ChannelDuplexHandler() {
          @Override
          public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
            if (evt instanceof TsiHandshakeHandler.TsiHandshakeCompletionEvent) {
              tsiEvent = (TsiHandshakeHandler.TsiHandshakeCompletionEvent) evt;
            } else {
              super.userEventTriggered(ctx, evt);
            }
          }
        };

    ChannelHandler uncaughtExceptionHandler =
        new ChannelDuplexHandler() {
          @Override
          public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
            caughtException = cause;
            super.exceptionCaught(ctx, cause);
          }
        };

    TsiHandshakerFactory handshakerFactory =
        new DelegatingTsiHandshakerFactory(FakeTsiHandshaker.clientHandshakerFactory()) {
          @Override
          public TsiHandshaker newHandshaker() {
            return new DelegatingTsiHandshaker(super.newHandshaker()) {
              @Override
              public TsiPeer extractPeer() throws GeneralSecurityException {
                return mockedTsiPeer;
              }

              @Override
              public Object extractPeerObject() throws GeneralSecurityException {
                return mockedAltsContext;
              }
            };
          }
        };
    handler = AltsProtocolNegotiator.create(handshakerFactory).newHandler(grpcHandler);
    channel = new EmbeddedChannel(uncaughtExceptionHandler, handler, userEventHandler);
  }

  @After
  public void teardown() throws Exception {
    if (channel != null) {
      @SuppressWarnings("unused") // go/futurereturn-lsc
      Future<?> possiblyIgnoredError = channel.close();
    }

    for (ReferenceCounted reference : references) {
      ReferenceCountUtil.safeRelease(reference);
    }
  }

  @Test
  public void handshakeShouldBeSuccessful() throws Exception {
    doHandshake();
  }

  @Test
  @SuppressWarnings("unchecked") // List cast
  public void protectShouldRoundtrip() throws Exception {
    // Write the message 1 character at a time. The message should be buffered
    // and not interfere with the handshake.
    final AtomicInteger writeCount = new AtomicInteger();
    String message = "hello";
    for (int ix = 0; ix < message.length(); ++ix) {
      ByteBuf in = Unpooled.copiedBuffer(message, ix, 1, UTF_8);
      @SuppressWarnings("unused") // go/futurereturn-lsc
      Future<?> possiblyIgnoredError =
          channel
              .write(in)
              .addListener(
                  new ChannelFutureListener() {
                    @Override
                    public void operationComplete(ChannelFuture future) throws Exception {
                      if (future.isSuccess()) {
                        writeCount.incrementAndGet();
                      }
                    }
                  });
    }
    channel.flush();

    // Now do the handshake. The buffered message will automatically be protected
    // and sent.
    doHandshake();

    // Capture the protected data written to the wire.
    assertEquals(1, channel.outboundMessages().size());
    ByteBuf protectedData = channel.<ByteBuf>readOutbound();
    assertEquals(message.length(), writeCount.get());

    // Read the protected message at the server and verify it matches the original message.
    TsiFrameProtector serverProtector = serverHandshaker.createFrameProtector(channel.alloc());
    List<ByteBuf> unprotected = new ArrayList<>();
    serverProtector.unprotect(protectedData, (List<Object>) (List<?>) unprotected, channel.alloc());
    // We try our best to remove the HTTP2 handler as soon as possible, but just by constructing it
    // a settings frame is written (and an HTTP2 preface).  This is hard coded into Netty, so we
    // have to remove it here.  See {@code Http2ConnectionHandler.PrefaceDecode.sendPreface}.
    int settingsFrameLength = 9;

    CompositeByteBuf unprotectedAll =
        new CompositeByteBuf(channel.alloc(), false, unprotected.size() + 1, unprotected);
    ByteBuf unprotectedData = unprotectedAll.slice(settingsFrameLength, message.length());
    assertEquals(message, unprotectedData.toString(UTF_8));

    // Protect the same message at the server.
    final AtomicReference<ByteBuf> newlyProtectedData = new AtomicReference<>();
    serverProtector.protectFlush(
        Collections.singletonList(unprotectedData),
        new Consumer<ByteBuf>() {
          @Override
          public void accept(ByteBuf buf) {
            newlyProtectedData.set(buf);
          }
        },
        channel.alloc());

    // Read the protected message at the client and verify that it matches the original message.
    channel.writeInbound(newlyProtectedData.get());
    assertEquals(1, channel.inboundMessages().size());
    assertEquals(message, channel.<ByteBuf>readInbound().toString(UTF_8));
  }

  @Test
  public void unprotectLargeIncomingFrame() throws Exception {

    // We use a server frameprotector with twice the standard frame size.
    int serverFrameSize = 4096 * 2;
    // This should fit into one frame.
    byte[] unprotectedBytes = new byte[serverFrameSize - 500];
    Arrays.fill(unprotectedBytes, (byte) 7);
    ByteBuf unprotectedData = Unpooled.wrappedBuffer(unprotectedBytes);
    unprotectedData.writerIndex(unprotectedBytes.length);

    // Perform handshake.
    doHandshake();

    // Protect the message on the server.
    TsiFrameProtector serverProtector =
        serverHandshaker.createFrameProtector(serverFrameSize, channel.alloc());
    serverProtector.protectFlush(
        Collections.singletonList(unprotectedData),
        new Consumer<ByteBuf>() {
          @Override
          public void accept(ByteBuf buf) {
            channel.writeInbound(buf);
          }
        },
        channel.alloc());
    channel.flushInbound();

    // Read the protected message at the client and verify that it matches the original message.
    assertEquals(1, channel.inboundMessages().size());

    ByteBuf receivedData1 = channel.<ByteBuf>readInbound();
    int receivedLen1 = receivedData1.readableBytes();
    byte[] receivedBytes = new byte[receivedLen1];
    receivedData1.readBytes(receivedBytes, 0, receivedLen1);

    assertThat(unprotectedBytes.length).isEqualTo(receivedBytes.length);
    assertThat(unprotectedBytes).isEqualTo(receivedBytes);
  }

  @Test
  public void flushShouldFailAllPromises() throws Exception {
    doHandshake();

    channel
        .pipeline()
        .addFirst(
            new ChannelDuplexHandler() {
              @Override
              public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise)
                  throws Exception {
                throw new Exception("Fake exception");
              }
            });

    // Write the message 1 character at a time.
    String message = "hello";
    final AtomicInteger failures = new AtomicInteger();
    for (int ix = 0; ix < message.length(); ++ix) {
      ByteBuf in = Unpooled.copiedBuffer(message, ix, 1, UTF_8);
      @SuppressWarnings("unused") // go/futurereturn-lsc
      Future<?> possiblyIgnoredError =
          channel
              .write(in)
              .addListener(
                  new ChannelFutureListener() {
                    @Override
                    public void operationComplete(ChannelFuture future) throws Exception {
                      if (!future.isSuccess()) {
                        failures.incrementAndGet();
                      }
                    }
                  });
    }
    channel.flush();

    // Verify that the promises fail.
    assertEquals(message.length(), failures.get());
  }

  @Test
  public void doNotFlushEmptyBuffer() throws Exception {
    doHandshake();
    assertEquals(1, protectors.size());
    InterceptingProtector protector = protectors.poll();

    String message = "hello";
    ByteBuf in = Unpooled.copiedBuffer(message, UTF_8);

    assertEquals(0, protector.flushes.get());
    Future<?> done = channel.write(in);
    channel.flush();
    done.get(5, TimeUnit.SECONDS);
    assertEquals(1, protector.flushes.get());

    done = channel.write(Unpooled.EMPTY_BUFFER);
    channel.flush();
    done.get(5, TimeUnit.SECONDS);
    assertEquals(1, protector.flushes.get());
  }

  @Test
  public void peerPropagated() throws Exception {
    doHandshake();

    assertThat(grpcHandler.attrs.get(AltsProtocolNegotiator.getTsiPeerAttributeKey()))
        .isEqualTo(mockedTsiPeer);
    assertThat(grpcHandler.attrs.get(AltsProtocolNegotiator.getAltsAuthContextAttributeKey()))
        .isEqualTo(mockedAltsContext);
    assertThat(grpcHandler.attrs.get(Grpc.TRANSPORT_ATTR_REMOTE_ADDR).toString())
        .isEqualTo("embedded");
    assertThat(grpcHandler.attrs.get(CallCredentials.ATTR_SECURITY_LEVEL))
        .isEqualTo(SecurityLevel.PRIVACY_AND_INTEGRITY);
  }

  private void doHandshake() throws Exception {
    // Capture the client frame and add to the server.
    assertEquals(1, channel.outboundMessages().size());
    ByteBuf clientFrame = channel.<ByteBuf>readOutbound();
    assertTrue(serverHandshaker.processBytesFromPeer(clientFrame));

    // Get the server response handshake frames.
    ByteBuf serverFrame = channel.alloc().buffer();
    serverHandshaker.getBytesToSendToPeer(serverFrame);
    channel.writeInbound(serverFrame);

    // Capture the next client frame and add to the server.
    assertEquals(1, channel.outboundMessages().size());
    clientFrame = channel.<ByteBuf>readOutbound();
    assertTrue(serverHandshaker.processBytesFromPeer(clientFrame));

    // Get the server response handshake frames.
    serverFrame = channel.alloc().buffer();
    serverHandshaker.getBytesToSendToPeer(serverFrame);
    channel.writeInbound(serverFrame);

    // Ensure that both sides have confirmed that the handshake has completed.
    assertFalse(serverHandshaker.isInProgress());

    if (caughtException != null) {
      throw new RuntimeException(caughtException);
    }
    assertNotNull(tsiEvent);
  }

  private CapturingGrpcHttp2ConnectionHandler capturingGrpcHandler() {
    // Netty Boilerplate.  We don't really need any of this, but there is a tight coupling
    // between a Http2ConnectionHandler and its dependencies.
    Http2Connection connection = new DefaultHttp2Connection(true);
    Http2FrameWriter frameWriter = new DefaultHttp2FrameWriter();
    Http2FrameReader frameReader = new DefaultHttp2FrameReader(false);
    DefaultHttp2ConnectionEncoder encoder =
        new DefaultHttp2ConnectionEncoder(connection, frameWriter);
    DefaultHttp2ConnectionDecoder decoder =
        new DefaultHttp2ConnectionDecoder(connection, encoder, frameReader);

    return new CapturingGrpcHttp2ConnectionHandler(decoder, encoder, new Http2Settings());
  }

  private final class CapturingGrpcHttp2ConnectionHandler extends GrpcHttp2ConnectionHandler {
    private Attributes attrs;

    private CapturingGrpcHttp2ConnectionHandler(
        Http2ConnectionDecoder decoder,
        Http2ConnectionEncoder encoder,
        Http2Settings initialSettings) {
      super(null, decoder, encoder, initialSettings);
    }

    @Override
    public void handleProtocolNegotiationCompleted(
        Attributes attrs, InternalChannelz.Security securityInfo) {
      // If we are added to the pipeline, we need to remove ourselves.  The HTTP2 handler
      channel.pipeline().remove(this);
      this.attrs = attrs;
    }
  }

  private static class DelegatingTsiHandshakerFactory implements TsiHandshakerFactory {

    private TsiHandshakerFactory delegate;

    DelegatingTsiHandshakerFactory(TsiHandshakerFactory delegate) {
      this.delegate = delegate;
    }

    @Override
    public TsiHandshaker newHandshaker() {
      return delegate.newHandshaker();
    }
  }

  private class DelegatingTsiHandshaker implements TsiHandshaker {

    private final TsiHandshaker delegate;

    DelegatingTsiHandshaker(TsiHandshaker delegate) {
      this.delegate = delegate;
    }

    @Override
    public void getBytesToSendToPeer(ByteBuffer bytes) throws GeneralSecurityException {
      delegate.getBytesToSendToPeer(bytes);
    }

    @Override
    public boolean processBytesFromPeer(ByteBuffer bytes) throws GeneralSecurityException {
      return delegate.processBytesFromPeer(bytes);
    }

    @Override
    public boolean isInProgress() {
      return delegate.isInProgress();
    }

    @Override
    public TsiPeer extractPeer() throws GeneralSecurityException {
      return delegate.extractPeer();
    }

    @Override
    public Object extractPeerObject() throws GeneralSecurityException {
      return delegate.extractPeerObject();
    }

    @Override
    public TsiFrameProtector createFrameProtector(ByteBufAllocator alloc) {
      InterceptingProtector protector =
          new InterceptingProtector(delegate.createFrameProtector(alloc));
      protectors.add(protector);
      return protector;
    }

    @Override
    public TsiFrameProtector createFrameProtector(int maxFrameSize, ByteBufAllocator alloc) {
      InterceptingProtector protector =
          new InterceptingProtector(delegate.createFrameProtector(maxFrameSize, alloc));
      protectors.add(protector);
      return protector;
    }
  }

  private static class InterceptingProtector implements TsiFrameProtector {
    private final TsiFrameProtector delegate;
    final AtomicInteger flushes = new AtomicInteger();

    InterceptingProtector(TsiFrameProtector delegate) {
      this.delegate = delegate;
    }

    @Override
    public void protectFlush(
        List<ByteBuf> unprotectedBufs, Consumer<ByteBuf> ctxWrite, ByteBufAllocator alloc)
        throws GeneralSecurityException {
      flushes.incrementAndGet();
      delegate.protectFlush(unprotectedBufs, ctxWrite, alloc);
    }

    @Override
    public void unprotect(ByteBuf in, List<Object> out, ByteBufAllocator alloc)
        throws GeneralSecurityException {
      delegate.unprotect(in, out, alloc);
    }

    @Override
    public void destroy() {
      delegate.destroy();
    }
  }
}
