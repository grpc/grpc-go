/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.perfmark;

import com.google.errorprone.annotations.CompileTimeConstant;
import javax.annotation.concurrent.ThreadSafe;

/**
 * PerfMark is a collection of stub methods for marking key points in the RPC lifecycle.  This
 * class is {@link io.grpc.Internal} and {@link io.grpc.ExperimentalApi}.  Do not use this yet.
 */
@ThreadSafe
public final class PerfMark {
  private PerfMark() {
    throw new AssertionError("nope");
  }

  /**
   * Start a Task with a Tag to identify it; a task represents some work that spans some time, and
   * you are interested in both its start time and end time.
   *
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #taskStart(String)} or {@link PerfTag#create(String)}.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static void taskStart(PerfTag tag, @CompileTimeConstant String taskName) {}

  /**
   * Start a Task with a Tag to identify it; a task represents some work that spans some time, and
   * you are interested in both its start time and end time.
   *
   * @param scopeId The scope of the task. The scope id is used to join task starting and ending
   *     across threads. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #taskStart(String)} or {@link PerfTag#create(String)}.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static void taskStart(
      long scopeId, String scopeName, PerfTag tag, @CompileTimeConstant String taskName) {}

  /**
   * Start a Task; a task represents some work that spans some time, and you are interested in both
   * its start time and end time.
   *
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static void taskStart(@CompileTimeConstant String taskName) {}

  /**
   * Start a Task; a task represents some work that spans some time, and you are interested in both
   * its start time and end time.
   *
   * @param scopeId The scope of the task. The scope id is used to join task starting and ending
   *     across threads. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static void taskStart(
      long scopeId, String scopeName, @CompileTimeConstant String taskName) {}

  /**
   * Start a Task with a Tag to identify it and with a time threshold; a task represents some work
   * that spans some time, and you are interested in both its start time and end time.
   *
   * <p>Sometimes, you may be interested in only events that take more than a certain time
   * threshold. In such cases, you can use this method. A Task that takes less than the specified
   * threshold, along with all its sub-tasks, events, and additional tags will be discarded.
   *
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #taskStartWithMinPeriod(long, String)} or {@link PerfTag#create(String)}.
   * @param minPeriodNanos Tasks that takes less than the specified time period, in nanosecond, will
   *     be discarded, along with its sub-tasks, events, and additional tags.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static void taskStartWithMinPeriod(
      PerfTag tag, long minPeriodNanos, @CompileTimeConstant String taskName) {}

  /**
   * Start a Task with a Tag to identify it and with a time threshold; a task represents some work
   * that spans some time, and you are interested in both its start time and end time.
   *
   * <p>Sometimes, you may be interested in only events that take more than a certain time
   * threshold. In such cases, you can use this method. A Task that takes less than the specified
   * threshold, along with all its sub-tasks, events, and additional tags will be discarded.
   *
   * @param scopeId The scope of the task. The scope id is used to join task starting and ending
   *     across threads. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #taskStartWithMinPeriod(long, String)} or {@link PerfTag#create(String)}.
   * @param minPeriodNanos Tasks that takes less than the specified time period, in nanosecond, will
   *     be discarded, along with its sub-tasks, events, and additional tags.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static void taskStartWithMinPeriod(
      long scopeId,
      String scopeName,
      PerfTag tag,
      long minPeriodNanos,
      @CompileTimeConstant String taskName) {}

  /**
   * Start a Task with time threshold. A task represents some work that spans some time, and you are
   * interested in both its start time and end time.
   *
   * <p>Sometimes, you may be interested in only events that take more than a certain time
   * threshold. In such cases, you can use this method. A task that takes less than the specified
   * threshold, along with all its sub-tasks, events, and additional tags will be discarded.
   *
   * @param minPeriodNanos Tasks that takes less than the specified time period, in nanosecond, will
   *     be discarded, along with its sub-tasks, events, and additional tags.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static void taskStartWithMinPeriod(
      long minPeriodNanos, @CompileTimeConstant String taskName) {}

  /**
   * Start a Task with time threshold. A task represents some work that spans some time, and you are
   * interested in both its start time and end time.
   *
   * <p>Sometimes, you may be interested in only events that take more than a certain time
   * threshold. In such cases, you can use this method. A task that takes less than the specified
   * threshold, along with all its sub-tasks, events, and additional tags will be discarded.
   *
   * @param scopeId The scope of the task. The scope id is used to join task starting and ending
   *     across threads. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param minPeriodNanos Tasks that takes less than the specified time period, in nanosecond, will
   *     be discarded, along with its sub-tasks, events, and additional tags.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static void taskStartWithMinPeriod(
      long scopeId, String scopeName, long minPeriodNanos, @CompileTimeConstant String taskName) {}

  /** End a Task. See {@link #taskStart(PerfTag, String)}. */
  public static void taskEnd() {}

  /**
   * End a Task. See {@link #taskStart(PerfTag, String)}.
   *
   * @param scopeId The scope of the task. The scope id is used to join task starting and ending
   *     across threads. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   */
  public static void taskEnd(long scopeId, String scopeName) {}

  /**
   * Start a Task with a Tag to identify it in a try-with-resource statement; a task represents some
   * work that spans some time, and you are interested in both its start time and end time.
   *
   * <p>Use this in a try-with-resource statement so that task will end automatically.
   *
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #task(String)} or {@link PerfTag#create(String)}.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static PerfMarkTask task(PerfTag tag, @CompileTimeConstant String taskName) {
    return AUTO_DO_NOTHING;
  }

  /**
   * Start a Task with a Tag to identify it in a try-with-resource statement; a task represents some
   * work that spans some time, and you are interested in both its start time and end time.
   *
   * <p>Use this in a try-with-resource statement so that task will end automatically.
   *
   * @param scopeId The scope of the task. The scope id is used to join task starting and ending
   *     across threads. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #task(String)} or {@link PerfTag#create(String)}.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static PerfMarkTask task(
      long scopeId, String scopeName, PerfTag tag, @CompileTimeConstant String taskName) {
    return AUTO_DO_NOTHING;
  }

  /**
   * Start a Task it in a try-with-resource statement; a task represents some work that spans some
   * time, and you are interested in both its start time and end time.
   *
   * <p>Use this in a try-with-resource statement so that task will end automatically.
   *
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static PerfMarkTask task(@CompileTimeConstant String taskName) {
    return AUTO_DO_NOTHING;
  }

  /**
   * Start a Task it in a try-with-resource statement; a task represents some work that spans some
   * time, and you are interested in both its start time and end time.
   *
   * <p>Use this in a try-with-resource statement so that task will end automatically.
   *
   * @param scopeId The scope of the task. The scope id is used to join task starting and ending
   *     across threads. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static PerfMarkTask task(
      long scopeId, String scopeName, @CompileTimeConstant String taskName) {
    return AUTO_DO_NOTHING;
  }

  /**
   * Start a Task with a Tag to identify it, and with time threshold, in a try-with-resource
   * statement; a task represents some work that spans some time, and you are interested in both its
   * start time and end time.
   *
   * <p>Use this in a try-with-resource statement so that task will end automatically.
   *
   * <p>Sometimes, you may be interested in only events that take more than a certain time
   * threshold. In such cases, you can use this method. A task that takes less than the specified
   * threshold, along with all its sub-tasks, events, and additional tags will be discarded.
   *
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #taskWithMinPeriod(long, String)} or {@link PerfTag#create(String)}.
   * @param minPeriodNanos Tasks that takes less than the specified time period, in nanosecond, will
   *     be discarded, along with its sub-tasks, events, and additional tags.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static PerfMarkTask taskWithMinPeriod(
      PerfTag tag, long minPeriodNanos, @CompileTimeConstant String taskName) {
    return AUTO_DO_NOTHING;
  }

  /**
   * Start a Task with a Tag to identify it, and with time threshold, in a try-with-resource
   * statement; a task represents some work that spans some time, and you are interested in both its
   * start time and end time.
   *
   * <p>Use this in a try-with-resource statement so that task will end automatically.
   *
   * <p>Sometimes, you may be interested in only events that take more than a certain time
   * threshold. In such cases, you can use this method. A task that takes less than the specified
   * threshold, along with all its sub-tasks, events, and additional tags will be discarded.
   *
   * @param scopeId The scope of the task. The scope id is used to join task starting and ending
   *     across threads. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #taskWithMinPeriod(long, String)} or {@link PerfTag#create(String)}.
   * @param minPeriodNanos Tasks that takes less than the specified time period, in nanosecond, will
   *     be discarded, along with its sub-tasks, events, and additional tags.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static PerfMarkTask taskWithMinPeriod(
      long scopeId,
      String scopeName,
      PerfTag tag,
      long minPeriodNanos,
      @CompileTimeConstant String taskName) {
    return AUTO_DO_NOTHING;
  }

  /**
   * Start a Task with time threshold in a try-with-resource statement; a task represents some work
   * that spans some time, and you are interested in both its start time and end time.
   *
   * <p>Use this in a try-with-resource statement so that task will end automatically.
   *
   * <p>Sometimes, you may be interested in only events that take more than a certain time
   * threshold. In such cases, you can use this method. A task that takes less than the specified
   * threshold, along with all its sub-tasks, events, and additional tags will be discarded.
   *
   * @param minPeriodNanos Tasks that takes less than the specified time period, in nanosecond, will
   *     be discarded, along with its sub-tasks, events, and additional tags.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static PerfMarkTask taskWithMinPeriod(
      long minPeriodNanos, @CompileTimeConstant String taskName) {
    return AUTO_DO_NOTHING;
  }

  /**
   * Start a Task with time threshold in a try-with-resource statement; a task represents some work
   * that spans some time, and you are interested in both its start time and end time.
   *
   * <p>Use this in a try-with-resource statement so that task will end automatically.
   *
   * <p>Sometimes, you may be interested in only events that take more than a certain time
   * threshold. In such cases, you can use this method. A task that takes less than the specified
   * threshold, along with all its sub-tasks, events, and additional tags will be discarded.
   *
   * @param scopeId The scope of the task. The scope id is used to join task starting and ending
   *     across threads. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param minPeriodNanos Tasks that takes less than the specified time period, in nanosecond, will
   *     be discarded, along with its sub-tasks, events, and additional tags.
   * @param taskName The name of the task. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this task.
   */
  public static PerfMarkTask taskWithMinPeriod(
      long scopeId, String scopeName, long minPeriodNanos, @CompileTimeConstant String taskName) {
    return AUTO_DO_NOTHING;
  }

  static final PerfMarkTask AUTO_DO_NOTHING = new PerfMarkTask() {
    @Override
    public void close() {}
  };

  /**
   * Records an Event with a Tag to identify it.
   *
   * <p>An Event is different from a Task in that you don't care how much time it spanned. You are
   * interested in only the time it happened.
   *
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #event(String)} or {@link PerfTag#create(String)}.
   * @param eventName The name of the event. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this event.
   */
  public static void event(PerfTag tag, @CompileTimeConstant String eventName) {}

  /**
   * Records an Event with a Tag to identify it.
   *
   * <p>An Event is different from a Task in that you don't care how much time it spanned. You are
   * interested in only the time it happened.
   *
   * @param scopeId The scope of the event. The scope id is used to associated the event with a
   *     particular scope. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     #event(String)} or {@link PerfTag#create(String)}.
   * @param eventName The name of the event. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this event.
   */
  public static void event(
      long scopeId, String scopeName, PerfTag tag, @CompileTimeConstant String eventName) {}

  /**
   * Records an Event.
   *
   * <p>An Event is different from a Task in that you don't care how much time it spanned. You are
   * interested in only the time it happened.
   *
   * @param eventName The name of the event. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this event.
   */
  public static void event(@CompileTimeConstant String eventName) {}

  /**
   * Records an Event.
   *
   * <p>An Event is different from a Task in that you don't care how much time it spanned. You are
   * interested in only the time it happened.
   *
   * @param scopeId The scope of the event. The scope id is used to associate the event with a
   *     particular scope. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param eventName The name of the event. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this event.
   */
  public static void event(long scopeId, String scopeName, @CompileTimeConstant String eventName) {}

  /**
   * Add an additional tag to the last task that was started.
   *
   * <p>A tag is different from an Event or a task in that clients don't care about the time at
   * which this tag is added. Instead, it allows clients to associate an additional tag to the
   * current Task.
   *
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     PerfTag#create(String)}.
   * @param tagName The name of the tag. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this tag.
   */
  public static void tag(PerfTag tag, @CompileTimeConstant String tagName) {}

  /**
   * Add an additional tag to the last task that was started.
   *
   * <p>A tag is different from an Event or a task in that clients don't care about the time at
   * which this tag is added. Instead, it allows clients to associate an additional tag to the
   * current Task.
   *
   * @param scopeId The scope of the tag. The scope id is used to associate a tag with a particular
   *     scope. The scope id must be a positive number.
   * @param scopeName The name of the scope.
   * @param tag a Tag object associated with the task. See {@link PerfTag} for description. Don't
   *     use 0 for the {@code numericTag} of the Tag object. 0 is reserved to represent that a task
   *     does not have a numeric tag associated. In this case, you are encouraged to use {@link
   *     PerfTag#create(String)}.
   * @param tagName The name of the tag. <b>This parameter must be a compile-time constant!</b>
   *     Otherwise, instrumentation result will show "(invalid name)" for this tag.
   */
  public static void tag(
      long scopeId, String scopeName, PerfTag tag, @CompileTimeConstant String tagName) {}
}
