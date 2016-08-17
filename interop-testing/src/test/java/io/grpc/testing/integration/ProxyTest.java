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

import org.junit.AfterClass;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

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

@RunWith(JUnit4.class)
public class ProxyTest {

  private int serverPort = 5001;
  private int proxyPort = 5050;
  private String loopBack = "127.0.0.1";
  private static ThreadPoolExecutor executor =
      new ThreadPoolExecutor(1, 4, 1, TimeUnit.SECONDS, new LinkedBlockingQueue<Runnable>());

  @AfterClass
  public static void stopExecutor() {
    executor.shutdown();
  }

  @Test
  public void smallLatency()
      throws UnknownHostException, IOException, InterruptedException, ExecutionException {
    Server server = new Server();
    Thread serverThread = new Thread(server);
    serverThread.start();

    int latency = (int) TimeUnit.MILLISECONDS.toNanos(10);
    TrafficControlProxy p = new TrafficControlProxy(1024 * 1024, latency, TimeUnit.NANOSECONDS);
    startProxy(p).get();
    Socket client = new Socket(loopBack, proxyPort);
    client.setReuseAddress(true);
    DataOutputStream clientOut = new DataOutputStream(client.getOutputStream());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());
    byte[] message = new byte[1];

    // test
    long start = System.nanoTime();
    clientOut.write(message, 0, 1);
    clientIn.read(message);
    long stop = System.nanoTime();

    p.shutDown();
    server.shutDown();
    client.close();

    long rtt = (stop - start);
    assertEquals(latency, rtt, latency);
  }

  @Test
  public void bigLatency()
      throws UnknownHostException, IOException, InterruptedException, ExecutionException {
    Server server = new Server();
    Thread serverThread = new Thread(server);
    serverThread.start();

    int latency = (int) TimeUnit.MILLISECONDS.toNanos(250);
    TrafficControlProxy p = new TrafficControlProxy(1024 * 1024, latency, TimeUnit.NANOSECONDS);
    startProxy(p).get();
    Socket client = new Socket(loopBack, proxyPort);
    DataOutputStream clientOut = new DataOutputStream(client.getOutputStream());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());
    byte[] message = new byte[1];

    // test
    long start = System.nanoTime();
    clientOut.write(message, 0, 1);
    clientIn.read(message);
    long stop = System.nanoTime();

    p.shutDown();
    server.shutDown();
    client.close();

    long rtt = (stop - start);
    assertEquals(latency, rtt, latency);
  }

  @Test
  public void smallBandwidth()
      throws UnknownHostException, IOException, InterruptedException, ExecutionException {
    Server server = new Server();
    server.setMode("stream");
    (new Thread(server)).start();
    assertEquals(server.mode(), "stream");

    int bandwidth = 64 * 1024;
    TrafficControlProxy p = new TrafficControlProxy(bandwidth, 200, TimeUnit.MILLISECONDS);
    startProxy(p).get();
    Socket client = new Socket(loopBack, proxyPort);
    DataOutputStream clientOut = new DataOutputStream(client.getOutputStream());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());

    clientOut.write(new byte[1]);
    clientIn.readFully(new byte[100 * 1024]);
    long start = System.nanoTime();
    clientIn.readFully(new byte[5 * bandwidth]);
    long stop = System.nanoTime();

    p.shutDown();
    server.shutDown();
    client.close();

    long bandUsed = ((5 * bandwidth) / ((stop - start) / TimeUnit.SECONDS.toNanos(1)));
    assertEquals(bandwidth, bandUsed, .5 * bandwidth);
  }

  @Test
  public void largeBandwidth()
      throws UnknownHostException, IOException, InterruptedException, ExecutionException {
    Server server = new Server();
    server.setMode("stream");
    (new Thread(server)).start();
    assertEquals(server.mode(), "stream");
    int bandwidth = 10 * 1024 * 1024;
    TrafficControlProxy p = new TrafficControlProxy(bandwidth, 200, TimeUnit.MILLISECONDS);
    startProxy(p).get();
    Socket client = new Socket(loopBack, proxyPort);
    DataOutputStream clientOut = new DataOutputStream(client.getOutputStream());
    DataInputStream clientIn = new DataInputStream(client.getInputStream());

    clientOut.write(new byte[1]);
    clientIn.readFully(new byte[100 * 1024]);
    long start = System.nanoTime();
    clientIn.readFully(new byte[5 * bandwidth]);
    long stop = System.nanoTime();

    p.shutDown();
    server.shutDown();
    client.close();

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
  private class Server implements Runnable {
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

    public void shutDown() {
      try {
        rcv.close();
        server.close();
        shutDown = true;
      } catch (IOException e) {
        shutDown = true;
      }
    }

    @Override
    public void run() {
      try {
        server = new ServerSocket(serverPort);
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
        e.printStackTrace();
      }
    }
  }
}
