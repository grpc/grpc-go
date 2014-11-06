package com.google.net.stubby.stub;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Call;
import com.google.net.stubby.Channel;
import com.google.net.stubby.ClientInterceptor;
import com.google.net.stubby.ClientInterceptors.ForwardingCall;
import com.google.net.stubby.ClientInterceptors.ForwardingListener;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.Status;

import java.util.concurrent.atomic.AtomicReference;

/**
 * Utility functions for binding and receiving headers
 */
public class MetadataUtils {

  /**
   * Attach a set of request headers to a stub.
   * @param stub to bind the headers to.
   * @param extraHeaders the headers to be passed by each call on the returned stub.
   * @return an implementation of the stub with extraHeaders bound to each call.
   */
  @SuppressWarnings({"unchecked", "rawtypes"})
  public static <T extends AbstractStub> T attachHeaders(
      T stub,
      final Metadata.Headers extraHeaders) {
    return (T) stub.configureNewStub().addInterceptor(
        newAttachHeadersInterceptor(extraHeaders)).build();
  }

  /**
   * Return a client interceptor that attaches a set of headers to requests.
   *
   * @param extraHeaders the headers to be passed by each call that is processed by the returned
   *                     interceptor
   */
  public static ClientInterceptor newAttachHeadersInterceptor(final Metadata.Headers extraHeaders) {
    return new ClientInterceptor() {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
          Channel next) {
        return new ForwardingCall<ReqT, RespT>(next.newCall(method)) {
          @Override
          public void start(Listener<RespT> responseListener, Metadata.Headers headers) {
            headers.merge(extraHeaders);
            super.start(responseListener, headers);
          }
        };
      }
    };
  }

  /**
   * Capture the last received metadata for a stub. Useful for testing
   * @param stub to capture for
   * @param headersCapture to record the last received headers
   * @param trailersCapture to record the last received trailers
   * @return an implementation of the stub with extraHeaders bound to each call.
   */
  @SuppressWarnings({"unchecked", "rawtypes"})
  public static <T extends AbstractStub> T captureMetadata(
      T stub,
      AtomicReference<Metadata.Headers> headersCapture,
      AtomicReference<Metadata.Trailers> trailersCapture) {
    return (T) stub.configureNewStub().addInterceptor(
        newCaptureMetadataInterceptor(headersCapture, trailersCapture)).build();
  }

  /**
   * Capture the last received metadata on a channel. Useful for testing
   *
   * @param channel to channel to capture for.
   * @param headersCapture to record the last received headers
   * @param trailersCapture to record the last received trailers
   * @return an implementation of the channel with captures installed.
   */
  @SuppressWarnings("unchecked")
  public static ClientInterceptor newCaptureMetadataInterceptor(
      final AtomicReference<Metadata.Headers> headersCapture,
      final AtomicReference<Metadata.Trailers> trailersCapture) {
    return new ClientInterceptor() {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
          Channel next) {
        return new ForwardingCall<ReqT, RespT>(next.newCall(method)) {
          @Override
          public void start(Listener<RespT> responseListener, Metadata.Headers headers) {
            headersCapture.set(null);
            trailersCapture.set(null);
            super.start(new ForwardingListener<RespT>(responseListener) {
              @Override
              public ListenableFuture<Void> onHeaders(Metadata.Headers headers) {
                headersCapture.set(headers);
                return super.onHeaders(headers);
              }

              @Override
              public void onClose(Status status, Metadata.Trailers trailers) {
                trailersCapture.set(trailers);
                super.onClose(status, trailers);
              }
            }, headers);
          }
        };
      }
    };
  }
}
