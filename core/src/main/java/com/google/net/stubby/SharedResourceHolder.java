package com.google.net.stubby;

import com.google.common.base.Preconditions;

import java.util.IdentityHashMap;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;

import javax.annotation.concurrent.ThreadSafe;

/**
 * A holder for shared resource singletons.
 *
 * <p>Components like client channels and servers need certain resources, e.g. a thread pool, to
 * run. If the user has not provided such resources, these components will use a default one, which
 * is shared as a static resource. This class holds these default resources and manages their
 * life-cycles.
 *
 * <p>A resource is identified by the reference of a {@link Resource} object, which is typically a
 * singleton, provided to the get() and release() methods. Each Resource object (not its class) maps
 * to an object cached in the holder.
 *
 * <p>Resources are ref-counted and shut down after a delay when the ref-count reaches zero.
 */
@ThreadSafe
public final class SharedResourceHolder {
  static final long DESTROY_DELAY_SECONDS = 1;

  // The sole holder instance.
  private static final SharedResourceHolder holder = new SharedResourceHolder(
      new ScheduledExecutorFactory() {
        @Override
        public ScheduledExecutorService createScheduledExecutor() {
          return Executors.newSingleThreadScheduledExecutor(new ThreadFactory() {
            @Override public Thread newThread(Runnable r) {
              Thread thread = new Thread(r);
              thread.setDaemon(true);
              return thread;
            }
          });
        }
      });

  private final IdentityHashMap<Resource<?>, Instance> instances =
      new IdentityHashMap<Resource<?>, Instance>();

  private final ScheduledExecutorFactory destroyerFactory;

  private ScheduledExecutorService destroyer;

  // Visible to tests that would need to create instances of the holder.
  SharedResourceHolder(ScheduledExecutorFactory destroyerFactory) {
    this.destroyerFactory = destroyerFactory;
  }

  /**
   * Try to get an existing instance of the given resource. If an instance does not exist, create a
   * new one with the given factory.
   *
   * @param resource the singleton object that identifies the requested static resource
   */
  public static <T> T get(Resource<T> resource) {
    return holder.getInternal(resource);
  }

  /**
   * Releases an instance of the given resource.
   *
   * <p>The instance must have been obtained from {@link #get(Resource)}. Otherwise will throw
   * IllegalArgumentException.
   *
   * <p>Caller must not release a reference more than once. It's advisory that you clear the
   * reference to the instance with the null returned by this method.
   *
   * @param resource the singleton Resource object that identifies the released static resource
   * @param instance the released static resource
   *
   * @return a null which the caller can use to clear the reference to that instance.
   */
  public static <T> T release(final Resource<T> resource, final T instance) {
    return holder.releaseInternal(resource, instance);
  }

  /**
   * Visible to unit tests.
   *
   * @see {@link #get(Resource)}.
   */
  synchronized <T> T getInternal(Resource<T> resource) {
    Instance instance = instances.get(resource);
    if (instance == null) {
      instance = new Instance(resource.create());
      instances.put(resource, instance);
    }
    if (instance.destroyTask != null) {
      instance.destroyTask.cancel(false);
      instance.destroyTask = null;
    }
    instance.refcount++;
    return (T) instance.payload;
  }

  /**
   * Visible to unit tests.
   *
   * @see {@link #releaseInternal(Resource, T)}.
   */
  synchronized <T> T releaseInternal(final Resource<T> resource, final T instance) {
    final Instance cached = instances.get(resource);
    if (cached == null) {
      throw new IllegalArgumentException("No cached instance found for " + resource);
    }
    Preconditions.checkArgument(instance == cached.payload, "Releasing the wrong instance");
    Preconditions.checkState(cached.refcount > 0, "Refcount has already reached zero");
    cached.refcount--;
    if (cached.refcount == 0) {
      Preconditions.checkState(cached.destroyTask == null, "Destroy task already scheduled");
      // Schedule a delayed task to destroy the resource.
      if (destroyer == null) {
        destroyer = destroyerFactory.createScheduledExecutor();
      }
      cached.destroyTask = destroyer.schedule(new Runnable() {
        @Override public void run() {
          synchronized (SharedResourceHolder.this) {
            // Refcount may have gone up since the task was scheduled. Re-check it.
            if (cached.refcount == 0) {
              resource.close(instance);
              instances.remove(resource);
              if (instances.isEmpty()) {
                destroyer.shutdown();
                destroyer = null;
              }
            }
          }
        }
      }, DESTROY_DELAY_SECONDS, TimeUnit.SECONDS);
    }
    // Always returning null
    return null;
  }

  /**
   * Defines a resource, and the way to create and destroy instances of it.
   */
  public interface Resource<T> {
    /**
     * Create a new instance of the resource.
     */
    T create();

    /**
     * Destroy the given instance.
     */
    void close(T instance);
  }

  interface ScheduledExecutorFactory {
    ScheduledExecutorService createScheduledExecutor();
  }

  private static class Instance {
    final Object payload;
    int refcount;
    ScheduledFuture<?> destroyTask;
    Instance(Object payload) {
      this.payload = payload;
    }
  }
}
