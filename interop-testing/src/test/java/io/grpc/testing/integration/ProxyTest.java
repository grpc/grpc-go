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

import static org.junit.Assert.assertEquals;

import io.netty.util.concurrent.DefaultThreadFactory;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.UnknownHostException;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Future;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.AfterClass;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class ProxyTest {

  private static ThreadPoolExecutor executor =
      new ThreadPoolExecutor(8, 8, 1, TimeUnit.SECONDS, new LinkedBlockingQueue<Runnable>(),
          new DefaultThreadFactory("proxy-test-pool", true));

  private TrafficControlProxy proxy;
  private Socket client;
  private Server server;

  @AfterClass
  public static void stopExecutor() {
    executor.shutdown();
  }

  @After
  public void shutdownTest() throws IOException {
    proxy.shutDown();
    server.shutDown();
    client.close();
  }

  @Test
  public void smallLatency()
      throws UnknownHostException, IOException, InterruptedException, ExecutionException {
    server = new Server();
    int serverPort = server.init();
    executor.execute(server);

    int latency = (int) TimeUnit.MILLISECONDS.toNanos(50);
    proxy = new TrafficControlProxy(serverPort, 1024 * 1024, latency, TimeUnit.NANOSECONDS);
    startProxy(proxy).get();
    client = new Socket("localhost", proxy.getPort());
    client.setReuseAddress(true);
    DataOutputStream clientOut = new DataOutputStream(client.getOutputStream());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());
    byte[] message = new byte[1];

    // warmup
    for (int i = 0; i < 5; i++) {
      clientOut.write(message, 0, 1);
    }
    clientIn.readFully(new byte[5]);

    // test
    long start = System.nanoTime();
    clientOut.write(message, 0, 1);
    clientIn.read(message);
    long stop = System.nanoTime();

    long rtt = (stop - start);
    assertEquals(latency, rtt, latency);
  }

  @Test
  public void bigLatency()
      throws UnknownHostException, IOException, InterruptedException, ExecutionException {
    server = new Server();
    int serverPort = server.init();
    executor.execute(server);

    int latency = (int) TimeUnit.MILLISECONDS.toNanos(250);
    proxy = new TrafficControlProxy(serverPort, 1024 * 1024, latency, TimeUnit.NANOSECONDS);
    startProxy(proxy).get();
    client = new Socket("localhost", proxy.getPort());
    DataOutputStream clientOut = new DataOutputStream(client.getOutputStream());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());
    byte[] message = new byte[1];

    // warmup
    for (int i = 0; i < 5; i++) {
      clientOut.write(message, 0, 1);
    }
    clientIn.readFully(new byte[5]);

    // test
    long start = System.nanoTime();
    clientOut.write(message, 0, 1);
    clientIn.read(message);
    long stop = System.nanoTime();

    long rtt = (stop - start);
    assertEquals(latency, rtt, latency);
  }

  @Test
  public void smallBandwidth()
      throws UnknownHostException, IOException, InterruptedException, ExecutionException {
    server = new Server();
    int serverPort = server.init();
    server.setMode("stream");
    executor.execute(server);
    assertEquals(server.mode(), "stream");

    int bandwidth = 64 * 1024;
    proxy = new TrafficControlProxy(serverPort, bandwidth, 200, TimeUnit.MILLISECONDS);
    startProxy(proxy).get();
    client = new Socket("localhost", proxy.getPort());
    DataOutputStream clientOut = new DataOutputStream(client.getOutputStream());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());

    clientOut.write(new byte[1]);
    clientIn.readFully(new byte[100 * 1024]);
    long start = System.nanoTime();
    clientIn.readFully(new byte[5 * bandwidth]);
    long stop = System.nanoTime();

    long bandUsed = ((5 * bandwidth) / ((stop - start) / TimeUnit.SECONDS.toNanos(1)));
    assertEquals(bandwidth, bandUsed, .5 * bandwidth);
  }

  @Test
  public void largeBandwidth()
      throws UnknownHostException, IOException, InterruptedException, ExecutionException {
    server = new Server();
    int serverPort = server.init();
    server.setMode("stream");
    executor.execute(server);
    assertEquals(server.mode(), "stream");
    int bandwidth = 10 * 1024 * 1024;
    proxy = new TrafficControlProxy(serverPort, bandwidth, 200, TimeUnit.MILLISECONDS);
    startProxy(proxy).get();
    client = new Socket("localhost", proxy.getPort());
    DataOutputStream clientOut = new DataOutputStream(client.getOutputStream());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());

    clientOut.write(new byte[1]);
    clientIn.readFully(new byte[100 * 1024]);
    long start = System.nanoTime();
    clientIn.readFully(new byte[5 * bandwidth]);
    long stop = System.nanoTime();

    long bandUsed = ((5 * bandwidth) / ((stop - start) / TimeUnit.SECONDS.toNanos(1)));
    assertEquals(bandwidth, bandUsed, .5 * bandwidth);
  }

  private Future<?> startProxy(final TrafficControlProxy p) {
    return executor.submit(new Runnable() {
      @Override
      public void run() {
        try {
          p.start();
        } catch (Exception e) {
          throw new RuntimeException(e);
        }
      }
    });
  }

  // server with echo and streaming modes
  private static class Server implements Runnable {
    private ServerSocket server;
    private Socket rcv;
    private boolean shutDown;
    private String mode = "echo";

    public void setMode(String mode) {
      this.mode = mode;
    }

    public String mode() {
      return mode;
    }

    /**
     * Initializes server and returns its listening port.
     */
    public int init() throws IOException {
      server = new ServerSocket(0);
      return server.getLocalPort();
    }

    public void shutDown() {
      try {
        server.close();
        rcv.close();
        shutDown = true;
      } catch (IOException e) {
        shutDown = true;
      }
    }

    @Override
    public void run() {
      try {
        rcv = server.accept();
        DataInputStream serverIn = new DataInputStream(rcv.getInputStream());
        DataOutputStream serverOut = new DataOutputStream(rcv.getOutputStream());
        byte[] response = new byte[1024];
        if (mode.equals("echo")) {
          while (!shutDown) {
            int readable = serverIn.read(response);
            serverOut.write(response, 0, readable);
          }
        } else if (mode.equals("stream")) {
          serverIn.read(response);
          byte[] message = new byte[16 * 1024];
          while (!shutDown) {
            serverOut.write(message, 0, message.length);
          }
          serverIn.close();
          serverOut.close();
          rcv.close();
        } else {
          System.out.println("Unknown mode: use 'echo' or 'stream'");
        }
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
    }
  }
}
