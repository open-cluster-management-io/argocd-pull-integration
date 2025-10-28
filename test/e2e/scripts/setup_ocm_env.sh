#!/bin/bash

set -o nounset
set -o pipefail

# kind delete clusters --all; kind create cluster --name hub; kind create cluster --name cluster1

KUBECTL=${KUBECTL:-kubectl}

BUILD_DIR="$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"
DEPLOY_DIR="$(dirname "$BUILD_DIR")"
EXAMPLE_DIR="$(dirname "$DEPLOY_DIR")"
REPO_DIR="$(dirname "$EXAMPLE_DIR")"
WORK_DIR="${REPO_DIR}/_output"
CLUSTERADM="clusteradm"

export PATH=$PATH:${WORK_DIR}/bin

echo "############  Install clusteradm"
go install open-cluster-management.io/clusteradm/cmd/clusteradm@main

echo "############ Init hub"
$KUBECTL config use-context kind-hub
${CLUSTERADM} init --wait --bundle-version=latest
joincmd=$(${CLUSTERADM} get token | grep clusteradm)

echo "############ Init agent as cluster1"
$KUBECTL config use-context kind-cluster1
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --bundle-version=latest | sed "s/<cluster_name>/cluster1/g")

echo "############ Accept join of cluster1"
$KUBECTL config use-context kind-hub
${CLUSTERADM} accept --clusters cluster1

echo "############  All-in-one env is installed successfully!!"
