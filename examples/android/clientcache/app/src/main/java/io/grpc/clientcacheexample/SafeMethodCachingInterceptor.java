package io.grpc.clientcacheexample;

import android.util.Log;
import android.util.LruCache;
import com.google.common.base.Splitter;
import com.google.protobuf.MessageLite;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.Deadline;
import io.grpc.ForwardingClientCall;
import io.grpc.ForwardingClientCallListener;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import java.util.Locale;
import java.util.Objects;
import java.util.concurrent.TimeUnit;

/**
 * An example of an on-device cache for Android implemented using the {@link ClientInterceptor} API.
 *
 * <p>Client-side cache-control directives are not directly supported. Instead, two call options can
 * be added to the call: no-cache (always go to the network) or only-if-cached (never use network;
 * if response is not in cache, the request fails).
 *
 * <p>This interceptor respects the cache-control directives in the server's response: max-age
 * determines when the cache entry goes stale. no-cache, no-store, and no-transform entirely skip
 * caching of the response. must-revalidate is ignored, as the cache does not support returning
 * stale responses.
 *
 * <p>Note: other response headers besides cache-control (such as Expiration, Varies) are ignored by
 * this implementation.
 */
final class SafeMethodCachingInterceptor implements ClientInterceptor {
  static CallOptions.Key<Boolean> NO_CACHE_CALL_OPTION = CallOptions.Key.of("no-cache", false);
  static CallOptions.Key<Boolean> ONLY_IF_CACHED_CALL_OPTION =
      CallOptions.Key.of("only-if-cached", false);
  private static final String TAG = "grpcCacheExample";

  public static final class Key {
    private final String fullMethodName;
    private final MessageLite request;

    public Key(String fullMethodName, MessageLite request) {
      this.fullMethodName = fullMethodName;
      this.request = request;
    }

    @Override
    public boolean equals(Object object) {
      if (object instanceof Key) {
        Key other = (Key) object;
        return Objects.equals(this.fullMethodName, other.fullMethodName)
            && Objects.equals(this.request, other.request);
      }
      return false;
    }

    @Override
    public int hashCode() {
      return Objects.hash(fullMethodName, request);
    }
  }

  public static final class Value {
    private final MessageLite response;
    private final Deadline maxAgeDeadline;

    public Value(MessageLite response, Deadline maxAgeDeadline) {
      this.response = response;
      this.maxAgeDeadline = maxAgeDeadline;
    }

    @Override
    public boolean equals(Object object) {
      if (object instanceof Value) {
        Value other = (Value) object;
        return Objects.equals(this.response, other.response)
            && Objects.equals(this.maxAgeDeadline, other.maxAgeDeadline);
      }
      return false;
    }

    @Override
    public int hashCode() {
      return Objects.hash(response, maxAgeDeadline);
    }
  }

  public interface Cache {
    void put(Key key, Value value);

    Value get(Key key);

    void remove(Key key);

    void clear();
  }

  /**
   * Obtain a new cache with a least-recently used eviction policy and the specified size limit. The
   * backing caching implementation is provided by {@link LruCache}. It is safe for a single cache
   * to be shared across multiple {@link SafeMethodCachingInterceptor}s without synchronization.
   */
  public static Cache newLruCache(final int cacheSizeInBytes) {
    return new Cache() {
      private final LruCache<Key, Value> lruCache =
          new LruCache<Key, Value>(cacheSizeInBytes) {
            protected int sizeOf(Key key, Value value) {
              return value.response.getSerializedSize();
            }
          };

      @Override
      public void put(Key key, Value value) {
        lruCache.put(key, value);
      }

      @Override
      public Value get(Key key) {
        return lruCache.get(key);
      }

      @Override
      public void remove(Key key) {
        lruCache.remove(key);
      }

      @Override
      public void clear() {
        lruCache.evictAll();
      }
    };
  }

  public static SafeMethodCachingInterceptor newSafeMethodCachingInterceptor(Cache cache) {
    return newSafeMethodCachingInterceptor(cache, DEFAULT_MAX_AGE_SECONDS);
  }

  public static SafeMethodCachingInterceptor newSafeMethodCachingInterceptor(
      Cache cache, int defaultMaxAge) {
    return new SafeMethodCachingInterceptor(cache, defaultMaxAge);
  }

  private static int DEFAULT_MAX_AGE_SECONDS = 3600;

  private static final Metadata.Key<String> CACHE_CONTROL_KEY =
      Metadata.Key.of("cache-control", Metadata.ASCII_STRING_MARSHALLER);

  private static final Splitter CACHE_CONTROL_SPLITTER =
      Splitter.on(',').trimResults().omitEmptyStrings();

  private final Cache internalCache;
  private final int defaultMaxAge;

  private SafeMethodCachingInterceptor(Cache cache, int defaultMaxAge) {
    this.internalCache = cache;
    this.defaultMaxAge = defaultMaxAge;
  }

  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
      final MethodDescriptor<ReqT, RespT> method, final CallOptions callOptions, Channel next) {
    // Currently only unary methods can be marked safe, but check anyways.
    if (!method.isSafe() || method.getType() != MethodDescriptor.MethodType.UNARY) {
      return next.newCall(method, callOptions);
    }

    final String fullMethodName = method.getFullMethodName();

    return new ForwardingClientCall.SimpleForwardingClientCall<ReqT, RespT>(
        next.newCall(method, callOptions)) {
      private Listener<RespT> interceptedListener;
      private Key requestKey;
      private boolean cacheResponse = true;
      private volatile String cacheOptionsErrorMsg;

      @Override
      public void start(Listener<RespT> responseListener, Metadata headers) {
        interceptedListener =
            new ForwardingClientCallListener.SimpleForwardingClientCallListener<RespT>(
                responseListener) {
              private Deadline deadline;
              private int maxAge = -1;

              @Override
              public void onHeaders(Metadata headers) {
                Iterable<String> cacheControlHeaders = headers.getAll(CACHE_CONTROL_KEY);
                if (cacheResponse && cacheControlHeaders != null) {
                  for (String cacheControlHeader : cacheControlHeaders) {
                    for (String directive : CACHE_CONTROL_SPLITTER.split(cacheControlHeader)) {
                      if (directive.equalsIgnoreCase("no-cache")) {
                        cacheResponse = false;
                        break;
                      } else if (directive.equalsIgnoreCase("no-store")) {
                        cacheResponse = false;
                        break;
                      } else if (directive.equalsIgnoreCase("no-transform")) {
                        cacheResponse = false;
                        break;
                      } else if (directive.toLowerCase(Locale.US).startsWith("max-age")) {
                        String[] parts = directive.split("=");
                        if (parts.length == 2) {
                          try {
                            maxAge = Integer.parseInt(parts[1]);
                          } catch (NumberFormatException e) {
                            Log.e(TAG, "max-age directive failed to parse", e);
                            continue;
                          }
                        }
                      }
                    }
                  }
                }
                if (cacheResponse) {
                  if (maxAge > -1) {
                    deadline = Deadline.after(maxAge, TimeUnit.SECONDS);
                  } else {
                    deadline = Deadline.after(defaultMaxAge, TimeUnit.SECONDS);
                  }
                }
                super.onHeaders(headers);
              }

              @Override
              public void onMessage(RespT message) {
                if (cacheResponse && !deadline.isExpired()) {
                  Value value = new Value((MessageLite) message, deadline);
                  internalCache.put(requestKey, value);
                }
                super.onMessage(message);
              }

              @Override
              public void onClose(Status status, Metadata trailers) {
                if (cacheOptionsErrorMsg != null) {
                  // UNAVAILABLE is the canonical gRPC mapping for HTTP response code 504 (as used
                  // by the built-in Android HTTP request cache).
                  super.onClose(
                      Status.UNAVAILABLE.withDescription(cacheOptionsErrorMsg), new Metadata());
                } else {
                  super.onClose(status, trailers);
                }
              }
            };
        delegate().start(interceptedListener, headers);
      }

      @Override
      public void sendMessage(ReqT message) {
        boolean noCache = callOptions.getOption(NO_CACHE_CALL_OPTION);
        boolean onlyIfCached = callOptions.getOption(ONLY_IF_CACHED_CALL_OPTION);

        if (noCache) {
          if (onlyIfCached) {
            cacheOptionsErrorMsg = "Unsatisfiable Request (no-cache and only-if-cached conflict)";
            super.cancel(cacheOptionsErrorMsg, null);
            return;
          }
          cacheResponse = false;
          super.sendMessage(message);
          return;
        }

        // Check the cache
        requestKey = new Key(fullMethodName, (MessageLite) message);
        Value cachedResponse = internalCache.get(requestKey);
        if (cachedResponse != null) {
          if (cachedResponse.maxAgeDeadline.isExpired()) {
            internalCache.remove(requestKey);
          } else {
            cacheResponse = false; // already cached
            interceptedListener.onMessage((RespT) cachedResponse.response);
            Metadata metadata = new Metadata();
            interceptedListener.onClose(Status.OK, metadata);
            return;
          }
        }

        if (onlyIfCached) {
          cacheOptionsErrorMsg =
              "Unsatisfiable Request (only-if-cached set, but value not in cache)";
          super.cancel(cacheOptionsErrorMsg, null);
          return;
        }
        super.sendMessage(message);
      }

      @Override
      public void halfClose() {
        if (cacheOptionsErrorMsg != null) {
          // already canceled
          return;
        }
        super.halfClose();
      }
    };
  }
}
