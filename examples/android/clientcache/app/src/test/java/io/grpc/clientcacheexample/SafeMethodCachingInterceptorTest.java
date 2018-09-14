package io.grpc.clientcacheexample;

import com.google.common.truth.Truth;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.fail;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientInterceptors;
import io.grpc.ForwardingServerCall;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerInterceptors;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.examples.helloworld.AnotherGreeterGrpc;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.stub.ClientCalls;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.GrpcServerRule;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;

public class SafeMethodCachingInterceptorTest {
  private static final Metadata.Key<String> CACHE_CONTROL_METADATA_KEY =
      Metadata.Key.of("cache-control", Metadata.ASCII_STRING_MARSHALLER);

  @Rule public final GrpcServerRule grpcServerRule = new GrpcServerRule().directExecutor();

  private final GreeterGrpc.GreeterImplBase greeterServiceImpl =
      new GreeterGrpc.GreeterImplBase() {
        private int count = 1;

        @Override
        public void sayHello(HelloRequest req, StreamObserver<HelloReply> responseObserver) {
          HelloReply reply =
              HelloReply.newBuilder().setMessage("Hello " + req.getName() + " " + count++).build();
          responseObserver.onNext(reply);
          responseObserver.onCompleted();
        }

        @Override
        public void sayAnotherHello(HelloRequest req, StreamObserver<HelloReply> responseObserver) {
          HelloReply reply =
              HelloReply.newBuilder()
                  .setMessage("Hello again " + req.getName() + " " + count++)
                  .build();
          responseObserver.onNext(reply);
          responseObserver.onCompleted();
        }
      };

  private final AnotherGreeterGrpc.AnotherGreeterImplBase anotherGreeterServiceImpl =
      new AnotherGreeterGrpc.AnotherGreeterImplBase() {
        private int count = 1;

        @Override
        public void sayHello(HelloRequest req, StreamObserver<HelloReply> responseObserver) {
          HelloReply reply =
              HelloReply.newBuilder().setMessage("Hey " + req.getName() + " " + count++).build();
          responseObserver.onNext(reply);
          responseObserver.onCompleted();
        }
      };

  private final List<String> cacheControlDirectives = new ArrayList<>();
  private ServerInterceptor injectCacheControlInterceptor =
      new ServerInterceptor() {
        @Override
        public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
            ServerCall<ReqT, RespT> call,
            final Metadata requestHeaders,
            ServerCallHandler<ReqT, RespT> next) {
          return next.startCall(
              new ForwardingServerCall.SimpleForwardingServerCall<ReqT, RespT>(call) {
                @Override
                public void sendHeaders(Metadata headers) {
                  for (String cacheControlDirective : cacheControlDirectives) {
                    headers.put(CACHE_CONTROL_METADATA_KEY, cacheControlDirective);
                  }
                  super.sendHeaders(headers);
                }
              },
              requestHeaders);
        }
      };

  private final HelloRequest message = HelloRequest.newBuilder().setName("Test Name").build();
  private final MethodDescriptor<HelloRequest, HelloReply> safeGreeterSayHelloMethod =
      GreeterGrpc.getSayHelloMethod().toBuilder().setSafe(true).build();
  private final TestCache cache = new TestCache();

  private ManagedChannel baseChannel;
  private Channel channelToUse;

  @Before
  public void setUp() throws Exception {
    grpcServerRule
        .getServiceRegistry()
        .addService(
            ServerInterceptors.intercept(greeterServiceImpl, injectCacheControlInterceptor));
    grpcServerRule.getServiceRegistry().addService(anotherGreeterServiceImpl);
    baseChannel = grpcServerRule.getChannel();

    SafeMethodCachingInterceptor interceptor =
        SafeMethodCachingInterceptor.newSafeMethodCachingInterceptor(cache);

    channelToUse = ClientInterceptors.intercept(baseChannel, interceptor);
  }

  @After
  public void tearDown() {
    baseChannel.shutdown();
  }

  @Test
  public void safeCallsAreCached() {
    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertSame(reply1, reply2);
  }

  @Test
  public void safeCallsAreCachedWithCopiedMethodDescriptor() {
    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse,
            safeGreeterSayHelloMethod.toBuilder().build(),
            CallOptions.DEFAULT,
            message);

    assertSame(reply1, reply2);
  }

  @Test
  public void requestWithNoCacheOptionSkipsCache() {
    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse,
            safeGreeterSayHelloMethod,
            CallOptions.DEFAULT.withOption(SafeMethodCachingInterceptor.NO_CACHE_CALL_OPTION, true),
            message);
    HelloReply reply3 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
    assertSame(reply1, reply3);
  }

  @Test
  public void requestWithOnlyIfCachedOption_unavailableIfNotInCache() {
    try {
      ClientCalls.blockingUnaryCall(
          channelToUse,
          safeGreeterSayHelloMethod,
          CallOptions.DEFAULT.withOption(
              SafeMethodCachingInterceptor.ONLY_IF_CACHED_CALL_OPTION, true),
          message);
      fail("Expected call to fail");
    } catch (StatusRuntimeException sre) {
      assertEquals(Status.UNAVAILABLE.getCode(), sre.getStatus().getCode());
      assertEquals(
          "Unsatisfiable Request (only-if-cached set, but value not in cache)",
          sre.getStatus().getDescription());
    }
  }

  @Test
  public void requestWithOnlyIfCachedOption_usesCache() {
    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse,
            safeGreeterSayHelloMethod,
            CallOptions.DEFAULT.withOption(
                SafeMethodCachingInterceptor.ONLY_IF_CACHED_CALL_OPTION, true),
            message);

    assertSame(reply1, reply2);
  }

  @Test
  public void requestWithNoCacheAndOnlyIfCached_fails() {
    try {
      ClientCalls.blockingUnaryCall(
          channelToUse,
          safeGreeterSayHelloMethod,
          CallOptions.DEFAULT
              .withOption(SafeMethodCachingInterceptor.NO_CACHE_CALL_OPTION, true)
              .withOption(SafeMethodCachingInterceptor.ONLY_IF_CACHED_CALL_OPTION, true),
          message);
      fail("Expected call to fail");
    } catch (StatusRuntimeException sre) {
      assertEquals(Status.UNAVAILABLE.getCode(), sre.getStatus().getCode());
      assertEquals(
          "Unsatisfiable Request (no-cache and only-if-cached conflict)",
          sre.getStatus().getDescription());
    }
  }

  @Test
  public void responseNoCacheDirective_notCached() throws Exception {
    cacheControlDirectives.add("no-cache");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
    assertNotEquals(reply1, reply2);
    Truth.assertThat(cache.internalCache).isEmpty();
    Truth.assertThat(cache.removedKeys).isEmpty();
  }

  @Test
  public void responseNoStoreDirective_notCached() throws Exception {
    cacheControlDirectives.add("no-store");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
    Truth.assertThat(cache.internalCache).isEmpty();
    Truth.assertThat(cache.removedKeys).isEmpty();
  }

  @Test
  public void responseNoTransformDirective_notCached() throws Exception {
    cacheControlDirectives.add("no-transform");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
    Truth.assertThat(cache.internalCache).isEmpty();
    Truth.assertThat(cache.removedKeys).isEmpty();
  }

  @Test
  public void responseMustRevalidateDirective_isIgnored() throws Exception {
    cacheControlDirectives.add("must-revalidate");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertSame(reply1, reply2);
  }

  @Test
  public void responseMaxAge_caseInsensitive() throws Exception {
    cacheControlDirectives.add("MaX-aGe=0");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
    Truth.assertThat(cache.internalCache).isEmpty();
    Truth.assertThat(cache.removedKeys).isEmpty();
  }

  @Test
  public void responseNoCache_caseInsensitive() throws Exception {
    cacheControlDirectives.add("No-CaCHe");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
    Truth.assertThat(cache.internalCache).isEmpty();
    Truth.assertThat(cache.removedKeys).isEmpty();
  }

  @Test
  public void combinedResponseCacheControlDirectives_parsesWithoutError() throws Exception {
    cacheControlDirectives.add("max-age=1,no-store , no-cache");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
    Truth.assertThat(cache.internalCache).isEmpty();
    Truth.assertThat(cache.removedKeys).isEmpty();
  }

  @Test
  public void separateResponseCacheControlDirectives_parsesWithoutError() throws Exception {
    cacheControlDirectives.add("max-age=1");
    cacheControlDirectives.add("no-store , no-cache");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
    Truth.assertThat(cache.internalCache).isEmpty();
    Truth.assertThat(cache.removedKeys).isEmpty();
  }

  @Test
  public void afterResponseMaxAge_cacheEntryInvalidated() throws Exception {
    cacheControlDirectives.add("max-age=1");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    assertSame(reply1, reply2);

    // Wait for cache entry to expire
    sleepAtLeast(1001);

    assertNotEquals(
        reply1,
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message));
    Truth.assertThat(cache.removedKeys).hasSize(1);
    assertEquals(
        new SafeMethodCachingInterceptor.Key(
            GreeterGrpc.getSayHelloMethod().getFullMethodName(), message),
        cache.removedKeys.get(0));
  }

  @Test
  public void invalidResponseMaxAge_usesDefault() throws Exception {
    SafeMethodCachingInterceptor interceptorWithCustomMaxAge =
        SafeMethodCachingInterceptor.newSafeMethodCachingInterceptor(cache, 1);
    channelToUse = ClientInterceptors.intercept(baseChannel, interceptorWithCustomMaxAge);
    cacheControlDirectives.add("max-age=-10");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    assertEquals(reply1, reply2);

    // Wait for cache entry to expire
    sleepAtLeast(1001);

    assertNotEquals(
        reply1,
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message));
    Truth.assertThat(cache.removedKeys).hasSize(1);
    assertEquals(
        new SafeMethodCachingInterceptor.Key(
            GreeterGrpc.getSayHelloMethod().getFullMethodName(), message),
        cache.removedKeys.get(0));
  }

  @Test
  public void responseMaxAgeZero_notAddedToCache() throws Exception {
    cacheControlDirectives.add("max-age=0");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
    Truth.assertThat(cache.internalCache).isEmpty();
    Truth.assertThat(cache.removedKeys).isEmpty();
  }

  @Test
  public void cacheHit_doesNotResetExpiration() throws Exception {
    cacheControlDirectives.add("max-age=1");

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    sleepAtLeast(1001);

    HelloReply reply3 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);

    assertSame(reply1, reply2);
    assertNotEquals(reply1, reply3);
    Truth.assertThat(cache.internalCache).hasSize(1);
    Truth.assertThat(cache.removedKeys).hasSize(1);
  }

  @Test
  public void afterDefaultMaxAge_cacheEntryInvalidated() throws Exception {
    SafeMethodCachingInterceptor interceptorWithCustomMaxAge =
        SafeMethodCachingInterceptor.newSafeMethodCachingInterceptor(cache, 1);
    channelToUse = ClientInterceptors.intercept(baseChannel, interceptorWithCustomMaxAge);

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    assertSame(reply1, reply2);

    // Wait for cache entry to expire
    sleepAtLeast(1001);

    assertNotEquals(
        reply1,
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message));
    Truth.assertThat(cache.removedKeys).hasSize(1);
    assertEquals(
        new SafeMethodCachingInterceptor.Key(
            GreeterGrpc.getSayHelloMethod().getFullMethodName(), message),
        cache.removedKeys.get(0));
  }

  @Test
  public void unsafeCallsAreNotCached() {
    GreeterGrpc.GreeterBlockingStub stub = GreeterGrpc.newBlockingStub(channelToUse);

    HelloReply reply1 = stub.sayHello(message);
    HelloReply reply2 = stub.sayHello(message);

    assertNotEquals(reply1, reply2);
  }

  @Test
  public void differentMethodCallsAreNotConflated() {
    MethodDescriptor<HelloRequest, HelloReply> anotherSafeMethod =
        GreeterGrpc.getSayAnotherHelloMethod().toBuilder().setSafe(true).build();

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, anotherSafeMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
  }

  @Test
  public void differentServiceCallsAreNotConflated() {
    MethodDescriptor<HelloRequest, HelloReply> anotherSafeMethod =
        AnotherGreeterGrpc.getSayHelloMethod().toBuilder().setSafe(true).build();

    HelloReply reply1 =
        ClientCalls.blockingUnaryCall(
            channelToUse, safeGreeterSayHelloMethod, CallOptions.DEFAULT, message);
    HelloReply reply2 =
        ClientCalls.blockingUnaryCall(
            channelToUse, anotherSafeMethod, CallOptions.DEFAULT, message);

    assertNotEquals(reply1, reply2);
  }

  private static void sleepAtLeast(long millis) throws InterruptedException {
    long delay = TimeUnit.MILLISECONDS.toNanos(millis);
    long end = System.nanoTime() + delay;
    while (delay > 0) {
      TimeUnit.NANOSECONDS.sleep(delay);
      delay = end - System.nanoTime();
    }
  }

  private static class TestCache implements SafeMethodCachingInterceptor.Cache {
    private Map<SafeMethodCachingInterceptor.Key, SafeMethodCachingInterceptor.Value>
        internalCache =
            new HashMap<SafeMethodCachingInterceptor.Key, SafeMethodCachingInterceptor.Value>();
    private List<SafeMethodCachingInterceptor.Key> removedKeys =
        new ArrayList<SafeMethodCachingInterceptor.Key>();

    @Override
    public void put(
        SafeMethodCachingInterceptor.Key key, SafeMethodCachingInterceptor.Value value) {
      internalCache.put(key, value);
    }

    @Override
    public SafeMethodCachingInterceptor.Value get(SafeMethodCachingInterceptor.Key key) {
      return internalCache.get(key);
    }

    @Override
    public void remove(SafeMethodCachingInterceptor.Key key) {
      removedKeys.add(key);
      internalCache.remove(key);
    }

    @Override
    public void clear() {}
  }
}
