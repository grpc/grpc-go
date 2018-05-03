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

import io.grpc.stub.StreamObserver
import io.grpc.testing.GrpcServerRule
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
     * This creates and starts an in-process server, and creates a client with an in-process channel.
     * When the test is done, it also shuts down the in-process client and server.
     */
    @get:Rule
    val grpcServerRule = GrpcServerRule().directExecutor()

    private val serviceImpl = mock(GreeterGrpc.GreeterImplBase::class.java, delegatesTo<Any>(object : GreeterGrpc.GreeterImplBase() {

    }))
    private var client: HelloWorldClient? = null

    @Before
    @Throws(Exception::class)
    fun setUp() {
        // Add service.
        grpcServerRule.serviceRegistry.addService(serviceImpl)
        // Create a HelloWorldClient using the in-process channel;
        client = HelloWorldClient(grpcServerRule.channel)
    }

    /**
     * To test the client, call from the client against the fake server, and verify behaviors or state
     * changes from the server side.
     */
    @Test
    fun greet_messageDeliveredToServer() {
        val requestCaptor = ArgumentCaptor.forClass(HelloRequest::class.java)
        val testName = "test name"

        client!!.greet(testName)

        verify<GreeterGrpc.GreeterImplBase>(serviceImpl)
                .sayHello(requestCaptor.capture(), Matchers.any<StreamObserver<HelloReply>>())
        assertEquals(testName, requestCaptor.value.name)
    }
}
