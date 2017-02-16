/*
 * Copyright 2016, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
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
    BlockingQueue<Message> queue = new DelayQueue<Message>();

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
