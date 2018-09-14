/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc.services;

import static com.google.common.base.Charsets.UTF_8;
import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import com.google.common.io.ByteStreams;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ForwardingClientCall.SimpleForwardingClientCall;
import io.grpc.ForwardingClientCallListener.SimpleForwardingClientCallListener;
import io.grpc.ForwardingServerCall.SimpleForwardingServerCall;
import io.grpc.ForwardingServerCallListener.SimpleForwardingServerCallListener;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCall;
import io.grpc.ServerCall.Listener;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerMethodDefinition;
import io.grpc.internal.IoUtils;
import io.grpc.internal.NoopClientCall;
import io.grpc.internal.NoopServerCall;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link BinaryLogProvider}. */
@RunWith(JUnit4.class)
public class BinaryLogProviderTest {
  private final InvocationCountMarshaller<String> reqMarshaller =
      new InvocationCountMarshaller<String>() {
        @Override
        Marshaller<String> delegate() {
          return StringMarshaller.INSTANCE;
        }
      };
  private final InvocationCountMarshaller<Integer> respMarshaller =
      new InvocationCountMarshaller<Integer>() {
        @Override
        Marshaller<Integer> delegate() {
          return IntegerMarshaller.INSTANCE;
        }
      };
  private final MethodDescriptor<String, Integer> method =
      MethodDescriptor
          .newBuilder(reqMarshaller, respMarshaller)
          .setFullMethodName("myservice/mymethod")
          .setType(MethodType.UNARY)
          .setSchemaDescriptor(new Object())
          .setIdempotent(true)
          .setSafe(true)
          .setSampledToLocalTracing(true)
          .build();
  private final List<byte[]> binlogReq = new ArrayList<byte[]>();
  private final List<byte[]> binlogResp = new ArrayList<byte[]>();
  private final BinaryLogProvider binlogProvider = new BinaryLogProvider() {
    @Override
    public ServerInterceptor getServerInterceptor(String fullMethodName) {
      return new TestBinaryLogServerInterceptor();
    }

    @Override
    public ClientInterceptor getClientInterceptor(
        String fullMethodName, CallOptions callOptions) {
      return new TestBinaryLogClientInterceptor();
    }
  };

  @Test
  public void wrapChannel_methodDescriptor() throws Exception {
    final AtomicReference<MethodDescriptor<?, ?>> methodRef =
        new AtomicReference<MethodDescriptor<?, ?>>();
    Channel channel = new Channel() {
      @Override
      public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
          MethodDescriptor<RequestT, ResponseT> method, CallOptions callOptions) {
        methodRef.set(method);
        return new NoopClientCall<RequestT, ResponseT>();
      }

      @Override
      public String authority() {
        throw new UnsupportedOperationException();
      }
    };
    Channel wChannel = binlogProvider.wrapChannel(channel);
    ClientCall<String, Integer> unusedClientCall = wChannel.newCall(method, CallOptions.DEFAULT);
    validateWrappedMethod(methodRef.get());
  }

  @Test
  public void wrapChannel_handler() throws Exception {
    final List<byte[]> serializedReq = new ArrayList<byte[]>();
    final AtomicReference<ClientCall.Listener<?>> listener =
        new AtomicReference<ClientCall.Listener<?>>();
    Channel channel = new Channel() {
      @Override
      public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
          MethodDescriptor<RequestT, ResponseT> methodDescriptor, CallOptions callOptions) {
        return new NoopClientCall<RequestT, ResponseT>() {
          @Override
          public void start(Listener<ResponseT> responseListener, Metadata headers) {
            listener.set(responseListener);
          }

          @Override
          public void sendMessage(RequestT message) {
            serializedReq.add((byte[]) message);
          }
        };
      }

      @Override
      public String authority() {
        throw new UnsupportedOperationException();
      }
    };
    Channel wChannel = binlogProvider.wrapChannel(channel);
    ClientCall<String, Integer> clientCall = wChannel.newCall(method, CallOptions.DEFAULT);
    final List<Integer> observedResponse = new ArrayList<>();
    clientCall.start(
        new NoopClientCall.NoopClientCallListener<Integer>() {
          @Override
          public void onMessage(Integer message) {
            observedResponse.add(message);
          }
        },
        new Metadata());

    String expectedRequest = "hello world";
    assertThat(binlogReq).isEmpty();
    assertThat(serializedReq).isEmpty();
    assertEquals(0, reqMarshaller.streamInvocations);
    clientCall.sendMessage(expectedRequest);
    // it is unacceptably expensive for the binlog to double parse every logged message
    assertEquals(1, reqMarshaller.streamInvocations);
    assertEquals(0, reqMarshaller.parseInvocations);
    assertThat(binlogReq).hasSize(1);
    assertThat(serializedReq).hasSize(1);
    assertEquals(
        expectedRequest,
        StringMarshaller.INSTANCE.parse(new ByteArrayInputStream(binlogReq.get(0))));
    assertEquals(
        expectedRequest,
        StringMarshaller.INSTANCE.parse(new ByteArrayInputStream(serializedReq.get(0))));

    int expectedResponse = 12345;
    assertThat(binlogResp).isEmpty();
    assertThat(observedResponse).isEmpty();
    assertEquals(0, respMarshaller.parseInvocations);
    onClientMessageHelper(listener.get(), IntegerMarshaller.INSTANCE.stream(expectedResponse));
    // it is unacceptably expensive for the binlog to double parse every logged message
    assertEquals(1, respMarshaller.parseInvocations);
    assertEquals(0, respMarshaller.streamInvocations);
    assertThat(binlogResp).hasSize(1);
    assertThat(observedResponse).hasSize(1);
    assertEquals(
        expectedResponse,
        (int) IntegerMarshaller.INSTANCE.parse(new ByteArrayInputStream(binlogResp.get(0))));
    assertEquals(expectedResponse, (int) observedResponse.get(0));
  }

  @SuppressWarnings({"rawtypes", "unchecked"})
  private static void onClientMessageHelper(ClientCall.Listener listener, Object request) {
    listener.onMessage(request);
  }

  private void validateWrappedMethod(MethodDescriptor<?, ?> wMethod) {
    assertSame(BinaryLogProvider.BYTEARRAY_MARSHALLER, wMethod.getRequestMarshaller());
    assertSame(BinaryLogProvider.BYTEARRAY_MARSHALLER, wMethod.getResponseMarshaller());
    assertEquals(method.getType(), wMethod.getType());
    assertEquals(method.getFullMethodName(), wMethod.getFullMethodName());
    assertEquals(method.getSchemaDescriptor(), wMethod.getSchemaDescriptor());
    assertEquals(method.isIdempotent(), wMethod.isIdempotent());
    assertEquals(method.isSafe(), wMethod.isSafe());
    assertEquals(method.isSampledToLocalTracing(), wMethod.isSampledToLocalTracing());
  }

  @Test
  public void wrapMethodDefinition_methodDescriptor() throws Exception {
    ServerMethodDefinition<String, Integer> methodDef =
        ServerMethodDefinition.create(
            method,
            new ServerCallHandler<String, Integer>() {
              @Override
              public Listener<String> startCall(
                  ServerCall<String, Integer> call, Metadata headers) {
                throw new UnsupportedOperationException();
              }
            });
    ServerMethodDefinition<?, ?> wMethodDef = binlogProvider.wrapMethodDefinition(methodDef);
    validateWrappedMethod(wMethodDef.getMethodDescriptor());
  }

  @Test
  public void wrapMethodDefinition_handler() throws Exception {
    // The request as seen by the user supplied server code
    final List<String> observedRequest = new ArrayList<>();
    final AtomicReference<ServerCall<String, Integer>> serverCall =
        new AtomicReference<ServerCall<String, Integer>>();
    ServerMethodDefinition<String, Integer> methodDef =
        ServerMethodDefinition.create(
            method,
            new ServerCallHandler<String, Integer>() {
              @Override
              public ServerCall.Listener<String> startCall(
                  ServerCall<String, Integer> call, Metadata headers) {
                serverCall.set(call);
                return new ServerCall.Listener<String>() {
                  @Override
                  public void onMessage(String message) {
                    observedRequest.add(message);
                  }
                };
              }
            });
    ServerMethodDefinition<?, ?> wDef = binlogProvider.wrapMethodDefinition(methodDef);
    List<Object> serializedResp = new ArrayList<>();
    ServerCall.Listener<?> wListener = startServerCallHelper(wDef, serializedResp);

    String expectedRequest = "hello world";
    assertThat(binlogReq).isEmpty();
    assertThat(observedRequest).isEmpty();
    assertEquals(0, reqMarshaller.parseInvocations);
    onServerMessageHelper(wListener, StringMarshaller.INSTANCE.stream(expectedRequest));
    // it is unacceptably expensive for the binlog to double parse every logged message
    assertEquals(1, reqMarshaller.parseInvocations);
    assertEquals(0, reqMarshaller.streamInvocations);
    assertThat(binlogReq).hasSize(1);
    assertThat(observedRequest).hasSize(1);
    assertEquals(
        expectedRequest,
        StringMarshaller.INSTANCE.parse(new ByteArrayInputStream(binlogReq.get(0))));
    assertEquals(expectedRequest, observedRequest.get(0));

    int expectedResponse = 12345;
    assertThat(binlogResp).isEmpty();
    assertThat(serializedResp).isEmpty();
    assertEquals(0, respMarshaller.streamInvocations);
    serverCall.get().sendMessage(expectedResponse);
    // it is unacceptably expensive for the binlog to double parse every logged message
    assertEquals(0, respMarshaller.parseInvocations);
    assertEquals(1, respMarshaller.streamInvocations);
    assertThat(binlogResp).hasSize(1);
    assertThat(serializedResp).hasSize(1);
    assertEquals(
        expectedResponse,
        (int) IntegerMarshaller.INSTANCE.parse(new ByteArrayInputStream(binlogResp.get(0))));
    assertEquals(expectedResponse,
        (int) method.parseResponse(new ByteArrayInputStream((byte[]) serializedResp.get(0))));
  }

  @SuppressWarnings({"rawtypes", "unchecked"})
  private static void onServerMessageHelper(ServerCall.Listener listener, Object request) {
    listener.onMessage(request);
  }

  private static <ReqT, RespT> ServerCall.Listener<ReqT> startServerCallHelper(
      final ServerMethodDefinition<ReqT, RespT> methodDef,
      final List<Object> serializedResp) {
    ServerCall<ReqT, RespT> serverCall = new NoopServerCall<ReqT, RespT>() {
      @Override
      public void sendMessage(RespT message) {
        serializedResp.add(message);
      }

      @Override
      public MethodDescriptor<ReqT, RespT> getMethodDescriptor() {
        return methodDef.getMethodDescriptor();
      }
    };
    return methodDef.getServerCallHandler().startCall(serverCall, new Metadata());
  }

  private final class TestBinaryLogClientInterceptor implements ClientInterceptor {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        final MethodDescriptor<ReqT, RespT> method,
        CallOptions callOptions,
        Channel next) {
      assertSame(BinaryLogProvider.BYTEARRAY_MARSHALLER, method.getRequestMarshaller());
      assertSame(BinaryLogProvider.BYTEARRAY_MARSHALLER, method.getResponseMarshaller());
      return new SimpleForwardingClientCall<ReqT, RespT>(next.newCall(method, callOptions)) {
        @Override
        public void start(Listener<RespT> responseListener, Metadata headers) {
          super.start(
              new SimpleForwardingClientCallListener<RespT>(responseListener) {
                @Override
                public void onMessage(RespT message) {
                  assertTrue(message instanceof InputStream);
                  try {
                    byte[] bytes = IoUtils.toByteArray((InputStream) message);
                    binlogResp.add(bytes);
                    ByteArrayInputStream input = new ByteArrayInputStream(bytes);
                    RespT dup = method.parseResponse(input);
                    super.onMessage(dup);
                  } catch (IOException e) {
                    throw new RuntimeException(e);
                  }
                }
              },
              headers);
        }

        @Override
        public void sendMessage(ReqT message) {
            byte[] bytes = (byte[]) message;
            binlogReq.add(bytes);
            ByteArrayInputStream input = new ByteArrayInputStream(bytes);
            ReqT dup = method.parseRequest(input);
            super.sendMessage(dup);
        }
      };
    }
  }

  private final class TestBinaryLogServerInterceptor implements ServerInterceptor {
    @Override
    public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
        final ServerCall<ReqT, RespT> call,
        Metadata headers,
        ServerCallHandler<ReqT, RespT> next) {
      assertSame(
          BinaryLogProvider.BYTEARRAY_MARSHALLER,
          call.getMethodDescriptor().getRequestMarshaller());
      assertSame(
          BinaryLogProvider.BYTEARRAY_MARSHALLER,
          call.getMethodDescriptor().getResponseMarshaller());
      ServerCall<ReqT, RespT> wCall = new SimpleForwardingServerCall<ReqT, RespT>(call) {
        @Override
        public void sendMessage(RespT message) {
            byte[] bytes = (byte[]) message;
            binlogResp.add(bytes);
            ByteArrayInputStream input = new ByteArrayInputStream(bytes);
            RespT dup = call.getMethodDescriptor().parseResponse(input);
            super.sendMessage(dup);
        }
      };
      final ServerCall.Listener<ReqT> oListener = next.startCall(wCall, headers);
      return new SimpleForwardingServerCallListener<ReqT>(oListener) {
        @Override
        public void onMessage(ReqT message) {
          assertTrue(message instanceof InputStream);
          try {
            byte[] bytes = IoUtils.toByteArray((InputStream) message);
            binlogReq.add(bytes);
            ByteArrayInputStream input = new ByteArrayInputStream(bytes);
            ReqT dup = call.getMethodDescriptor().parseRequest(input);
            super.onMessage(dup);
          } catch (IOException e) {
            throw new RuntimeException(e);
          }
        }
      };
    }
  }

  private abstract static class InvocationCountMarshaller<T>
      implements MethodDescriptor.Marshaller<T> {
    private int streamInvocations = 0;
    private int parseInvocations = 0;

    abstract MethodDescriptor.Marshaller<T> delegate();

    @Override
    public InputStream stream(T value) {
      streamInvocations++;
      return delegate().stream(value);
    }

    @Override
    public T parse(InputStream stream) {
      parseInvocations++;
      return delegate().parse(stream);
    }
  }


  private static class StringMarshaller implements MethodDescriptor.Marshaller<String> {
    public static final StringMarshaller INSTANCE = new StringMarshaller();

    @Override
    public InputStream stream(String value) {
      return new ByteArrayInputStream(value.getBytes(UTF_8));
    }

    @Override
    public String parse(InputStream stream) {
      try {
        return new String(ByteStreams.toByteArray(stream), UTF_8);
      } catch (IOException ex) {
        throw new RuntimeException(ex);
      }
    }
  }

  private static class IntegerMarshaller implements MethodDescriptor.Marshaller<Integer> {
    public static final IntegerMarshaller INSTANCE = new IntegerMarshaller();

    @Override
    public InputStream stream(Integer value) {
      return StringMarshaller.INSTANCE.stream(value.toString());
    }

    @Override
    public Integer parse(InputStream stream) {
      return Integer.valueOf(StringMarshaller.INSTANCE.parse(stream));
    }
  }
}
