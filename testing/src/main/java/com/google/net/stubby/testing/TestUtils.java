package com.google.net.stubby.testing;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.ServerCall;
import com.google.net.stubby.ServerCallHandler;
import com.google.net.stubby.ServerInterceptor;
import com.google.net.stubby.ServerInterceptors;
import com.google.net.stubby.Status;

import java.util.Arrays;
import java.util.HashSet;
import java.util.Set;

/**
 * Common utility functions useful for writing tests.
 */
public class TestUtils {

  /**
   * Echo the request headers from a client into response headers and trailers. Useful for
   * testing end-to-end metadata propagation.
   */
  public static ServerInterceptor echoRequestHeadersInterceptor(Metadata.Key... keys) {
    final Set<Metadata.Key> keySet = new HashSet<Metadata.Key>(Arrays.asList(keys));
    return new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(String method,
           ServerCall<RespT> call,
           final Metadata.Headers requestHeaders,
           ServerCallHandler<ReqT, RespT> next) {
        ServerCall.Listener<ReqT> listener = next.startCall(method,
            new ServerInterceptors.ForwardingServerCall<RespT>(call) {
              boolean sentHeaders;

              @Override
              public void sendHeaders(Metadata.Headers responseHeaders) {
                responseHeaders.merge(requestHeaders, keySet);
                super.sendHeaders(responseHeaders);
                sentHeaders = true;
              }

              @Override
              public void sendPayload(RespT payload) {
                if (!sentHeaders) {
                  sendHeaders(new Metadata.Headers());
                }
                super.sendPayload(payload);
              }

              @Override
              public void close(Status status, Metadata.Trailers trailers) {
                trailers.merge(requestHeaders, keySet);
                super.close(status, trailers);
              }
            }, requestHeaders);
        return listener;
      }
    };
  }

  private TestUtils() {}
}
