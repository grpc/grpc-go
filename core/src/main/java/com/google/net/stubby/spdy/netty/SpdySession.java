package com.google.net.stubby.spdy.netty;

import com.google.net.stubby.Request;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Response;
import com.google.net.stubby.Session;
import com.google.net.stubby.transport.MessageFramer;

import io.netty.channel.Channel;

import java.util.concurrent.atomic.AtomicInteger;

/**
 * An implementation of {@link Session} that can be used by clients to start
 * a {@link Request}
 */
public class SpdySession implements Session {

  public static final String PROTORPC = "application/protorpc";

  private final Channel channel;
  private final boolean clientSession;
  private final RequestRegistry requestRegistry;
  private AtomicInteger sessionId;

  public SpdySession(Channel channel, RequestRegistry requestRegistry) {
    this.channel = channel;
    this.clientSession = true;
    this.requestRegistry = requestRegistry;
    // Clients are odd numbers starting at 1, servers are even numbers stating at 2
    sessionId = new AtomicInteger(1);
  }

  private int getNextStreamId() {
    return (sessionId.getAndIncrement() * 2) + (clientSession ? -1 : 0);
  }

  @Override
  public Request startRequest(String operationName, Response.ResponseBuilder response) {
    int nextSessionId = getNextStreamId();
    Request operation = new SpdyRequest(response.build(nextSessionId), channel, operationName,
        new MessageFramer(4096));
    requestRegistry.register(operation);
    return operation;
  }
}
