package com.google.net.stubby.stub;

public interface MessageSink<E> {

  public void receive(E message, boolean last);

  public void close();
}
