package com.google.net.stubby.spdy.okhttp;

import com.google.common.collect.Lists;

import com.squareup.okhttp.internal.spdy.Header;

import java.util.List;

/**
 * Constants for request/response headers.
 */
public class Headers {
  public static final Header SCHEME_HEADER = new Header(Header.TARGET_SCHEME, "https");
  public static final Header CONTENT_TYPE_HEADER =
      new Header("content-type", "application/protorpc");
  public static final Header RESPONSE_STATUS_OK = new Header(Header.RESPONSE_STATUS, "200");

  public static List<Header> createRequestHeaders(String operationName) {
    List<Header> headers = Lists.newArrayListWithCapacity(6);
    headers.add(new Header(Header.TARGET_PATH, operationName));
    headers.add(SCHEME_HEADER);
    headers.add(CONTENT_TYPE_HEADER);
    return headers;
  }

  public static List<Header> createResponseHeaders() {
    // TODO(user): Need to review status code handling
    List<Header> headers = Lists.newArrayListWithCapacity(6);
    headers.add(RESPONSE_STATUS_OK);
    return headers;
  }
}