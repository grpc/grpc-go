package com.google.net.stubby.context;

import com.google.common.collect.Maps;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Call;
import com.google.net.stubby.Channel;
import com.google.net.stubby.Marshaller;
import com.google.net.stubby.MethodDescriptor;

import java.io.InputStream;
import java.util.Map;

import javax.annotation.concurrent.NotThreadSafe;
import javax.inject.Provider;

/**
 * A channel implementation that sends bound context values and records received context.
 * Unlike {@Channel} this class is not thread-safe so it is recommended to create an instance
 * per thread.
 */
@NotThreadSafe
public class ContextExchangeChannel extends ForwardingChannel {

  private Map<String, Object> captured;
  private Map<String, Provider<InputStream>> provided;

  public ContextExchangeChannel(Channel channel) {
    super(channel);
    // builder?
    captured = Maps.newTreeMap();
    provided = Maps.newTreeMap();
  }

  @SuppressWarnings("unchecked")
  public <T> Provider<T> receive(final String name, final Marshaller<T> m) {
    synchronized (captured) {
      captured.put(name, null);
    }
    return new Provider<T>() {
      @Override
      public T get() {
        synchronized (captured) {
          Object o = captured.get(name);
          if (o instanceof InputStream) {
            o = m.parse((InputStream) o);
            captured.put(name, o);
          }
          return (T) o;
        }
      }
    };
  }

  public <T> void send(final String name, final T value, final Marshaller<T> m) {
    synchronized (provided) {
      provided.put(name, new Provider<InputStream>() {
        @Override
        public InputStream get() {
          return m.stream(value);
        }
      });
    }
  }

  /**
   * Clear all received values and allow another call
   */
  public void clearLastReceived() {
    synchronized (captured) {
      for (Map.Entry<String, Object> entry : captured.entrySet()) {
        entry.setValue(null);
      }
    }
  }


  @Override
  public <ReqT, RespT> Call<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method) {
    return new CallImpl<ReqT, RespT>(delegate.newCall(method));
  }

  private class CallImpl<ReqT, RespT> extends ForwardingCall<ReqT, RespT> {
    private CallImpl(Call<ReqT, RespT> delegate) {
      super(delegate);
    }

    @Override
    public void start(Listener<RespT> responseListener) {
      super.start(new ListenerImpl<RespT>(responseListener));
      synchronized (provided) {
        for (Map.Entry<String, Provider<InputStream>> entry : provided.entrySet()) {
          sendContext(entry.getKey(), entry.getValue().get());
        }
      }
    }
  }

  private class ListenerImpl<T> extends ForwardingListener<T> {
    private ListenerImpl(Call.Listener<T> delegate) {
      super(delegate);
    }

    @Override
    public ListenableFuture<Void> onContext(String name, InputStream value) {
      synchronized (captured) {
        if (captured.containsKey(name)) {
          captured.put(name, value);
          return null;
        }
      }
      return super.onContext(name, value);
    }
  }
}
