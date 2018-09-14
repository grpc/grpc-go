/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc.okhttp;

import static io.grpc.internal.GrpcUtil.CONTENT_TYPE_KEY;
import static io.grpc.internal.GrpcUtil.USER_AGENT_KEY;

import com.google.common.base.Preconditions;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.TransportFrameUtil;
import io.grpc.okhttp.internal.framed.Header;
import java.util.ArrayList;
import java.util.List;
import okio.ByteString;

/**
 * Constants for request/response headers.
 */
class Headers {

  public static final Header SCHEME_HEADER = new Header(Header.TARGET_SCHEME, "https");
  public static final Header METHOD_HEADER = new Header(Header.TARGET_METHOD, GrpcUtil.HTTP_METHOD);
  public static final Header METHOD_GET_HEADER = new Header(Header.TARGET_METHOD, "GET");
  public static final Header CONTENT_TYPE_HEADER =
      new Header(CONTENT_TYPE_KEY.name(), GrpcUtil.CONTENT_TYPE_GRPC);
  public static final Header TE_HEADER = new Header("te", GrpcUtil.TE_TRAILERS);

  /**
   * Serializes the given headers and creates a list of OkHttp {@link Header}s to be used when
   * creating a stream. Since this serializes the headers, this method should be called in the
   * application thread context.
   */
  public static List<Header> createRequestHeaders(
      Metadata headers, String defaultPath, String authority, String userAgent, boolean useGet) {
    Preconditions.checkNotNull(headers, "headers");
    Preconditions.checkNotNull(defaultPath, "defaultPath");
    Preconditions.checkNotNull(authority, "authority");

    // Discard any application supplied duplicates of the reserved headers
    headers.discardAll(GrpcUtil.CONTENT_TYPE_KEY);
    headers.discardAll(GrpcUtil.TE_HEADER);
    headers.discardAll(GrpcUtil.USER_AGENT_KEY);

    // 7 is the number of explicit add calls below.
    List<Header> okhttpHeaders = new ArrayList<>(7 + InternalMetadata.headerCount(headers));

    // Set GRPC-specific headers.
    okhttpHeaders.add(SCHEME_HEADER);
    if (useGet) {
      okhttpHeaders.add(METHOD_GET_HEADER);
    } else {
      okhttpHeaders.add(METHOD_HEADER);
    }

    okhttpHeaders.add(new Header(Header.TARGET_AUTHORITY, authority));
    String path = defaultPath;
    okhttpHeaders.add(new Header(Header.TARGET_PATH, path));

    okhttpHeaders.add(new Header(GrpcUtil.USER_AGENT_KEY.name(), userAgent));

    // All non-pseudo headers must come after pseudo headers.
    okhttpHeaders.add(CONTENT_TYPE_HEADER);
    okhttpHeaders.add(TE_HEADER);

    // Now add any application-provided headers.
    byte[][] serializedHeaders = TransportFrameUtil.toHttp2Headers(headers);
    for (int i = 0; i < serializedHeaders.length; i += 2) {
      ByteString key = ByteString.of(serializedHeaders[i]);
      String keyString = key.utf8();
      if (isApplicationHeader(keyString)) {
        ByteString value = ByteString.of(serializedHeaders[i + 1]);
        okhttpHeaders.add(new Header(key, value));
      }
    }

    return okhttpHeaders;
  }

  /**
   * Returns {@code true} if the given header is an application-provided header. Otherwise, returns
   * {@code false} if the header is reserved by GRPC.
   */
  private static boolean isApplicationHeader(String key) {
    // Don't allow HTTP/2 pseudo headers or content-type to be added by the application.
    return (!key.startsWith(":")
            && !CONTENT_TYPE_KEY.name().equalsIgnoreCase(key))
            && !USER_AGENT_KEY.name().equalsIgnoreCase(key);
  }
}
