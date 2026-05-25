# AKS Flex Node networking contract

AKS Flex nodes that run against an AKS cluster with `networkPlugin: azure` must use Azure CNI pod IPs that are routable from the AKS VNet. Do not install a host-local bridge fallback such as `10.244.0.0/16` or a node-specific `10.244.x.0/24` conflist to make pods appear Ready; that hides the platform failure and produces pod IPs that other AKS workloads cannot route to.

## Required contract for Azure CNI nodes

1. The AKS VNet and the Flex node VNet must have bidirectional VNet peering when the Flex node NIC is not in the AKS VNet. Both peerings must allow virtual network access and forwarded traffic.
2. NSGs and routes must allow the AKS control plane and AKS worker subnets to reach the Flex node kubelet on TCP `10250` and Flex pod IPs on workload ports.
3. Each Flex node NIC must have Azure CNI pod IP inventory before kubelet schedules pods. For Azure VM-backed Flex nodes, attach secondary private IP configurations on the node NIC in the Flex subnet. Provision at least the expected pod capacity for the node, plus headroom for system pods.
4. The agent config should leave `cni.mode` unset or set it to `"azure"`. `"bridge"` is only supported with `agent.e2eMode: true` for local tests.

The AKSFlexNode agent does not currently own production Azure VM, Karpenter, or cross-VNet peering provisioning. The owner that creates AzureFlex/H100 nodes must satisfy the peering and secondary NIC IP configuration requirements before bootstrapping the agent. The E2E Bicep module in this repo models the NIC-side requirement with `secondaryPrivateIPAddressCount`.

## H100 longhaul incident summary

The scheduled H100 GPU longhaul leg in `aks-ai-runtime` failed while A10/A100 legs passed. Fail-fast diagnostics from PR `azure-management-and-platforms/aks-ai-runtime#433` exposed two infrastructure failures on `flex-h100-tx4cb` in `aks-flex-westus`.

First, the API server could not reach the kubelet proxy on `172.17.1.10:10250` because the node was created in `voice-agent-flex-sweden-rg/flex-nodes-vnet` (`172.17.0.0/16`) while AKS was peered to a different intended H100 VNet. Peering `MC_rg-aks-flex-westus-kecho_aks-flex-westus_westus/aks-vnet-15796304` and `voice-agent-flex-sweden-rg/flex-nodes-vnet` in both directions, with VNet access and forwarded traffic enabled, made:

```bash
kubectl get --raw /api/v1/nodes/flex-h100-tx4cb/proxy/healthz
```

return `ok`.

Second, H100 pods received non-routable `10.244.200.x` addresses because `/etc/cni/net.d/10-h100-bridge.conflist` was ordered ahead of `15-azure.conflist`. Ray workers started, but the Ray head timed out connecting to the worker pod IP and raylet exited after GCS marked it dead. Azure CNI logs on the H100 node showed `Failed to allocate address: No available addresses` for pool `172.17.1.0/24`; the NIC only had primary IP `172.17.1.10` and no secondary IP configurations for pods.

Live repair disabled the bridge conflist, added secondary NIC IP configurations `172.17.1.20`-`172.17.1.39` to `flex-h100-tx4cb-nic`, and verified a pod received `172.17.1.22`. An AKS `raypool` pod reached that H100 pod over HTTP, and `TestInferenceRayJobGPU` plus `TestTrainingRayJobGPU` passed locally.

## Validation checklist

Use a new or disposable Flex node for validation; do not mutate longhaul nodes unless the test owner approves it.

```bash
# Kubelet proxy must be reachable from the API server.
kubectl get --raw /api/v1/nodes/<flex-node-name>/proxy/healthz

# Azure CNI should be the first active CNI config. There must be no host-local 10.244 bridge fallback.
sudo ls -1 /etc/cni/net.d
sudo grep -R "10\\.244\\|host-local\\|bridge" /etc/cni/net.d || true

# The node NIC must have secondary private IP configurations available for pod allocation.
az network nic ip-config list \
  --resource-group <flex-node-resource-group> \
  --nic-name <flex-node-nic-name> \
  --query "[].{name:name,privateIp:privateIPAddress,primary:primary}" \
  --output table

# A scheduled pod should receive an IP from the Flex VNet/subnet, not 10.244.x.x.
kubectl get pod -o wide --field-selector spec.nodeName=<flex-node-name>
```
