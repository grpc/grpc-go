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

import static java.lang.Math.atan2;
import static java.lang.Math.cos;
import static java.lang.Math.max;
import static java.lang.Math.min;
import static java.lang.Math.sin;
import static java.lang.Math.sqrt;
import static java.lang.Math.toRadians;
import static java.util.concurrent.TimeUnit.NANOSECONDS;

import io.grpc.ServerImpl;
import io.grpc.stub.StreamObserver;
import io.grpc.transport.netty.NettyServerBuilder;

import java.io.IOException;
import java.net.URL;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A sample gRPC server that serve the RouteGuide (see route_guide.proto) service.
 */
public class RouteGuideServer {
  private static final Logger logger = Logger.getLogger(RouteGuideServer.class.getName());

  private final int port;
  private final Collection<Feature> features;
  private ServerImpl gRpcServer;

  public RouteGuideServer(int port) {
    this(port, RouteGuideUtil.getDefaultFeaturesFile());
  }

  public RouteGuideServer(int port, URL featureFile) {
    try {
      this.port = port;
      features = RouteGuideUtil.parseFeatures(featureFile);
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }

  public void start() {
    gRpcServer = NettyServerBuilder.forPort(port)
        .addService(RouteGuideGrpc.bindService(new RouteGuideService(features)))
        .build().start();
    logger.info("Server started, listening on " + port);
    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        // Use stderr here since the logger may has been reset by its JVM shutdown hook.
        System.err.println("*** shutting down gRPC server since JVM is shutting down");
        RouteGuideServer.this.stop();
        System.err.println("*** server shut down");
      }
    });
  }

  public void stop() {
    if (gRpcServer != null) {
      gRpcServer.shutdown();
    }
  }

  public static void main(String[] args) throws Exception {
    RouteGuideServer server = new RouteGuideServer(8980);
    server.start();
  }

  /**
   * Our implementation of RouteGuide service.
   *
   * <p> See route_guide.proto for details of the methods.
   */
  private static class RouteGuideService implements RouteGuideGrpc.RouteGuide {
    private final Collection<Feature> features;
    private final ConcurrentMap<Point, List<RouteNote>> routeNotes =
        new ConcurrentHashMap<Point, List<RouteNote>>();

    RouteGuideService(Collection<Feature> features) {
      this.features = features;
    }

    /**
     * Gets the {@link Feature} at the requested {@link Point}. If no feature at that location
     * exists, an unnammed feature is returned at the provided location.
     *
     * @param request the requested location for the feature.
     * @param responseObserver the observer that will receive the feature at the requested point.
     */
    @Override
    public void getFeature(Point request, StreamObserver<Feature> responseObserver) {
      responseObserver.onValue(getFeature(request));
      responseObserver.onCompleted();
    }

    /**
     * Gets all features contained within the given bounding {@link Rectangle}.
     *
     * @param request the bounding rectangle for the requested features.
     * @param responseObserver the observer that will receive the features.
     */
    @Override
    public void listFeatures(Rectangle request, StreamObserver<Feature> responseObserver) {
      int left = min(request.getLo().getLongitude(), request.getHi().getLongitude());
      int right = max(request.getLo().getLongitude(), request.getHi().getLongitude());
      int top = max(request.getLo().getLatitude(), request.getHi().getLatitude());
      int bottom = min(request.getLo().getLatitude(), request.getHi().getLatitude());

      for (Feature feature : features) {
        if (!RouteGuideUtil.exists(feature)) {
          continue;
        }

        int lat = feature.getLocation().getLatitude();
        int lon = feature.getLocation().getLongitude();
        if (lon >= left && lon <= right && lat >= bottom && lat <= top) {
          responseObserver.onValue(feature);
        }
      }
      responseObserver.onCompleted();
    }

    /**
     * Gets a stream of points, and responds with statistics about the "trip": number of points,
     * number of known features visited, total distance traveled, and total time spent.
     *
     * @param responseObserver an observer to receive the response summary.
     * @return an observer to receive the requested route points.
     */
    @Override
    public StreamObserver<Point> recordRoute(final StreamObserver<RouteSummary> responseObserver) {
      return new StreamObserver<Point>() {
        int pointCount;
        int featureCount;
        int distance;
        Point previous;
        long startTime = System.nanoTime();

        @Override
        public void onValue(Point point) {
          pointCount++;
          if (RouteGuideUtil.exists(getFeature(point))) {
            featureCount++;
          }
          // For each point after the first, add the incremental distance from the previous point to
          // the total distance value.
          if (previous != null) {
            distance += calcDistance(previous, point);
          }
          previous = point;
        }

        @Override
        public void onError(Throwable t) {
          logger.log(Level.WARNING, "Encountered error in recordRoute", t);
        }

        @Override
        public void onCompleted() {
          long seconds = NANOSECONDS.toSeconds(System.nanoTime() - startTime);
          responseObserver.onValue(RouteSummary.newBuilder().setPointCount(pointCount)
              .setFeatureCount(featureCount).setDistance(distance)
              .setElapsedTime((int) seconds).build());
          responseObserver.onCompleted();
        }
      };
    }

    /**
     * Receives a stream of message/location pairs, and responds with a stream of all previous
     * messages at each of those locations.
     *
     * @param responseObserver an observer to receive the stream of previous messages.
     * @return an observer to handle requested message/location pairs.
     */
    @Override
    public StreamObserver<RouteNote> routeChat(final StreamObserver<RouteNote> responseObserver) {
      return new StreamObserver<RouteNote>() {
        @Override
        public void onValue(RouteNote note) {
          List<RouteNote> notes = getOrCreateNotes(note.getLocation());

          // Respond with all previous notes at this location.
          for (RouteNote prevNote : notes.toArray(new RouteNote[0])) {
            responseObserver.onValue(prevNote);
          }

          // Now add the new note to the list
          notes.add(note);
        }

        @Override
        public void onError(Throwable t) {
          logger.log(Level.WARNING, "Encountered error in routeChat", t);
        }

        @Override
        public void onCompleted() {
          responseObserver.onCompleted();
        }
      };
    }

    /**
     * Get the notes list for the given location. If missing, create it.
     */
    private List<RouteNote> getOrCreateNotes(Point location) {
      List<RouteNote> notes = Collections.synchronizedList(new ArrayList<RouteNote>());
      List<RouteNote> prevNotes = routeNotes.putIfAbsent(location, notes);
      return prevNotes != null ? prevNotes : notes;
    }

    /**
     * Gets the feature at the given point.
     *
     * @param location the location to check.
     * @return The feature object at the point. Note that an empty name indicates no feature.
     */
    private Feature getFeature(Point location) {
      for (Feature feature : features) {
        if (feature.getLocation().getLatitude() == location.getLatitude()
            && feature.getLocation().getLongitude() == location.getLongitude()) {
          return feature;
        }
      }

      // No feature was found, return an unnamed feature.
      return Feature.newBuilder().setName("").setLocation(location).build();
    }

    /**
     * Calculate the distance between two points using the "haversine" formula.
     * This code was taken from http://www.movable-type.co.uk/scripts/latlong.html.
     *
     * @param start The starting point
     * @param end The end point
     * @return The distance between the points in meters
     */
    private static double calcDistance(Point start, Point end) {
      double lat1 = RouteGuideUtil.getLatitude(start);
      double lat2 = RouteGuideUtil.getLatitude(end);
      double lon1 = RouteGuideUtil.getLongitude(start);
      double lon2 = RouteGuideUtil.getLongitude(end);
      int R = 6371000; // metres
      double φ1 = toRadians(lat1);
      double φ2 = toRadians(lat2);
      double Δφ = toRadians(lat2-lat1);
      double Δλ = toRadians(lon2-lon1);

      double a = sin(Δφ/2) * sin(Δφ/2) + cos(φ1) * cos(φ2) * sin(Δλ/2) * sin(Δλ/2);
      double c = 2 * atan2(sqrt(a), sqrt(1-a));

      return R * c;
    }
  }
}
