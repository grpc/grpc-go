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

import com.google.common.io.ByteStreams;
import io.netty.util.concurrent.DefaultThreadFactory;
import java.io.DataInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.ServerSocket;
import java.net.Socket;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
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
  @org.junit.Ignore // flaky. latency commonly too high
  public void smallLatency() throws Exception {
    server = new Server();
    int serverPort = server.init();
    executor.execute(server);

    int latency = (int) TimeUnit.MILLISECONDS.toNanos(50);
    proxy = new TrafficControlProxy(serverPort, 1024 * 1024, latency, TimeUnit.NANOSECONDS);
    proxy.start();
    client = new Socket("localhost", proxy.getPort());
    client.setReuseAddress(true);
    OutputStream clientOut = client.getOutputStream();
    DataInputStream clientIn = new DataInputStream(client.getInputStream());
    byte[] message = new byte[1];

    // warmup
    for (int i = 0; i < 5; i++) {
      clientOut.write(message, 0, 1);
    }
    clientIn.readFully(new byte[5]);

    // test
    List<Long> rtts = new ArrayList<>();
    for (int i = 0; i < 3; i++) {
      long start = System.nanoTime();
      clientOut.write(message, 0, 1);
      clientIn.read(message);
      rtts.add(System.nanoTime() - start);
    }
    Collections.sort(rtts);
    long rtt = rtts.get(0);
    assertEquals(latency, rtt, .5 * latency);
  }

  @Test
  public void bigLatency() throws Exception {
    server = new Server();
    int serverPort = server.init();
    executor.execute(server);

    int latency = (int) TimeUnit.MILLISECONDS.toNanos(250);
    proxy = new TrafficControlProxy(serverPort, 1024 * 1024, latency, TimeUnit.NANOSECONDS);
    proxy.start();
    client = new Socket("localhost", proxy.getPort());
    OutputStream clientOut = client.getOutputStream();
    DataInputStream clientIn = new DataInputStream(client.getInputStream());
    byte[] message = new byte[1];

    // warmup
    for (int i = 0; i < 5; i++) {
      clientOut.write(message, 0, 1);
    }
    clientIn.readFully(new byte[5]);

    // test
    List<Long> rtts = new ArrayList<>();
    for (int i = 0; i < 2; i++) {
      long start = System.nanoTime();
      clientOut.write(message, 0, 1);
      clientIn.read(message);
      rtts.add(System.nanoTime() - start);
    }
    Collections.sort(rtts);
    long rtt = rtts.get(0);
    assertEquals(latency, rtt, .5 * latency);
  }

  @Test
  public void smallBandwidth() throws Exception {
    server = new Server();
    int serverPort = server.init();
    server.setMode("stream");
    executor.execute(server);

    int bandwidth = 64 * 1024;
    proxy = new TrafficControlProxy(serverPort, bandwidth, 200, TimeUnit.MILLISECONDS);
    proxy.start();
    client = new Socket("localhost", proxy.getPort());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());

    clientIn.readFully(new byte[100 * 1024]);
    int sample = bandwidth / 5;
    List<Double> bandwidths = new ArrayList<>();
    for (int i = 0; i < 5; i++) {
      long start = System.nanoTime();
      clientIn.readFully(new byte[sample]);
      long duration = System.nanoTime() - start;
      double actualBandwidth = sample / (((double) duration) / TimeUnit.SECONDS.toNanos(1));
      bandwidths.add(actualBandwidth);
    }
    Collections.sort(bandwidths);
    double bandUsed = bandwidths.get(bandwidths.size() - 1);
    assertEquals(bandwidth, bandUsed, .5 * bandwidth);
  }

  @Test
  public void largeBandwidth() throws Exception {
    server = new Server();
    int serverPort = server.init();
    server.setMode("stream");
    executor.execute(server);
    int bandwidth = 10 * 1024 * 1024;
    proxy = new TrafficControlProxy(serverPort, bandwidth, 200, TimeUnit.MILLISECONDS);
    proxy.start();
    client = new Socket("localhost", proxy.getPort());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());

    clientIn.readFully(new byte[100 * 1024]);
    int sample = bandwidth / 5;
    List<Double> bandwidths = new ArrayList<>();
    for (int i = 0; i < 5; i++) {
      long start = System.nanoTime();
      clientIn.readFully(new byte[sample]);
      long duration = System.nanoTime() - start;
      double actualBandwidth = sample / (((double) duration) / TimeUnit.SECONDS.toNanos(1));
      bandwidths.add(actualBandwidth);
    }
    Collections.sort(bandwidths);
    double bandUsed = bandwidths.get(bandwidths.size() - 1);
    assertEquals(bandwidth, bandUsed, .5 * bandwidth);
  }

  // server with echo and streaming modes
  private static class Server implements Runnable {
    private ServerSocket server;
    private String mode = "echo";

    public void setMode(String mode) {
      this.mode = mode;
    }

    /**
     * Initializes server and returns its listening port.
     */
    public int init() throws IOException {
      server = new ServerSocket(0);
      return server.getLocalPort();
    }

    public void shutDown() throws IOException {
      server.close();
    }

    @Override
    public void run() {
      try {
        Socket rcv = server.accept();
        try {
          handleSocket(rcv);
        } finally {
          rcv.close();
        }
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
    }

    private void handleSocket(Socket rcv) throws IOException {
      InputStream serverIn = rcv.getInputStream();
      OutputStream serverOut = rcv.getOutputStream();
      if (mode.equals("echo")) {
        ByteStreams.copy(serverIn, serverOut);
      } else if (mode.equals("stream")) {
        byte[] message = new byte[1024];
        while (true) {
          try {
            serverOut.write(message);
          } catch (IOException ignored) {
            // Client closed
            break;
          }
        }
      } else {
        throw new RuntimeException("Unknown mode: use 'echo' or 'stream'");
      }
    }
  }
}
