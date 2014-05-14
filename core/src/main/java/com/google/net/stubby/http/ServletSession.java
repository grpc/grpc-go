package com.google.net.stubby.http;

import com.google.common.io.ByteBuffers;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.AbstractResponse;
import com.google.net.stubby.Operation;
import com.google.net.stubby.Request;
import com.google.net.stubby.Response;
import com.google.net.stubby.Session;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Framer;
import com.google.net.stubby.transport.MessageFramer;
import com.google.net.stubby.transport.Transport;
import com.google.net.stubby.transport.TransportFrameUtil;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.util.concurrent.Executor;

import javax.servlet.ServletException;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

/**
 * A server-only session to be used with a servlet engine. The wrapped session MUST use a
 * same-thread executor for work-dispatch
 */
// TODO(user) Support a more flexible threading model than same-thread
// TODO(user) Investigate Servlet3 compliance, in particular thread detaching
public class ServletSession extends HttpServlet {

  public static final String PROTORPC = "application/protorpc";
  public static final String CONTENT_TYPE = "content-type";

  private final Session session;
  private final Executor executor;

  public ServletSession(Session session, Executor executor) {
    this.session = session;
    this.executor = executor;
  }

  @Override
  public String getServletName() {
    return "gRPCServlet";
  }

  @Override
  protected void doPost(HttpServletRequest req, HttpServletResponse resp) throws ServletException,
      IOException {
    try {
      final SettableFuture<Void> requestCompleteFuture = SettableFuture.create();
      final ResponseStream responseStream = new ResponseStream(resp, requestCompleteFuture);
      final Request request = startRequest(req, resp, responseStream);
      if (request == null) {
        return;
      }

      // Deframe the request and begin the response processing.
      new HttpStreamDeframer().deframe(req.getInputStream(), request);
      request.close(Status.OK);

      // Notify the response processing that the request is complete.
      requestCompleteFuture.set(null);

      // Block until the response is complete.
      responseStream.getResponseCompleteFuture().get();

    } catch (Exception e) {
      throw new RuntimeException(e);
    }
  }

  /**
   * Start the Request operation on the server
   */
  private Request startRequest(HttpServletRequest req, HttpServletResponse resp,
      ResponseStream responseStream) throws IOException {
    // TODO(user): Move into shared utility
    if (!PROTORPC.equals(req.getHeader(CONTENT_TYPE))) {
      resp.sendError(HttpServletResponse.SC_UNSUPPORTED_MEDIA_TYPE,
          "The only supported content-type is " + PROTORPC);
      return null;
    }
    // Use Path to specify the operation
    String operationName = normalizeOperationName(req.getPathInfo());
    if (operationName == null) {
      resp.sendError(HttpServletResponse.SC_NOT_FOUND);
      return null;
    }
    // Create the operation and bind an HTTP response operation
    Request op = session.startRequest(operationName, HttpResponseOperation.builder(responseStream));
    if (op == null) {
      // TODO(user): Unify error handling once spec finalized
      resp.sendError(HttpServletResponse.SC_NOT_FOUND, "Unknown RPC operation");
      return null;
    }
    return op;
  }

  private String normalizeOperationName(String path) {
    // TODO(user): This is where we would add path-namespacing of different implementations
    // of services so they do not collide. For the moment this is not supported.
    return path.substring(1);
  }

  /**
   * Implementation of {@link Response}
   */
  private static class HttpResponseOperation extends AbstractResponse implements Framer.Sink {

    static ResponseBuilder builder(final ResponseStream responseStream) {
      return new ResponseBuilder() {
        @Override
        public Response build(int id) {
          return new HttpResponseOperation(id, responseStream);
        }

        @Override
        public Response build() {
          return new HttpResponseOperation(-1, responseStream);
        }
      };
    }

    private final MessageFramer framer;
    private final ResponseStream responseStream;

    private HttpResponseOperation(int id, ResponseStream responseStream) {
      super(id);
      this.responseStream = responseStream;
      // Always use no compression framing and treat the stream as one large frame
      framer = new MessageFramer(4096);
      framer.setAllowCompression(false);
      try {
        responseStream.write(TransportFrameUtil.NO_COMPRESS_FLAG);
      } catch (IOException ioe) {
        close(new Status(Transport.Code.INTERNAL, ioe));
      }
    }

    @Override
    public Operation addContext(String type, InputStream message, Phase nextPhase) {
      super.addContext(type, message, nextPhase);
      framer.writeContext(type, message, getPhase() == Phase.CLOSED, this);
      return this;
    }

    @Override
    public Operation addPayload(InputStream payload, Phase nextPhase) {
      super.addPayload(payload, Phase.PAYLOAD);
      framer.writePayload(payload, false, this);
      if (nextPhase == Phase.CLOSED) {
        close(Status.OK);
      }
      return this;
    }

    @Override
    public Operation close(Status status) {
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
        // Skip the frame flag as we don't care about it for streaming output
        frame.position(1);
        ByteBuffers.asByteSource(frame).copyTo(responseStream);
      } catch (Throwable t) {
        close(new Status(Transport.Code.INTERNAL, t));
      } finally {
        if (closed && endOfMessage) {
          framer.close();
          responseStream.close();
        }
      }
    }
  }

  /**
   * Wraps the HTTP response {@link OutputStream}. Will buffer bytes until the request is
   * complete. It will then flush its buffer to the output stream and all subsequent writes
   * will go directly to the HTTP response.
   */
  private class ResponseStream extends OutputStream {
    private final SettableFuture<Void> responseCompleteFuture = SettableFuture.create();
    private final ByteArrayOutputStream buffer = new ByteArrayOutputStream();
    private final HttpServletResponse httpResponse;
    private volatile boolean requestComplete;
    private volatile boolean closed;

    public ResponseStream(HttpServletResponse httpResponse,
        SettableFuture<Void> requestCompleteFuture) {
      this.httpResponse = httpResponse;
      httpResponse.setHeader("Content-Type", PROTORPC);

      requestCompleteFuture.addListener(new Runnable() {
        @Override
        public void run() {
          onRequestComplete();
        }
      }, executor);
    }

    public ListenableFuture<Void> getResponseCompleteFuture() {
      return responseCompleteFuture;
    }

    @Override
    public void close() {
      synchronized (buffer) {
        closed = true;

        // If all the data has been written to the output stream, finish the response.
        if (buffer.size() == 0) {
          finish();
        }
      }
    }

    @Override
    public void write(int b) throws IOException {
      write(new byte[] {(byte) b}, 0, 1);
    }

    @Override
    public void write(byte[] b) throws IOException {
      write(b, 0, b.length);
    }

    @Override
    public void write(byte[] b, int off, int len) throws IOException {
      // This loop will execute at most 2 times.
      while (true) {
        if (requestComplete) {
          // The request already completed, just write directly to the response output stream.
          httpResponse.getOutputStream().write(b, off, len);
          return;
        }

        synchronized (buffer) {
          if (requestComplete) {
            // Handle the case that we completed the request just after the first check
            // above. Just go back to the top of the loop and write directly to the response.
            continue;
          }

          // Request hasn't completed yet, buffer the data for now.
          buffer.write(b, off, len);
          return;
        }
      }
    }

    private void onRequestComplete() {
      try {
        // Write the content of the buffer to the HTTP response.
        synchronized (buffer) {
          if (buffer.size() > 0) {
            httpResponse.getOutputStream().write(buffer.toByteArray());
            buffer.reset();
          }
          requestComplete = true;

          if (closed) {
            // The response is complete, finish the response.
            finish();
          }
        }
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
    }

    /**
     * Closes the HTTP response and sets the future response completion future.
     */
    private void finish() {
      try {
        httpResponse.getOutputStream().close();
        responseCompleteFuture.set(null);
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
    }
  }
}
