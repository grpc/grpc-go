package com.google.net.stubby.http;

import com.google.common.io.ByteBuffers;
import com.google.net.stubby.AbstractRequest;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Operation;
import com.google.net.stubby.Response;
import com.google.net.stubby.Session;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Framer;
import com.google.net.stubby.transport.MessageFramer;

import java.io.DataOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URI;
import java.nio.ByteBuffer;

/**
 * Implementation of {@link Session} using {@link HttpURLConnection} for clients. Services
 * are dispatched relative to a base URI.
 */
public class UrlConnectionClientSession implements Session {

  private final URI base;

  public UrlConnectionClientSession(URI base) {
    this.base = base;
  }

  @Override
  public Request startRequest(String operationName, Metadata.Headers headers,
                              Response.ResponseBuilder responseBuilder) {
    return new Request(base.resolve(operationName), headers,
        responseBuilder.build());
  }

  private class Request extends AbstractRequest implements Framer.Sink {

    private final HttpURLConnection connection;
    private final DataOutputStream outputStream;
    private final MessageFramer framer;

    private Request(URI uri, Metadata.Headers headers, Response response) {
      super(response);
      try {
        connection = (HttpURLConnection) uri.toURL().openConnection();
        connection.setDoOutput(true);
        connection.setDoInput(true);
        connection.setRequestMethod("POST");
        connection.setRequestProperty("Content-Type", "application/protorpc");
        String[] serialized = headers.serializeAscii();
        for (int i = 0; i < serialized.length; i++) {
          connection.setRequestProperty(
              serialized[i],
              serialized[++i]);
        }
        outputStream = new DataOutputStream(connection.getOutputStream());
      } catch (IOException t) {
        throw new RuntimeException(t);
      }
      // No compression when framing over HTTP for the moment
      framer = new MessageFramer(4096);
      framer.setAllowCompression(false);
    }

    @Override
    public Operation addPayload(InputStream payload, Phase nextPhase) {
      super.addPayload(payload, nextPhase);
      framer.writePayload(payload, getPhase() == Phase.CLOSED, this);
      return this;
    }

    @Override
    public Operation close(Status status) {
      // TODO(user): This is broken but necessary to get test passing with the introduction
      // of Channel as now for most calls the close() call is decoupled from the last call to
      // addPayload. The real fix is to remove 'nextPhase' from the Operation interface and
      // clean up Framer. For a follow up CL.
      boolean alreadyClosed = getPhase() == Phase.CLOSED;
      super.close(status);
      if (!alreadyClosed) {
        framer.writeStatus(status, true, this);
      }
      return this;
    }

    @Override
    public void deliverFrame(ByteBuffer frame, boolean endOfMessage) {
      boolean closed = getPhase() == Phase.CLOSED;
      try {
        ByteBuffers.asByteSource(frame).copyTo(outputStream);
        if (closed && endOfMessage) {
          connection.getOutputStream().close();
          // The request has completed so now process the response. Must do this in the same
          // thread as URLConnection has threading issues.
          new HttpStreamDeframer().deframe(connection.getInputStream(), getResponse());
          connection.getInputStream().close();
          connection.disconnect();
        }
      } catch (IOException ioe) {
        close(Status.INTERNAL.withCause(ioe));
      }
    }
  }
}
