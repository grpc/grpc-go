package com.google.net.stubby.transport.okhttp;

import com.google.common.base.Preconditions;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.ClientStreamListener;
import com.google.net.stubby.transport.Http2ClientStream;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.Header;

import java.nio.ByteBuffer;
import java.util.List;
import java.util.concurrent.Executor;

import javax.annotation.concurrent.GuardedBy;

/**
 * Client stream for the okhttp transport.
 */
class OkHttpClientStream extends Http2ClientStream {

  private static int WINDOW_UPDATE_THRESHOLD =
      OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE / 2;

  /**
   * Construct a new client stream.
   */
  static OkHttpClientStream newStream(final Executor executor, ClientStreamListener listener,
                                       AsyncFrameWriter frameWriter,
                                       OkHttpClientTransport transport,
                                       OutboundFlowController outboundFlow) {
    // Create a lock object that can be used by both the executor and methods in the stream
    // to ensure consistent locking behavior.
    final Object executorLock = new Object();
    Executor synchronizingExecutor = new Executor() {
      @Override
      public void execute(final Runnable command) {
        executor.execute(new Runnable() {
          @Override
          public void run() {
            synchronized (executorLock) {
              command.run();
            }
          }
        });
      }
    };
    return new OkHttpClientStream(synchronizingExecutor, listener, frameWriter, transport,
        executorLock, outboundFlow);
  }

  @GuardedBy("executorLock")
  private int window = OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE;
  @GuardedBy("executorLock")
  private int processedWindow = OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE;
  private final AsyncFrameWriter frameWriter;
  private final OutboundFlowController outboundFlow;
  private final OkHttpClientTransport transport;
  // Lock used to synchronize with work done on the executor.
  private final Object executorLock;
  private Object outboundFlowState;

  private OkHttpClientStream(final Executor executor,
                     final ClientStreamListener listener,
                     AsyncFrameWriter frameWriter,
                     OkHttpClientTransport transport,
                     Object executorLock,
                     OutboundFlowController outboundFlow) {
    super(listener, null, executor);
    if (!GRPC_V2_PROTOCOL) {
      throw new RuntimeException("okhttp transport can only work with V2 protocol!");
    }
    this.frameWriter = frameWriter;
    this.transport = transport;
    this.executorLock = executorLock;
    this.outboundFlow = outboundFlow;
  }

  public void transportHeadersReceived(List<Header> headers, boolean endOfStream) {
    synchronized (executorLock) {
      if (endOfStream) {
        transportTrailersReceived(Utils.convertTrailers(headers));
      } else {
        transportHeadersReceived(Utils.convertHeaders(headers));
      }
    }
  }

  /**
   * We synchronized on "executorLock" for delivering frames and updating window size, so that
   * the future listeners (executed by synchronizedExecutor) will not be executed in the same time.
   */
  public void transportDataReceived(okio.Buffer frame, boolean endOfStream) {
    synchronized (executorLock) {
      long length = frame.size();
      window -= length;
      super.transportDataReceived(new OkHttpBuffer(frame), endOfStream);
    }
  }

  @Override
  protected void sendFrame(ByteBuffer frame, boolean endOfStream) {
    Preconditions.checkState(id() != 0, "streamId should be set");
    okio.Buffer buffer = new okio.Buffer();
    // Read the data into a buffer.
    // TODO(user): swap to NIO buffers or zero-copy if/when okhttp/okio supports it
    buffer.write(frame.array(), frame.arrayOffset(), frame.remaining());
    // Write the data to the remote endpoint.
    // Per http2 SPEC, the max data length should be larger than 64K, while our frame size is
    // only 4K.
    Preconditions.checkState(buffer.size() < frameWriter.maxDataLength());
    outboundFlow.data(endOfStream, id(), buffer);
  }

  @Override
  protected void returnProcessedBytes(int processedBytes) {
    synchronized (executorLock) {
      processedWindow -= processedBytes;
      if (processedWindow <= WINDOW_UPDATE_THRESHOLD) {
        int delta = OkHttpClientTransport.DEFAULT_INITIAL_WINDOW_SIZE - processedWindow;
        window += delta;
        processedWindow += delta;
        frameWriter.windowUpdate(id(), delta);
      }
    }
  }

  @Override
  public boolean transportReportStatus(Status newStatus, Metadata.Trailers trailers) {
    synchronized (executorLock) {
      return super.transportReportStatus(newStatus, trailers);
    }
  }

  @Override
  protected void sendCancel() {
    if (transport.finishStream(id(), Status.CANCELLED)) {
      frameWriter.rstStream(id(), ErrorCode.CANCEL);
      transport.stopIfNecessary();
    }
  }

  @Override
  public void remoteEndClosed() {
    super.remoteEndClosed();
    if (transport.finishStream(id(), null)) {
      transport.stopIfNecessary();
    }
  }

  void setOutboundFlowState(Object outboundFlowState) {
    this.outboundFlowState = outboundFlowState;
  }

  Object getOutboundFlowState() {
    return outboundFlowState;
  }
}
