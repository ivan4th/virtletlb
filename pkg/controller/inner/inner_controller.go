/*
Copyright 2019 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Original copyright notice follows
/*
Copyright 2018 The Multicluster-Controller Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package inner

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"

	"admiralty.io/multicluster-controller/pkg/cluster"
	"admiralty.io/multicluster-controller/pkg/controller"
	"admiralty.io/multicluster-controller/pkg/reconcile"
	"admiralty.io/multicluster-controller/pkg/reference"
	"github.com/ivan4th/virtletlb/pkg/apis"
	"github.com/ivan4th/virtletlb/pkg/apis/virtletlb/v1alpha1"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ToJSON(o interface{}) string {
	bs, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		klog.Fatalf("error marshalling json: %v", err)
	}
	return string(bs)
}

var skipRx *regexp.Regexp = regexp.MustCompile("^kube-system/(kube-scheduler|kube-controller-manager)$")

func NewController(source *cluster.Cluster, dest *cluster.Cluster, targetNamespace string) (*controller.Controller, error) {
	klog.V(1).Infof("*** starting watch (targetNamespace: %v) ***", targetNamespace)
	sourceclient, err := source.GetDelegatingClient()
	if err != nil {
		return nil, fmt.Errorf("getting delegating client for source cluster: %v", err)
	}
	destclient, err := dest.GetDelegatingClient()
	if err != nil {
		return nil, fmt.Errorf("getting delegating client for dest cluster: %v", err)
	}

	co := controller.New(&reconciler{
		source:          sourceclient,
		dest:            destclient,
		targetNamespace: targetNamespace,
	}, controller.Options{})

	if err := co.WatchResourceReconcileObject(source, &v1.Endpoints{}, controller.WatchOptions{}); err != nil {
		return nil, fmt.Errorf("setting up Service watch in source cluster: %v", err)
	}

	if err := apis.AddToScheme(dest.GetScheme()); err != nil {
		return nil, fmt.Errorf("adding APIs to dest cluster's scheme: %v", err)
	}

	// Note: At the moment, all clusters share the same scheme under the hood
	// (k8s.io/client-go/kubernetes/scheme.Scheme), yet multicluster-controller gives each cluster a scheme pointer.
	// Therefore, if we needed a custom resource in multiple clusters, we would redundantly
	// add it to each cluster's scheme, which points to the same underlying scheme.
	if err := co.WatchResourceReconcileController(dest, &v1alpha1.InnerService{}, controller.WatchOptions{}); err != nil {
		return nil, fmt.Errorf("setting up InnerService watch in dest cluster: %v", err)
	}
	klog.V(1).Infof("*** watch started ***")

	return co, nil
}

type reconciler struct {
	source          client.Client
	dest            client.Client
	targetNamespace string
}

func (r *reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	reqName := req.NamespacedName.String()
	if skipRx.MatchString(reqName) {
		return reconcile.Result{}, nil
	}

	klog.V(1).Infof("*** inner watch: %v ***", reqName)
	ep := &v1.Endpoints{}
	if err := r.source.Get(context.TODO(), req.NamespacedName, ep); err != nil {
		if errors.IsNotFound(err) {
			klog.V(1).Infof("no endpoints for %v; deleting InnerService, if it exists", reqName)
			// ...TODO: multicluster garbage collector
			// Until then...
			err := r.deleteInnerService(r.targetNamespacedName(req.NamespacedName))
			return reconcile.Result{}, err
		}
		klog.Warningf("get endpoints error: %v", err)
		return reconcile.Result{}, err
	}

	svc := &v1.Service{}
	if err := r.source.Get(context.TODO(), req.NamespacedName, svc); err != nil {
		if errors.IsNotFound(err) {
			klog.V(1).Infof("no service for %v; deleting InnerService, if it exists", reqName)
			// ...TODO: multicluster garbage collector
			// Until then...
			err := r.deleteInnerService(r.targetNamespacedName(req.NamespacedName))
			return reconcile.Result{}, err
		}
		klog.Warningf("get svc error: %v", err)
		return reconcile.Result{}, err
	}

	if svc.Spec.Type != "LoadBalancer" {
		klog.V(1).Infof("wrong service type %q for %v; deleting InnerService, if it exists", svc.Spec.Type, reqName)
		err := r.deleteInnerService(r.targetNamespacedName(req.NamespacedName))
		return reconcile.Result{}, err
	}

	innerSvc := r.makeInnerService(svc, ep)
	reference.SetMulticlusterControllerReference(innerSvc, reference.NewMulticlusterOwnerReference(ep, ep.GroupVersionKind(), req.Context))

	curInnerSvc := &v1alpha1.InnerService{}
	if err := r.dest.Get(context.TODO(), r.targetNamespacedName(req.NamespacedName), curInnerSvc); err != nil {
		if errors.IsNotFound(err) {
			klog.V(1).Infof("creating new InnerService for %v", reqName)
			err := r.dest.Create(context.TODO(), innerSvc)
			return reconcile.Result{}, err
		}
		klog.Warningf("get dest innersvc error: %v", err)
		return reconcile.Result{}, err
	}

	if reflect.DeepEqual(innerSvc.Spec, curInnerSvc.Spec) {
		klog.V(1).Infof("src and dst service specs match")
		var err error
		if curInnerSvc.Status.LoadBalancerIP != innerSvc.Status.LoadBalancerIP {
			klog.V(1).Infof("setting service's LbIP to %s", curInnerSvc.Status.LoadBalancerIP)
			svc.Status = v1.ServiceStatus{
				LoadBalancer: v1.LoadBalancerStatus{
					Ingress: []v1.LoadBalancerIngress{
						{
							IP: curInnerSvc.Status.LoadBalancerIP,
						},
					},
				},
			}
			err = r.source.Status().Update(context.TODO(), svc)
		} else {
			klog.V(1).Infof("keeping inner service's LbIP %s", curInnerSvc.Status.LoadBalancerIP)
		}
		return reconcile.Result{}, err
	} else {
		klog.V(1).Infof("spec mismatch! WAS:\n%s\n\nNOW:\n%s\n", ToJSON(curInnerSvc.Spec), ToJSON(innerSvc.Spec))
	}

	klog.V(1).Infof("updating the InnerService")
	curInnerSvc.Spec = innerSvc.Spec
	err := r.dest.Update(context.TODO(), curInnerSvc)
	return reconcile.Result{}, err
}

func (r *reconciler) targetNamespacedName(pod types.NamespacedName) types.NamespacedName {
	return types.NamespacedName{
		Namespace: r.targetNamespace,
		Name:      fmt.Sprintf("%s-%s", pod.Namespace, pod.Name),
	}
}

func (r *reconciler) deleteInnerService(nsn types.NamespacedName) error {
	g := &v1alpha1.InnerService{}
	if err := r.dest.Get(context.TODO(), nsn, g); err != nil {
		if errors.IsNotFound(err) {
			// all good
			return nil
		}
		return err
	}
	if err := r.dest.Delete(context.TODO(), g); err != nil {
		return err
	}
	return nil
}

func (r *reconciler) makeInnerService(svc *v1.Service, ep *v1.Endpoints) *v1alpha1.InnerService {
	var nodeNames []string
	gotNodeNames := map[string]bool{}
	for _, s := range ep.Subsets {
		for _, addr := range s.Addresses {
			if addr.NodeName != nil && *addr.NodeName != "" {
				if gotNodeNames[*addr.NodeName] {
					continue
				}
				gotNodeNames[*addr.NodeName] = true
				nodeNames = append(nodeNames, *addr.NodeName)
			}
		}
	}

	lbIP := ""
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		lbIP = svc.Status.LoadBalancer.Ingress[0].IP
	}

	var ports []v1alpha1.InnerServicePort
	for _, p := range svc.Spec.Ports {
		ports = append(ports, v1alpha1.InnerServicePort{
			Name:     p.Name,
			Protocol: p.Protocol,
			Port:     p.Port,
			NodePort: p.NodePort,
		})
	}

	return &v1alpha1.InnerService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.targetNamespace,
			Name:      fmt.Sprintf("%s-%s", ep.Namespace, ep.Name),
		},
		Spec: v1alpha1.InnerServiceSpec{
			NodeNames: nodeNames,
			Ports:     ports,
		},
		Status: v1alpha1.InnerServiceStatus{
			LoadBalancerIP: lbIP,
		},
	}
}
