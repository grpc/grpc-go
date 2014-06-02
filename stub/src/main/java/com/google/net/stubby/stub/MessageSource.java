package com.google.net.stubby.stub;

public interface MessageSource<E> {

  public void produceToSink(MessageSink<E> sink);
}
