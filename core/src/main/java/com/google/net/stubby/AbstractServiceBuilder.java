package com.google.net.stubby;

import com.google.common.base.Throwables;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.Service;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

import javax.annotation.Nullable;

/**
 * Base class for channel builders and server builders.
 *
 * <p>The ownership rule: a builder generally does not take ownership of any objects passed to it.
 * The caller is responsible for closing them if needed. The builder is only responsible for the
 * life-cycle of objects created inside.
 */
abstract class AbstractServiceBuilder<ProductT extends Service,
         BuilderT extends AbstractServiceBuilder> {

  @Nullable
  private ExecutorService userExecutor;

  /**
   * Provides a custom executor.
   *
   * <p>It's an optional parameter. If the user has not provided an executor when the service is
   * built, the builder will create a cached thread-pool executor.
   *
   * <p>The service won't take ownership of the given executor. It's caller's responsibility to
   * shut down the executor when it's desired.
   */
  public final BuilderT executor(ExecutorService executor) {
    userExecutor = executor;
    return (BuilderT) this;
  }

  /**
   * Builds a service using the given parameters.
   *
   * <p>The returned service has not been started at this point. You will need to start it by
   * yourself or use {@link #buildAndStart()}.
   */
   private ProductT build() {
    final ExecutorService executor = (userExecutor == null)
        ?  Executors.newCachedThreadPool() : userExecutor;
    ProductT service = buildImpl(executor);
    // We shut down the executor only if we created it.
    if (userExecutor == null) {
      service.addListener(new ClosureHook() {
        @Override
        protected void onClosed() {
          executor.shutdown();
        }
      }, MoreExecutors.directExecutor());
    }
    return service;
  }

  /**
   * Builds and starts a service.
   *
   * <p>The service may not be running when this method returns. If you want to wait until it's up
   * and running, either use {@link Service#awaitRunning()} or {@link #buildAndWaitForRunning()}.
   *
   * @return the service that has just been built and started
   */
  public final ProductT buildAndStart() {
    ProductT service = build();
    service.startAsync();
    return service;
  }

  /**
   * Builds and starts a service, and wait until it's up and running.
   *
   * @return the service that has just been built and is now running.
   */
  public final ProductT buildAndWaitForRunning() {
    ProductT service = buildAndStart();
    try {
      service.awaitRunning();
    } catch (Exception e) {
      service.stopAsync();
      throw Throwables.propagate(e);
    }
    return service;
  }

  /**
   * Builds and starts a service, and wait until it's up and running, with a timeout.
   *
   * @return the service that has just been built and is now running.
   * @throws TimeoutException if the service didn't become running within the given timeout.
   */
  public final ProductT buildAndWaitForRunning(long timeout, TimeUnit unit)
      throws TimeoutException {
    ProductT service = buildAndStart();
    try {
      service.awaitRunning(timeout, unit);
    } catch (Exception e) {
      service.stopAsync();
      if (e instanceof TimeoutException) {
        throw (TimeoutException) e;
      } else {
        throw Throwables.propagate(e);
      }
    }
    return service;
  }

  /**
   * Subclasses may use this as a convenient listener for cleaning up after the built service.
   */
  protected abstract static class ClosureHook extends Service.Listener {
    protected abstract void onClosed();

    @Override
    public void terminated(Service.State from) {
      onClosed();
    }

    @Override
    public void failed(Service.State from, Throwable failure) {
      onClosed();
    }
  }

  /**
   * Implemented by subclasses to build the actual service object. The given executor is owned by
   * this base class.
   */
  protected abstract ProductT buildImpl(ExecutorService executor);
}
