/*
 * Copyright 2015, Google Inc. All rights reserved.
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

package io.grpc.examples.routeguide;

import com.google.common.util.concurrent.SettableFuture;

import io.grpc.ChannelImpl;
import io.grpc.examples.routeguide.RouteGuideGrpc.RouteGuideBlockingStub;
import io.grpc.examples.routeguide.RouteGuideGrpc.RouteGuideStub;
import io.grpc.stub.StreamObserver;
import io.grpc.transport.netty.NegotiationType;
import io.grpc.transport.netty.NettyChannelBuilder;

import java.util.Iterator;
import java.util.List;
import java.util.Random;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Sample client code that makes gRPC calls to the server.
 */
public class RouteGuideClient {
  private static final Logger logger = Logger.getLogger(RouteGuideClient.class.getName());

  private final ChannelImpl channel;
  private final RouteGuideBlockingStub blockingStub;
  private final RouteGuideStub asyncStub;

  public RouteGuideClient(String host, int port) {
    channel = NettyChannelBuilder.forAddress(host, port)
        .negotiationType(NegotiationType.PLAINTEXT)
        .build();
    blockingStub = RouteGuideGrpc.newBlockingStub(channel);
    asyncStub = RouteGuideGrpc.newStub(channel);
  }

  public void shutdown() throws InterruptedException {
    channel.shutdown().awaitTerminated(5, TimeUnit.SECONDS);
  }

  /**
   * This example shows how to make a blocking unary call.
   */
  public void getFeature(int lat, int lon) {
    try {
      info("*** GetFeature: lat={0} lon={1}", lat, lon);

      Point request = Point.newBuilder().setLatitude(lat).setLongitude(lon).build();
      Feature feature = blockingStub.getFeature(request);
      if (RouteGuideUtil.exists(feature)) {
        info("Found feature called \"{0}\" at {1}, {2}",
            feature.getName(),
            RouteGuideUtil.getLatitude(feature.getLocation()),
            RouteGuideUtil.getLongitude(feature.getLocation()));
      } else {
        info("Found no feature at {0}, {1}",
            RouteGuideUtil.getLatitude(feature.getLocation()),
            RouteGuideUtil.getLongitude(feature.getLocation()));
      }
    } catch (RuntimeException e) {
      logger.log(Level.WARNING, "RPC failed", e);
      throw e;
    }
  }

  /**
   * This example shows how to make a blocking unary call.
   */
  public void listFeatures(int lowLat, int lowLon, int hiLat, int hiLon) {
    try {
      info("*** ListFeatures: lowLat={0} lowLon={1} hiLat={2} hiLon={3}", lowLat, lowLon, hiLat,
          hiLon);

      Rectangle request =
          Rectangle.newBuilder()
              .setLo(Point.newBuilder().setLatitude(lowLat).setLongitude(lowLon).build())
              .setHi(Point.newBuilder().setLatitude(hiLat).setLongitude(hiLon).build()).build();
      Iterator<Feature> features = blockingStub.listFeatures(request);

      StringBuilder responseLog = new StringBuilder("Result: ");
      while (features.hasNext()) {
        Feature feature = features.next();
        responseLog.append(feature);
      }
      info(responseLog.toString());
    } catch (RuntimeException e) {
      logger.log(Level.WARNING, "RPC failed", e);
      throw e;
    }
  }

  public void recordRoute(List<Feature> features, int numPoints) throws Exception {
    info("*** RecordRoute");
    final SettableFuture<Void> finishFuture = SettableFuture.create();
    StreamObserver<RouteSummary> responseObserver = new StreamObserver<RouteSummary>() {
      @Override
      public void onValue(RouteSummary summary) {
        info("Finished trip with {0} points. Passed {1} features. "
            + "Travelled {2} meters. It took {3} seconds.", summary.getPointCount(),
            summary.getFeatureCount(), summary.getDistance(), summary.getElapsedTime());
      }

      @Override
      public void onError(Throwable t) {
        finishFuture.setException(t);
      }

      @Override
      public void onCompleted() {
        finishFuture.set(null);
      }
    };

    StreamObserver<Point> requestObserver = asyncStub.recordRoute(responseObserver);
    try {
      // Send numPoints points randomly selected from the features list.
      StringBuilder numMsg = new StringBuilder();
      Random rand = new Random();
      for (int i = 0; i < numPoints; ++i) {
        int index = rand.nextInt(features.size());
        Point point = features.get(index).getLocation();
        info("Visiting point {0}, {1}", RouteGuideUtil.getLatitude(point),
            RouteGuideUtil.getLongitude(point));
        requestObserver.onValue(point);
        // Sleep for a bit before sending the next one.
        Thread.sleep(rand.nextInt(1000) + 500);
        if (finishFuture.isDone()) {
          break;
        }
      }
      info(numMsg.toString());
      requestObserver.onCompleted();

      finishFuture.get();
      info("Finished RecordRoute");
    } catch (Exception e) {
      requestObserver.onError(e);
      logger.log(Level.WARNING, "RecordRoute Failed", e);
      throw e;
    }
  }

  /**
   * This example shows how to make a bi-directional streaming call, which can only be asynchronous.
   */
  public void routeChat() throws Exception {
    info("*** RoutChat");
    final SettableFuture<Void> finishFuture = SettableFuture.create();
    StreamObserver<RouteNote> requestObserver =
        asyncStub.routeChat(new StreamObserver<RouteNote>() {
          @Override
          public void onValue(RouteNote note) {
            info("Got message \"{0}\" at {1}, {2}", note.getMessage(), note.getLocation()
                .getLatitude(), note.getLocation().getLongitude());
          }

          @Override
          public void onError(Throwable t) {
            finishFuture.setException(t);
          }

          @Override
          public void onCompleted() {
            finishFuture.set(null);
          }
        });

    try {
      RouteNote[] requests =
          {newNote("First message", 0, 0), newNote("Second message", 0, 1),
              newNote("Third message", 1, 0), newNote("Fourth message", 1, 1)};

      for (RouteNote request : requests) {
        info("Sending message \"{0}\" at {1}, {2}", request.getMessage(), request.getLocation()
            .getLatitude(), request.getLocation().getLongitude());
        requestObserver.onValue(request);
      }
      requestObserver.onCompleted();

      finishFuture.get();
      info("Finished RouteChat");
    } catch (Exception t) {
      requestObserver.onError(t);
      logger.log(Level.WARNING, "RouteChat Failed", t);
      throw t;
    }
  }

  public static void main(String[] args) throws Exception {
    RouteGuideClient client = new RouteGuideClient("localhost", 8980);
    try {
      // Looking for a valid feature
      client.getFeature(409146138, -746188906);

      // Feature missing.
      client.getFeature(0, 0);

      // Looking for features between 40, -75 and 42, -73.
      client.listFeatures(400000000, -750000000, 420000000, -730000000);

      // Record a few randomly selected points from the features file.
      client.recordRoute(RouteGuideUtil.parseFeatures(RouteGuideUtil.getDefaultFeaturesFile()), 10);

      // Send and receive some notes.
      client.routeChat();
    } finally {
      client.shutdown();
    }
  }

  private static void info(String msg, Object... params) {
    logger.log(Level.INFO, msg, params);
  }

  private RouteNote newNote(String message, int lat, int lon) {
    return RouteNote.newBuilder().setMessage(message)
        .setLocation(Point.newBuilder().setLatitude(lat).setLongitude(lon).build()).build();
  }
}
