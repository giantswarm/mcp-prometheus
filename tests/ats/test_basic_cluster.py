import logging
import requests
import time
from contextlib import contextmanager
from functools import partial
from json import dumps
from pathlib import Path
from typing import Dict, List, Optional

import pykube
import pytest
from pytest_helm_charts.clusters import Cluster
from pytest_helm_charts.k8s.deployment import wait_for_deployments_to_run

logger = logging.getLogger(__name__)

deployment_name = "mcp-prometheus"
namespace_name = "mcp-prometheus"

timeout: int = 560


@pytest.mark.smoke
def test_api_working(kube_cluster: Cluster) -> None:
    """Very minimalistic example of using the [kube_cluster](pytest_helm_charts.fixtures.kube_cluster)
    fixture to get an instance of [Cluster](pytest_helm_charts.clusters.Cluster) under test
    and access its [kube_client](pytest_helm_charts.clusters.Cluster.kube_client) property
    to get access to Kubernetes API of cluster under test.
    Please refer to [pykube](https://pykube.readthedocs.io/en/latest/api/pykube.html) to get docs
    for [HTTPClient](https://pykube.readthedocs.io/en/latest/api/pykube.html#pykube.http.HTTPClient).
    """
    assert kube_cluster.kube_client is not None
    assert len(pykube.Node.objects(kube_cluster.kube_client)) >= 1


# scope "module" means this is run only once, for the first test case requesting! It might be tricky
# if you want to assert this multiple times
@pytest.fixture(scope="module")
def deployment(request, kube_cluster: Cluster) -> List[pykube.Deployment]:
    logger.info("Waiting for mcp-prometheus deployment..")

    deployment_ready = wait_for_deployment(kube_cluster)

    logger.info("mcp-prometheus deployment looks satisfied..")

    return deployment_ready


def wait_for_deployment(kube_cluster: Cluster) -> List[pykube.Deployment]:
    deployments = wait_for_deployments_to_run(
        kube_cluster.kube_client,
        [deployment_name],
        namespace_name,
        timeout,
    )
    return deployments

# when we start the tests on circleci, we have to wait for pods to be available, hence
# this additional delay and retries


@pytest.mark.smoke
@pytest.mark.upgrade
@pytest.mark.flaky(reruns=5, reruns_delay=10)
def test_pods_available(kube_cluster: Cluster, deployment: List[pykube.Deployment]):
    for s in deployment:
        assert int(s.obj["status"]["readyReplicas"]) == int(
            s.obj["spec"]["replicas"])
