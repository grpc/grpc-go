package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.collect.MapMaker;
import com.google.common.logging.FormattingLogger;
import com.google.net.stubby.transport.Transport;

import java.io.InputStream;
import java.util.concurrent.ConcurrentMap;

/**
 * Common implementation for {@link Request} and {@link Response} operations
 */
public abstract class AbstractOperation implements Operation {

  private static final FormattingLogger logger =
      FormattingLogger.getLogger(AbstractOperation.class);

  /**
   * Allow implementations to associate state with an operation
   */
  private ConcurrentMap stash;
  private final int id;
  private Phase phase;
  private Status status;

  public AbstractOperation(int id) {
    this.id = id;
    this.phase = Phase.HEADERS;
    stash = new MapMaker().concurrencyLevel(2).makeMap();
  }

  @Override
  public int getId() {
    return id;
  }

  @Override
  public Phase getPhase() {
    return phase;
  }

  /**
   * Move into the desired phase.
   */
  protected Operation progressTo(Phase desiredPhase) {
    if (desiredPhase.ordinal() < phase.ordinal()) {
      close(new Status(Transport.Code.INTERNAL,
          "Canot move to " + desiredPhase.name() + " from " + phase.name()));
    } else {
      phase = desiredPhase;
      if (phase == Phase.CLOSED) {
        status = Status.OK;
      }
    }
    return this;
  }

  @Override
  public Operation addContext(String type, InputStream message, Phase nextPhase) {
    if (getPhase() == Phase.CLOSED) {
      throw new RuntimeException("addContext called after operation closed");
    }
    if (phase == Phase.PAYLOAD) {
      progressTo(Phase.FOOTERS);
    }
    if (phase == Phase.HEADERS || phase == Phase.FOOTERS) {
      return progressTo(nextPhase);
    }
    throw new IllegalStateException("Cannot add context in phase " + phase.name());
  }

  @Override
  public Operation addPayload(InputStream payload, Phase nextPhase) {
    if (getPhase() == Phase.CLOSED) {
      throw new RuntimeException("addPayload called after operation closed");
    }
    if (phase == Phase.HEADERS) {
      progressTo(Phase.PAYLOAD);
    }
    if (phase == Phase.PAYLOAD) {
      return progressTo(nextPhase);
    }
    throw new IllegalStateException("Cannot add payload in phase " + phase.name());
  }

  @Override
  public Operation close(Status status) {
    // TODO(user): Handle synchronization properly.
    Preconditions.checkNotNull(status, "status");
    this.phase = Phase.CLOSED;
    if (this.status != null && this.status.getCode() != status.getCode()) {
      logger.severefmt(status.getCause(),
          "Attempting to override status of already closed operation from %s to %s",
        this.status.getCode(), status.getCode());
    } else {
      this.status = status;
    }
    return this;
  }

  @Override
  public Status getStatus() {
    return status;
  }

  @Override
  public <E> E put(Object key, E value) {
    return (E) stash.put(key, value);
  }

  @Override
  public <E> E get(Object key) {
    return (E) stash.get(key);
  }

  @Override
  public <E> E remove(Object key) {
    return (E) stash.remove(key);
  }
}
