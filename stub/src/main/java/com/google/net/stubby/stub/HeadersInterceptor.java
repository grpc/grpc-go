package com.google.net.stubby.stub;

import com.google.net.stubby.Call;
import com.google.net.stubby.Channel;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.context.ForwardingChannel;

/**
 * Utility functions for binding and receiving headers
 */
public class HeadersInterceptor {

  /**
   * Attach a set of request headers to a stub.
   * @param stub to bind the headers to.
   * @param extraHeaders the headers to be passed by each call on the returned stub.
   * @return an implementation of the stub with extraHeaders bound to each call.
   */
  @SuppressWarnings("unchecked")
  public static <T extends AbstractStub> T intercept(
      T stub,
      final Metadata.Headers extraHeaders) {
    return (T) stub.configureNewStub().setChannel(interceptChannel(stub.getChannel(), extraHeaders))
        .build();
  }

  /**
   * Attach a set of request headers to a channel.
   *
   * @param channel to channel to intercept.
   * @param extraHeaders the headers to be passed by each call on the returned stub.
   * @return an implementation of the channel with extraHeaders bound to each call.
   */
  @SuppressWarnings("unchecked")
  public static Channel interceptChannel(Channel channel, final Metadata.Headers extraHeaders) {
    return new ForwardingChannel(channel) {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method) {
        return new ForwardingCall<ReqT, RespT>(delegate.newCall(method)) {
          @Override
          public void start(Listener<RespT> responseListener, Metadata.Headers headers) {
            headers.merge(extraHeaders);
            delegate.start(responseListener, headers);
          }
        };
      }
    };
  }
}
