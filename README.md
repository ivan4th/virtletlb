# VirtletLB experiment

*NOTE:* this is rather quick-and-dirty setup.

1. Start Virtlet on kubeadm-dind-cluster
1. Go to Virtlet source directory
1. Add another Virtlet node `build/cmd.sh prepare-all-nodes && kubectl label node kube-node-2 extraRuntime=virtlet`
1. Add Virtlet's example VM key to the ssh agent `ssh-add examples/vmkey`
1. Start k8s-in-k8s `kubectl apply -f examples/k8s.yaml`
1. Wait for the inner k8s to be ready ("Master setup complete" message in the log of `k8s-0` pod)
1. Go to the source directory of this project
1. Run `./setup.sh` to start the inner and outer controllers
1. Start nginx example on the inner cluster `virtletctl ssh root@k8s-0 -- kubectl apply -f - <nginx.yaml`
1. Start another nginx example on the inner cluster `virtletctl ssh root@k8s-0 -- kubectl apply -f - <nginx2.yaml`
1. Inside the inner cluster, use `kubectl get svc -w` to wait till the
   `nginx` and `nginx2` services gets an external IP (let's say it'll
   be `10.192.0.240`)
1. Try accessing the external IPs from the previous steps on the machine
   that runs kubeadm-dind-cluster: `curl http://10.97.187.152`
   The first nginx example outputs the standard nginx banner
   while the second one just gives `<html><body>2nd nginx</body></html>`
1. Enter a kubeadm node via
   `docker exec -it kube-master /bin/bash`
   (this is needed so as to make inner node IPs accessible)
1. Grab the inner cluster config from secret:
   `kubectl get secret config -o json | jq -r '.data["admin.conf"]' | base64 --decode >/tmp/admin.conf`
1. Make sure the config works:
   `KUBECONFIG=/tmp/admin.conf kubectl get nodes`

`is.yaml` file is needed for debugging of the controllers w/o actually
using MetalLB.  It's not used during normal operation.
