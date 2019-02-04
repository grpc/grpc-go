/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.testing.integration;

import static com.google.common.base.Preconditions.checkArgument;

import io.netty.util.concurrent.DefaultThreadFactory;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.DelayQueue;
import java.util.concurrent.Delayed;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.logging.Logger;

public final class TrafficControlProxy {

  private static final int DEFAULT_BAND_BPS = 1024 * 1024;
  private static final int DEFAULT_DELAY_NANOS = 200 * 1000 * 1000;
  private static final Logger logger = Logger.getLogger(TrafficControlProxy.class.getName());

  // TODO: make host and ports arguments
  private final String localhost = "localhost";
  private final int serverPort;
  private final int queueLength;
  private final int chunkSize;
  private final int bandwidth;
  private final long latency;
  private volatile boolean shutDown;
  private ServerSocket clientAcceptor;
  private Socket serverSock;
  private Socket clientSock;
  private final ThreadPoolExecutor executor =
      new ThreadPoolExecutor(5, 10, 1, TimeUnit.SECONDS, new LinkedBlockingQueue<Runnable>(),
          new DefaultThreadFactory("proxy-pool", true));

  /**
   * Returns a new TrafficControlProxy with default bandwidth and latency.
   */
  public TrafficControlProxy(int serverPort) {
    this(serverPort, DEFAULT_BAND_BPS, DEFAULT_DELAY_NANOS, TimeUnit.NANOSECONDS);
  }

  /**
   * Returns a new TrafficControlProxy with bandwidth set to targetBPS, and latency set to
   * targetLatency in latencyUnits.
   */
  public TrafficControlProxy(int serverPort, int targetBps, int targetLatency,
      TimeUnit latencyUnits) {
    checkArgument(targetBps > 0);
    checkArgument(targetLatency > 0);
    this.serverPort = serverPort;
    bandwidth = targetBps;
    // divide by 2 because latency is applied in both directions
    latency = latencyUnits.toNanos(targetLatency) / 2;
    queueLength = (int) Math.max(bandwidth * latency / TimeUnit.SECONDS.toNanos(1), 1);
    chunkSize = Math.max(1, queueLength);
  }

  /**
   * Starts a new thread that waits for client and server and start reader/writer threads.
   */
  public void start() throws IOException {
    // ClientAcceptor uses a ServerSocket server so that the client can connect to the proxy as it
    // normally would a server. serverSock then connects the server using a regular Socket as a
    // client normally would.
    clientAcceptor = new ServerSocket();
    clientAcceptor.bind(new InetSocketAddress(localhost, 0));
    executor.execute(new Runnable() {
      @Override
      public void run() {
        try {
          clientSock = clientAcceptor.accept();
          serverSock = new Socket();
          serverSock.connect(new InetSocketAddress(localhost, serverPort));
          startWorkers();
        } catch (IOException e) {
          throw new RuntimeException(e);
        }
      }
    });
    logger.info("Started new proxy on port " + clientAcceptor.getLocalPort()
        + " with Queue Length " + queueLength);
  }

  public int getPort() {
    return clientAcceptor.getLocalPort();
  }

  /** Interrupt all workers and close sockets. */
  public void shutDown() throws IOException {
    // TODO: Handle case where a socket fails to close, therefore blocking the others from closing
    logger.info("Proxy shutting down... ");
    shutDown = true;
    executor.shutdown();
    clientAcceptor.close();
    clientSock.close();
    serverSock.close();
    logger.info("Shutdown Complete");
  }

  private void startWorkers() throws IOException {
    DataInputStream clientIn = new DataInputStream(clientSock.getInputStream());
    DataOutputStream clientOut = new DataOutputStream(serverSock.getOutputStream());
    DataInputStream serverIn = new DataInputStream(serverSock.getInputStream());
    DataOutputStream serverOut = new DataOutputStream(clientSock.getOutputStream());

    MessageQueue clientPipe = new MessageQueue(clientIn, clientOut);
    MessageQueue serverPipe = new MessageQueue(serverIn, serverOut);

    executor.execute(new Reader(clientPipe));
    executor.execute(new Writer(clientPipe));
    executor.execute(new Reader(serverPipe));
    executor.execute(new Writer(serverPipe));
  }

  private final class Reader implements Runnable {

    private final MessageQueue queue;

    Reader(MessageQueue queue) {
      this.queue = queue;
    }

    @Override
    public void run() {
      while (!shutDown) {
        try {
          queue.readIn();
        } catch (IOException e) {
          shutDown = true;
        } catch (InterruptedException e) {
          shutDown = true;
        }
      }
    }

  }

  private final class Writer implements Runnable {

    private final MessageQueue queue;

    Writer(MessageQueue queue) {
      this.queue = queue;
    }

    @Override
    public void run() {
      while (!shutDown) {
        try {
          queue.writeOut();
        } catch (IOException e) {
          shutDown = true;
        } catch (InterruptedException e) {
          shutDown = true;
        }
      }
    }
  }

  /**
   * A Delay Queue that counts by number of bytes instead of the number of elements.
   */
  private class MessageQueue {
    DataInputStream inStream;
    DataOutputStream outStream;
    int bytesQueued;
    BlockingQueue<Message> queue = new DelayQueue<>();

    MessageQueue(DataInputStream inputStream, DataOutputStream outputStream) {
      inStream = inputStream;
      outStream = outputStream;
    }

    /**
     * Take a message off the queue and write it to an endpoint. Blocks until a message becomes
     * available.
     */
    void writeOut() throws InterruptedException, IOException {
      Message next = queue.take();
      outStream.write(next.message, 0, next.messageLength);
      incrementBytes(-next.messageLength);
    }

    /**
     * Read bytes from an endpoint and add them as a message to the queue. Blocks if the queue is
     * full.
     */
    void readIn() throws InterruptedException, IOException {
      byte[] request = new byte[getNextChunk()];
      int readableBytes = inStream.read(request);
      long sendTime = System.nanoTime() + latency;
      queue.put(new Message(sendTime, request, readableBytes));
      incrementBytes(readableBytes);
    }

    /**
     * Block until space on the queue becomes available. Returns how many bytes can be read on to
     * the queue
     */
    synchronized int getNextChunk() throws InterruptedException {
      while (bytesQueued == queueLength) {
        wait();
      }
      return Math.max(0, Math.min(chunkSize, queueLength - bytesQueued));
    }

    synchronized void incrementBytes(int delta) {
      bytesQueued += delta;
      if (bytesQueued < queueLength) {
        notifyAll();
      }
    }
  }

  private static class Message implements Delayed {
    long sendTime;
    byte[] message;
    int messageLength;

    Message(long sendTime, byte[] message, int messageLength) {
      this.sendTime = sendTime;
      this.message = message;
      this.messageLength = messageLength;
    }

    @Override
    public int compareTo(Delayed o) {
      return ((Long) sendTime).compareTo(((Message) o).sendTime);
    }

    @Override
    public long getDelay(TimeUnit unit) {
      return unit.convert(sendTime - System.nanoTime(), TimeUnit.NANOSECONDS);
    }
  }
}
