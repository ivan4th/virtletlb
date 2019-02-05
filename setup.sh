#!/bin/bash
set -u -e
tmpdir="$(mktemp -d)"
trap "rm -rf '${tmpdir}'" EXIT

docker exec kube-master cat /etc/kubernetes/admin.conf >"${tmpdir}/outer.conf"
virtletctl ssh root@k8s-0 cat /etc/kubernetes/admin.conf >"${tmpdir}/inner.conf"

# XXX: this is a dirty hack

sed -i 's/\(^\| \)\(name\|cluster\): kubernetes$/\1\2: inner/' "${tmpdir}/inner.conf"
sed -i 's/kubernetes-admin@kubernetes/inner/g' "${tmpdir}/inner.conf"
sed -i 's/kubernetes-admin/inner-admin/g' "${tmpdir}/inner.conf"

sed -i 's/\(^\| \)\(name\|cluster\): kubernetes$/\1\2: outer/' "${tmpdir}/outer.conf"
sed -i 's/kubernetes-admin@kubernetes/outer/g' "${tmpdir}/outer.conf"
sed -i 's/kubernetes-admin/outer-admin/g' "${tmpdir}/outer.conf"

KUBECONFIG="${tmpdir}/outer.conf:${tmpdir}/inner.conf" \
          kubectl config view --raw >"${tmpdir}/clusters.conf"

kubectl create secret generic cluster-configs \
        --from-file=clusters.conf="${tmpdir}/clusters.conf" \
        --dry-run -o yaml |
  kubectl apply -f -

kubectl create secret generic cluster-configs \
        --from-file=clusters.conf="${tmpdir}/clusters.conf" \
        --dry-run -o yaml |
  virtletctl ssh root@k8s-0 -- kubectl apply -f -

# TODO: use service accounts

kubectl apply \
        -f config/crds/virtletlb_v1alpha1_innerservice.yaml \
        -f https://raw.githubusercontent.com/google/metallb/v0.7.3/manifests/metallb.yaml \
        -f metallb-conf.yaml 

kubectl apply -f outer-controller.yaml
virtletctl ssh root@k8s-0 -- kubectl apply -f - <inner-controller.yaml
