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

import static org.junit.Assert.assertEquals;

import io.netty.util.concurrent.DefaultThreadFactory;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.net.ServerSocket;
import java.net.Socket;
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
  public void smallLatency() throws Exception {
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
  public void bigLatency() throws Exception {
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
  public void smallBandwidth() throws Exception {
    server = new Server();
    int serverPort = server.init();
    server.setMode("stream");
    executor.execute(server);
    assertEquals("stream", server.mode());

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
  public void largeBandwidth() throws Exception {
    server = new Server();
    int serverPort = server.init();
    server.setMode("stream");
    executor.execute(server);
    assertEquals("stream", server.mode());
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
