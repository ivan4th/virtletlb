#!/bin/bash
set -u -e
tmpdir="$(mktemp -d)"
trap "rm -rf '${tmpdir}'" EXIT

# FIXME: do proper RBAC
kubectl create clusterrolebinding permissive-binding \
        --clusterrole=cluster-admin \
        --user=admin \
        --user=kubelet \
        --group=system:serviceaccounts
virtletctl ssh root@k8s-0 -- \
        kubectl create clusterrolebinding permissive-binding \
        --clusterrole=cluster-admin \
        --user=admin \
        --user=kubelet \
        --group=system:serviceaccounts

# TODO: use service accounts

kubectl apply \
        -f config/crds/virtletlb_v1alpha1_innerservice.yaml \
        -f https://raw.githubusercontent.com/google/metallb/v0.7.3/manifests/metallb.yaml \
        -f metallb-conf.yaml 

kubectl apply -f outer-controller.yaml
virtletctl ssh root@k8s-0 -- kubectl apply -f - <inner-controller.yaml
