package com.google.net.stubby.newtransport.http;

import static com.google.net.stubby.Status.CANCELLED;
import static com.google.net.stubby.newtransport.HttpUtil.CONTENT_TYPE_HEADER;
import static com.google.net.stubby.newtransport.HttpUtil.CONTENT_TYPE_PROTORPC;
import static com.google.net.stubby.newtransport.HttpUtil.HTTP_METHOD;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.AbstractClientStream;
import com.google.net.stubby.newtransport.AbstractClientTransport;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientStreamListener;
import com.google.net.stubby.newtransport.InputStreamDeframer;
import com.google.net.stubby.newtransport.StreamState;
import com.google.net.stubby.transport.Transport;

import java.io.DataOutputStream;
import java.io.IOException;
import java.net.HttpURLConnection;
import java.net.URI;
import java.nio.ByteBuffer;
import java.util.Collections;
import java.util.HashSet;
import java.util.Set;

/**
 * A simple client-side transport for RPC-over-HTTP/1.1. All execution (including listener
 * callbacks) are executed in the application thread context.
 */
public class HttpClientTransport extends AbstractClientTransport {

  private final URI baseUri;
  private final Set<HttpClientStream> streams =
      Collections.synchronizedSet(new HashSet<HttpClientStream>());

  public HttpClientTransport(URI baseUri) {
    this.baseUri = Preconditions.checkNotNull(baseUri, "baseUri");
  }

  @Override
  protected ClientStream newStreamInternal(MethodDescriptor<?, ?> method,
                                           Metadata.Headers headers,
                                           ClientStreamListener listener) {
    URI uri = baseUri.resolve(method.getName());
    HttpClientStream stream = new HttpClientStream(uri, headers.serializeAscii(), listener);
    synchronized (streams) {
      // Check for RUNNING to deal with race condition of this being executed right after doStop
      // cancels all the streams.
      if (state() != State.RUNNING) {
        throw new IllegalStateException("Invalid state for creating new stream: " + state());
      }
      streams.add(stream);
      return stream;
    }
  }

  @Override
  protected void doStart() {
    notifyStarted();
  }

  @Override
  protected void doStop() {
    // Cancel all of the streams for this transport.
    synchronized (streams) {
      // Guaranteed to be in the STOPPING state here.
      for (HttpClientStream stream : streams.toArray(new HttpClientStream[0])) {
        stream.cancel();
      }
    }
    notifyStopped();
  }

  /**
   * Client stream implementation for an HTTP transport.
   */
  private class HttpClientStream extends AbstractClientStream {
    final HttpURLConnection connection;
    final DataOutputStream outputStream;
    boolean connected;

    HttpClientStream(URI uri, String[] headers, ClientStreamListener listener) {
      super(listener);

      try {
        connection = (HttpURLConnection) uri.toURL().openConnection();
        connection.setDoOutput(true);
        connection.setDoInput(true);
        connection.setRequestMethod(HTTP_METHOD);
        connection.setRequestProperty(CONTENT_TYPE_HEADER, CONTENT_TYPE_PROTORPC);
        for (int i = 0; i < headers.length; i++) {
          connection.setRequestProperty(headers[i], headers[++i]);
        }
        outputStream = new DataOutputStream(connection.getOutputStream());
        connected = true;
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
    }

    @Override
    public void cancel() {
      outboundPhase = Phase.STATUS;
      if (setStatus(CANCELLED, new Metadata.Trailers())) {
        disconnect();
      }
    }

    @Override
    protected void sendFrame(ByteBuffer frame, boolean endOfStream) {
      if (state() == StreamState.CLOSED) {
        // Ignore outbound frames after the stream has closed.
        return;
      }

      try {
        // Synchronizing here to protect against cancellation due to the transport shutting down.
        synchronized (connection) {
          // Write the data to the connection output stream.
          outputStream.write(frame.array(), frame.arrayOffset(), frame.remaining());

          if (endOfStream) {
            // Close the output stream on this connection.
            connection.getOutputStream().close();

            // The request has completed so now process the response. This results in the listener's
            // closed() callback being invoked since we're indicating that this is the end of the
            // response stream.
            //
            // NOTE: Must read the response in the sending thread, since URLConnection has threading
            // issues.
            new InputStreamDeframer(inboundMessageHandler()).deliverFrame(
                connection.getInputStream(), true);

            // Close the input stream and disconnect.
            connection.getInputStream().close();
            disconnect();
          }
        }
      } catch (IOException ioe) {
        setStatus(new Status(Transport.Code.INTERNAL, ioe), new Metadata.Trailers());
      }
    }

    @Override
    public void dispose() {
      super.dispose();
      disconnect();
    }

    /**
     * This implementation does nothing since HTTP/1.1 does not support flow control.
     */
    @Override
    protected void disableWindowUpdate(ListenableFuture<Void> processingFuture) {
    }

    /**
     * Disconnects the HTTP connection if currently connected.
     */
    private void disconnect() {
      // Synchronizing since this may be called for the stream (i.e. cancel or read complete) or
      // due to shutting down the transport (i.e. cancel).
      synchronized (connection) {
        if (connected) {
          connected = false;
          streams.remove(this);
          connection.disconnect();
        }
      }
    }
  }
}
