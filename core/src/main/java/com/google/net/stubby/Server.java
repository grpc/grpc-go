package com.google.net.stubby;

import com.google.common.util.concurrent.Service;

import javax.annotation.concurrent.ThreadSafe;

/**
 * Server for listening for and dispatching incoming calls. Although Server is an interface, it is
 * not expected to be implemented by application code or interceptors.
 */
@ThreadSafe
public interface Server extends Service {}
