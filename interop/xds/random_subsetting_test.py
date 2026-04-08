import logging
from typing import Optional

import grpc
from absl import flags
from absl.testing import absltest

# These modules come from the grpc/grpc/tools/run_tests/xds_k8s_test_driver codebase
from framework import xds_k8s_testcase
from framework.helpers import skips

logger = logging.getLogger(__name__)
FLAGS = flags.FLAGS

class RandomSubsettingTest(xds_k8s_testcase.RegularXdsKubernetesTestCase):
    def test_random_subsetting(self) -> None:
        # Start a highly scaled server deployment
        with self.subTest('01_run_test_server'):
            self.startTestServers(replica_count=4)

        # Configure the Control Plane (Traffic Director) to use the new policy
        with self.subTest('02_setup_xds'):
            self.setup_xds_for_random_subsetting()

        # Start the client (which in this case will be the grpc-go client you built)
        with self.subTest('03_start_test_client'):
            self.startTestClient()

        # Send RPCs and confirm only a random subset of servers actually received traffic
        with self.subTest('04_verify_random_subsetting'):
            self.assertRpcStatusCodes(
                self.test_client,
                expected=(
                    grpc.StatusCode.OK,
                )
            )
            # Fetch stats from the client by sending 100 RPCs
            stats_response = self.test_client.get_load_balancer_stats(num_rpcs=100)
            
            # Subsetting logic check - we should see exactly subset_size backends
            # Assuming subset_size was configured to 2 in the setup function
            hit_backends = len(stats_response.rpcs_by_peer)
            self.assertEqual(hit_backends, 2, 
                             f"Expected exactly 2 backends to be hit due to random_subsetting, got {hit_backends}")
            
            logger.info("Successfully verified traffic was properly subsetted to %s out of 4 backends.", hit_backends)

    def setup_xds_for_random_subsetting(self):
        # We construct the Custom LB Policy protobuf
        # Note: Ensure the compiled envoy protobufs are available in the python env
        try:
            from envoy.extensions.load_balancing_policies.random_subsetting.v3 import random_subsetting_pb2
            from google.protobuf import any_pb2
            
            rs_config = random_subsetting_pb2.RandomSubsetting(
                subset_size=2
            )
            lb_config_any = any_pb2.Any()
            lb_config_any.Pack(rs_config)
            
            # You will likely need to inject this into your test framework's TrafficDirector
            # setup method. In the xds_k8s_test_driver, this looks something like:
            # self.td.setup_client_routing(
            #     default_lb_policy=lb_config_any
            # )
            logger.info("Setting up xDS generic Client Routing...")
            self.setup_client_routing()
            
            # NOTE: For advanced custom policies, you might need to extend
            # `framework.infrastructure.traffic_director` in the grpc repo to accept 
            # and wrap custom `Any` messages if it currently hardcodes RoundRobin/RingHash.
            
        except ImportError:
            logger.warning("Envoy protobufs for random_subsetting not found. "
                           "You may need to compile them or update the pip requirements.")

if __name__ == '__main__':
    absltest.main(failfast=True)
