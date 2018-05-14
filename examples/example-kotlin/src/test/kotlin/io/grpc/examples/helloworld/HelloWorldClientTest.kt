/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.examples.helloworld

import org.junit.Assert.assertEquals
import org.mockito.AdditionalAnswers.delegatesTo
import org.mockito.Mockito.mock
import org.mockito.Mockito.verify


import io.grpc.inprocess.InProcessChannelBuilder
import io.grpc.inprocess.InProcessServerBuilder
import io.grpc.stub.StreamObserver
import io.grpc.testing.GrpcCleanupRule
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.JUnit4
import org.mockito.ArgumentCaptor
import org.mockito.Matchers

/**
 * Unit tests for [HelloWorldClient].
 * For demonstrating how to write gRPC unit test only.
 * Not intended to provide a high code coverage or to test every major usecase.
 *
 *
 * For more unit test examples see [io.grpc.examples.routeguide.RouteGuideClientTest] and
 * [io.grpc.examples.routeguide.RouteGuideServerTest].
 */
@RunWith(JUnit4::class)
class HelloWorldClientTest {
    /**
     * This rule manages automatic graceful shutdown for the registered servers and channels at the
     * end of test.
     */
    @get:Rule
    val grpcCleanup = GrpcCleanupRule()

    private val serviceImpl = mock(GreeterGrpc.GreeterImplBase::class.java, delegatesTo<Any>(object : GreeterGrpc.GreeterImplBase() {

    }))
    private var client: HelloWorldClient? = null

    @Before
    @Throws(Exception::class)
    fun setUp() {
        // Generate a unique in-process server name.
        val serverName = InProcessServerBuilder.generateName()

        // Create a server, add service, start, and register for automatic graceful shutdown.
        grpcCleanup.register(InProcessServerBuilder
                .forName(serverName).directExecutor().addService(serviceImpl).build().start())

        // Create a client channel and register for automatic graceful shutdown.
        val channel = grpcCleanup.register(
                InProcessChannelBuilder.forName(serverName).directExecutor().build())

        // Create a HelloWorldClient using the in-process channel;
        client = HelloWorldClient(channel)
    }

    /**
     * To test the client, call from the client against the fake server, and verify behaviors or state
     * changes from the server side.
     */
    @Test
    fun greet_messageDeliveredToServer() {
        val requestCaptor = ArgumentCaptor.forClass(HelloRequest::class.java)

        client!!.greet("test name")

        verify<GreeterGrpc.GreeterImplBase>(serviceImpl)
                .sayHello(requestCaptor.capture(), Matchers.any<StreamObserver<HelloReply>>())
        assertEquals("test name", requestCaptor.value.name)
    }
}
