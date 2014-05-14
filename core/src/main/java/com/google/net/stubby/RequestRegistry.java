package com.google.net.stubby;

import com.google.common.collect.MapMaker;

import java.util.Collection;
import java.util.Collections;
import java.util.Iterator;
import java.util.concurrent.ConcurrentMap;

/**
 * Registry of in-flight requests..
 */
public class RequestRegistry {

  private final ConcurrentMap<Integer, Request> inFlight;

  public RequestRegistry() {
    inFlight = new MapMaker().concurrencyLevel(8).initialCapacity(1001).makeMap();
  }

  public void register(Request op) {
    if (inFlight.putIfAbsent(op.getId(), op) != null) {
      throw new IllegalArgumentException("Operation already bound for " + op.getId());
    }
  }

  public Request lookup(int id) {
    return inFlight.get(id);
  }

  public Request remove(int id) {
    return inFlight.remove(id);
  }

  public Collection<Integer> getAllRequests() {
    return Collections.unmodifiableSet(inFlight.keySet());
  }

  /**
   * Closes any requests (and their associated responses) with the given status and removes them
   * from the registry.
   */
  public void drainAllRequests(Status responseStatus) {
    Iterator<Request> it = inFlight.values().iterator();
    while (it.hasNext()) {
      Request request = it.next();
      if (request != null) {
        if (request.getPhase() != Operation.Phase.CLOSED) {
          request.close(responseStatus);
        }
        if (request.getResponse().getPhase() != Operation.Phase.CLOSED) {
          request.getResponse().close(responseStatus);
        }
      }
      it.remove();
    }
  }
}
