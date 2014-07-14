package com.google.net.stubby.http2.netty;

import com.google.net.stubby.Request;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Response;
import com.google.net.stubby.Session;
import com.google.net.stubby.transport.MessageFramer;

import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * An implementation of {@link Session} that can be used by clients to start
 * a {@link Request}
 */
public class Http2Session implements Session {

  public static final String PROTORPC = "application/protorpc";

  private final Http2Codec.Http2Writer writer;
  private final RequestRegistry requestRegistry;
  private final AtomicInteger streamId;

  public Http2Session(Http2Codec.Http2Writer writer, RequestRegistry requestRegistry) {
    this.writer = writer;
    this.requestRegistry = requestRegistry;
    // Clients are odd numbers starting at 3. A value of 1 is reserved for the upgrade protocol.
    streamId = new AtomicInteger(3);
  }

  private int getNextStreamId() {
    return streamId.getAndAdd(2);
  }

  @Override
  public Request startRequest(String operationName, Map<String, String> headers,
                              Response.ResponseBuilder response) {
    int nextSessionId = getNextStreamId();
    Request operation = new Http2Request(response.build(nextSessionId), operationName,
        headers, writer, new MessageFramer(4096));
    requestRegistry.register(operation);
    return operation;
  }
}
