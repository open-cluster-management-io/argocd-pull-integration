#!/usr/bin/env bash

# Copyright 2025 Open Cluster Management.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

KUBECTL=${KUBECTL:-kubectl}

echo "==========================================="
echo "Installing MetalLB"
echo "==========================================="

echo "Applying MetalLB manifests..."
$KUBECTL --context kind-hub apply -f https://raw.githubusercontent.com/metallb/metallb/main/config/manifests/metallb-native.yaml

echo "Waiting for MetalLB to be ready..."
$KUBECTL --context kind-hub wait --namespace metallb-system \
  --for=condition=Ready pods \
  --selector=app=metallb \
  --timeout=120s

echo "Configuring MetalLB IP address pool..."
cat <<EOF | $KUBECTL --context kind-hub apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: kind-address-pool
  namespace: metallb-system
spec:
  addresses:
  - 172.18.255.200-172.18.255.250
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: kind-l2-advertisement
  namespace: metallb-system
spec:
  ipAddressPools:
  - kind-address-pool
EOF

echo "==========================================="
echo "MetalLB installation complete"
echo "==========================================="

