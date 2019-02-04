/*
 * Copyright 2017 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.Decompressor;
import java.io.InputStream;
import java.util.ArrayDeque;
import java.util.Queue;
import javax.annotation.Nullable;

/**
 * Sits between {@link AbstractStream.TransportState} and {@link MessageDeframer} to deframe in the
 * client thread. Calls from the transport to the deframer are wrapped into an {@link
 * InitializingMessageProducer} and given back to the transport, where they will be run on the
 * client thread. Calls from the deframer back to the transport use {@link
 * TransportExecutor#runOnTransportThread} to run on the transport thread.
 */
public class ApplicationThreadDeframer implements Deframer, MessageDeframer.Listener {
  interface TransportExecutor {
    void runOnTransportThread(Runnable r);
  }

  private final MessageDeframer.Listener storedListener;
  private final MessageDeframer deframer;
  private final TransportExecutor transportExecutor;

  /** Queue for messages returned by the deframer when deframing in the application thread. */
  private final Queue<InputStream> messageReadQueue = new ArrayDeque<>();

  ApplicationThreadDeframer(
      MessageDeframer.Listener listener,
      TransportExecutor transportExecutor,
      MessageDeframer deframer) {
    this.storedListener = checkNotNull(listener, "listener");
    this.transportExecutor = checkNotNull(transportExecutor, "transportExecutor");
    deframer.setListener(this);
    this.deframer = deframer;
  }

  @Override
  public void setMaxInboundMessageSize(int messageSize) {
    deframer.setMaxInboundMessageSize(messageSize);
  }

  @Override
  public void setDecompressor(Decompressor decompressor) {
    deframer.setDecompressor(decompressor);
  }

  @Override
  public void setFullStreamDecompressor(GzipInflatingBuffer fullStreamDecompressor) {
    deframer.setFullStreamDecompressor(fullStreamDecompressor);
  }

  @Override
  public void request(final int numMessages) {
    storedListener.messagesAvailable(
        new InitializingMessageProducer(
            new Runnable() {
              @Override
              public void run() {
                if (deframer.isClosed()) {
                  return;
                }
                try {
                  deframer.request(numMessages);
                } catch (Throwable t) {
                  storedListener.deframeFailed(t);
                  deframer.close(); // unrecoverable state
                }
              }
            }));
  }

  @Override
  public void deframe(final ReadableBuffer data) {
    storedListener.messagesAvailable(
        new InitializingMessageProducer(
            new Runnable() {
              @Override
              public void run() {
                try {
                  deframer.deframe(data);
                } catch (Throwable t) {
                  deframeFailed(t);
                  deframer.close(); // unrecoverable state
                }
              }
            }));
  }

  @Override
  public void closeWhenComplete() {
    storedListener.messagesAvailable(
        new InitializingMessageProducer(
            new Runnable() {
              @Override
              public void run() {
                deframer.closeWhenComplete();
              }
            }));
  }

  @Override
  public void close() {
    deframer.stopDelivery();
    storedListener.messagesAvailable(
        new InitializingMessageProducer(
            new Runnable() {
              @Override
              public void run() {
                deframer.close();
              }
            }));
  }

  @Override
  public void bytesRead(final int numBytes) {
    transportExecutor.runOnTransportThread(
        new Runnable() {
          @Override
          public void run() {
            storedListener.bytesRead(numBytes);
          }
        });
  }

  @Override
  public void messagesAvailable(StreamListener.MessageProducer producer) {
    InputStream message;
    while ((message = producer.next()) != null) {
      messageReadQueue.add(message);
    }
  }

  @Override
  public void deframerClosed(final boolean hasPartialMessage) {
    transportExecutor.runOnTransportThread(
        new Runnable() {
          @Override
          public void run() {
            storedListener.deframerClosed(hasPartialMessage);
          }
        });
  }

  @Override
  public void deframeFailed(final Throwable cause) {
    transportExecutor.runOnTransportThread(
        new Runnable() {
          @Override
          public void run() {
            storedListener.deframeFailed(cause);
          }
        });
  }

  private class InitializingMessageProducer implements StreamListener.MessageProducer {
    private final Runnable runnable;
    private boolean initialized = false;

    private InitializingMessageProducer(Runnable runnable) {
      this.runnable = runnable;
    }

    private void initialize() {
      if (!initialized) {
        runnable.run();
        initialized = true;
      }
    }

    @Nullable
    @Override
    public InputStream next() {
      initialize();
      return messageReadQueue.poll();
    }
  }
}
