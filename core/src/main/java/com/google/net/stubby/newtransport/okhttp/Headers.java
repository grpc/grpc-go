package com.google.net.stubby.newtransport.okhttp;

import com.google.common.base.Preconditions;
import com.google.common.collect.Lists;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.newtransport.HttpUtil;

import com.squareup.okhttp.internal.spdy.Header;

import okio.ByteString;

import java.util.List;

/**
 * Constants for request/response headers.
 */
public class Headers {

  public static final Header SCHEME_HEADER = new Header(Header.TARGET_SCHEME, "https");
  public static final Header METHOD_HEADER = new Header(Header.TARGET_METHOD, HttpUtil.HTTP_METHOD);
  public static final Header CONTENT_TYPE_HEADER =
      new Header(HttpUtil.CONTENT_TYPE_HEADER, HttpUtil.CONTENT_TYPE_PROTORPC);
  public static final Header RESPONSE_STATUS_OK = new Header(Header.RESPONSE_STATUS, "200");

  /**
   * Serializes the given headers and creates a list of OkHttp {@link Header}s to be used when
   * creating a stream. Since this serializes the headers, this method should be called in the
   * application thread context.
   */
  public static List<Header> createRequestHeaders(Metadata.Headers headers, String defaultPath,
      String defaultAuthority) {
    Preconditions.checkNotNull(headers, "headers");
    Preconditions.checkNotNull(defaultPath, "defaultPath");
    Preconditions.checkNotNull(defaultAuthority, "defaultAuthority");

    List<Header> okhttpHeaders = Lists.newArrayListWithCapacity(6);

    // Set GRPC-specific headers.
    okhttpHeaders.add(SCHEME_HEADER);
    okhttpHeaders.add(METHOD_HEADER);
    String authority = headers.getAuthority() != null ? headers.getAuthority() : defaultAuthority;
    okhttpHeaders.add(new Header(Header.TARGET_AUTHORITY, authority));
    String path = headers.getPath() != null ? headers.getPath() : defaultPath;
    okhttpHeaders.add(new Header(Header.TARGET_PATH, path));

    // All non-pseudo headers must come after pseudo headers.
    okhttpHeaders.add(CONTENT_TYPE_HEADER);

    // Now add any application-provided headers.
    byte[][] serializedHeaders = headers.serialize();
    for (int i = 0; i < serializedHeaders.length; i++) {
      ByteString key = ByteString.of(serializedHeaders[i]);
      ByteString value = ByteString.of(serializedHeaders[++i]);
      if (isApplicationHeader(key)) {
        okhttpHeaders.add(new Header(key, value));
      }
    }

    return okhttpHeaders;
  }

  public static List<Header> createResponseHeaders() {
    // TODO(user): Need to review status code handling
    List<Header> headers = Lists.newArrayListWithCapacity(6);
    headers.add(RESPONSE_STATUS_OK);
    return headers;
  }

  /**
   * Returns {@code true} if the given header is an application-provided header. Otherwise, returns
   * {@code false} if the header is reserved by GRPC.
   */
  private static boolean isApplicationHeader(ByteString key) {
    String keyString = key.utf8();
    // Don't allow HTTP/2 pseudo headers or content-type to be added by the applciation.
    return (!keyString.startsWith(":")
        && !HttpUtil.CONTENT_TYPE_HEADER.equalsIgnoreCase(keyString));
  }
}
