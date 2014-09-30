package com.google.net.stubby;

/**
 * The call type of a method.
 */
public enum MethodType {
  UNARY,
  CLIENT_STREAMING,
  SERVER_STREAMING,
  DUPLEX_STREAMING,
  UNKNOWN
}
