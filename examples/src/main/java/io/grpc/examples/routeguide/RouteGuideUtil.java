/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

package io.grpc.examples.routeguide;

import com.google.protobuf.util.JsonFormat;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.Reader;
import java.net.URL;
import java.nio.charset.Charset;
import java.util.List;

/**
 * Common utilities for the RouteGuide demo.
 */
public class RouteGuideUtil {
  private static final double COORD_FACTOR = 1e7;

  /**
   * Gets the latitude for the given point.
   */
  public static double getLatitude(Point location) {
    return location.getLatitude() / COORD_FACTOR;
  }

  /**
   * Gets the longitude for the given point.
   */
  public static double getLongitude(Point location) {
    return location.getLongitude() / COORD_FACTOR;
  }

  /**
   * Gets the default features file from classpath.
   */
  public static URL getDefaultFeaturesFile() {
    return RouteGuideServer.class.getResource("route_guide_db.json");
  }

  /**
   * Parses the JSON input file containing the list of features.
   */
  public static List<Feature> parseFeatures(URL file) throws IOException {
    InputStream input = file.openStream();
    try {
      Reader reader = new InputStreamReader(input, Charset.forName("UTF-8"));
      try {
        FeatureDatabase.Builder database = FeatureDatabase.newBuilder();
        JsonFormat.parser().merge(reader, database);
        return database.getFeatureList();
      } finally {
        reader.close();
      }
    } finally {
      input.close();
    }
  }

  /**
   * Indicates whether the given feature exists (i.e. has a valid name).
   */
  public static boolean exists(Feature feature) {
    return feature != null && !feature.getName().isEmpty();
  }
}
