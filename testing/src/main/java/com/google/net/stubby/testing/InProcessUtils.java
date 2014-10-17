package com.google.net.stubby.testing;

import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.HandlerRegistry;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.SerializingExecutor;
import com.google.net.stubby.ServerCall;
import com.google.net.stubby.ServerMethodDefinition;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientStreamListener;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.ClientTransportFactory;
import com.google.net.stubby.newtransport.StreamState;

import java.io.IOException;
import java.io.InputStream;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.atomic.AtomicBoolean;

import javax.annotation.Nullable;

/**
 * Utility functions for binding clients to in-process services.
 */
public class InProcessUtils {

  /**
   * Create a {@link ClientTransportFactory} connected to the given
   * {@link com.google.net.stubby.HandlerRegistry}
   */
  public static ClientTransportFactory adaptHandlerRegistry(HandlerRegistry handlers,
                                                            ExecutorService executor) {
    final ClientTransport transport = new InProcessClientTransport(handlers, executor);
    return new ClientTransportFactory() {
      @Override
      public ClientTransport newClientTransport() {
        return transport;
      }
    };
  }

  private InProcessUtils() {
  }

  /**
   * Implementation of ClientTransport that delegates to a
   * {@link com.google.net.stubby.ServerCall.Listener}
   */
  private static class InProcessClientTransport extends AbstractService
      implements ClientTransport {
    private final HandlerRegistry handlers;

    private final ExecutorService executor;

    public InProcessClientTransport(HandlerRegistry handlers, ExecutorService executor) {
      this.handlers = handlers;
      this.executor = executor;
    }

    @Override
    protected void doStart() {
      notifyStarted();
    }

    @Override
    public void doStop() {
      notifyStopped();
    }

    @Override
    public ClientStream newStream(MethodDescriptor<?, ?> method,
                                  final Metadata.Headers headers,
                                  final ClientStreamListener clientListener) {
      // Separate FIFO executor queues for work on the client and server
      final SerializingExecutor serverWorkQueue = new SerializingExecutor(executor);
      final SerializingExecutor clientWorkQueue = new SerializingExecutor(executor);

      final HandlerRegistry.Method resolvedMethod = handlers.lookupMethod("/" + method.getName());
      if (resolvedMethod == null) {
        // Threading?
        clientWorkQueue.execute(new Runnable() {
          @Override
          public void run() {
            clientListener.closed(Status.UNIMPLEMENTED, new Metadata.Trailers());
          }
        });
        return new NoOpClientStream();
      }

      final ServerMethodDefinition serverMethod = resolvedMethod.getMethodDefinition();
      final AtomicBoolean cancelled = new AtomicBoolean();

      // Implementation of ServerCall which delegates to the client listener.
      final ServerCall serverCall = new ServerCall() {

        @Override
        public void sendHeaders(final Metadata.Headers headers) {
          clientWorkQueue.execute(new Runnable() {
            @Override
            public void run() {
              clientListener.headersRead(headers);
            }
          });
        }

        @Override
        public void sendPayload(final Object payload) {
          clientWorkQueue.execute(new Runnable() {
            @Override
            public void run() {
              try {
                // TODO(user): Consider adapting at the Channel layer on the client
                // so we avoid serialization costs.
                InputStream message = serverMethod.streamResponse(payload);
                clientListener.messageRead(message, message.available());
              } catch (IOException ioe) {
                close(Status.fromThrowable(ioe), new Metadata.Trailers());
              }
            }
          });
        }

        @Override
        public void close(final Status status, final Metadata.Trailers trailers) {
          clientWorkQueue.execute(new Runnable() {
            @Override
            public void run() {
              clientListener.closed(status, trailers);
            }
          });

        }

        @Override
        public boolean isCancelled() {
          return cancelled.get();
        }
      };

      // Get the listener from the service implementation
      final ServerCall.Listener serverListener =
          serverMethod.getServerCallHandler().startCall(method.getName(),
              serverCall, headers);

      // Return implementation of ClientStream which delegates to the server listener.
      return new ClientStream() {

        StreamState state = StreamState.OPEN;

        @Override
        public void cancel() {
          cancelled.set(true);
          state = StreamState.CLOSED;
          serverWorkQueue.execute(new Runnable() {
            @Override
            public void run() {
              serverListener.onCancel();
            }
          });
        }

        @Override
        public void halfClose() {
          state = StreamState.WRITE_ONLY;
          serverWorkQueue.execute(new Runnable() {
            @Override
            public void run() {
              serverListener.onHalfClose();
            }
          });
        }

        @Override
        public StreamState state() {
          return state;
        }

        @Override
        public void writeMessage(final InputStream message, int length,
                                 @Nullable final Runnable accepted) {
          serverWorkQueue.execute(new Runnable() {
            @Override
            public void run() {
              try {
                serverListener.onPayload(serverMethod.parseRequest(message));
              } catch (RuntimeException re) {
                serverCall.close(Status.fromThrowable(re), new Metadata.Trailers());
              } finally {
                if (accepted != null) {
                  accepted.run();
                }
              }
            }
          });
        }

        @Override
        public void flush() {
          // No-op
        }
      };
    }

    // Simple No-Op implementation of ClientStream
    private static class NoOpClientStream implements ClientStream {
      @Override
      public void cancel() {
        // No-op
      }

      @Override
      public void halfClose() {
        // No-op
      }

      @Override
      public StreamState state() {
        return StreamState.CLOSED;
      }

      @Override
      public void writeMessage(InputStream message, int length, @Nullable Runnable accepted) {
      }

      @Override
      public void flush() {
      }
    }
  }
}
