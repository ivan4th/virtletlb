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

package outer

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
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ivan4th/virtletlb/pkg/apis"
	"github.com/ivan4th/virtletlb/pkg/apis/virtletlb/v1alpha1"
)

func ToJSON(o interface{}) string {
	bs, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		klog.Fatalf("error marshalling json: %v", err)
	}
	return string(bs)
}

var skipRx *regexp.Regexp = regexp.MustCompile("^kube-system/(kube-scheduler|kube-controller-manager)$")

func NewController(cluster *cluster.Cluster, targetNamespace string) (*controller.Controller, error) {
	klog.V(1).Infof("*** starting watch ***")
	client, err := cluster.GetDelegatingClient()
	if err != nil {
		return nil, fmt.Errorf("getting delegating client for source cluster: %v", err)
	}

	co := controller.New(&reconciler{client: client, targetNamespace: targetNamespace}, controller.Options{})

	if err := co.WatchResourceReconcileObject(cluster, &v1.Endpoints{}, controller.WatchOptions{}); err != nil {
		return nil, fmt.Errorf("setting up Endpoints watch in the cluster: %v", err)
	}
	if err := co.WatchResourceReconcileObject(cluster, &v1alpha1.InnerService{}, controller.WatchOptions{}); err != nil {
		return nil, fmt.Errorf("setting up InnerService watch in the cluster: %v", err)
	}

	if err := apis.AddToScheme(cluster.GetScheme()); err != nil {
		return nil, fmt.Errorf("adding APIs to dest cluster's scheme: %v", err)
	}

	klog.V(1).Infof("*** watch started ***")

	return co, nil
}

type reconciler struct {
	client          client.Client
	targetNamespace string
}

func (r *reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	reqName := req.NamespacedName.String()
	if skipRx.MatchString(reqName) {
		return reconcile.Result{}, nil
	}

	klog.V(1).Infof("*** outer watch: %v ***", reqName)
	curSvc := &v1.Service{}
	if err := r.client.Get(context.TODO(), r.targetNamespacedName(req.NamespacedName), curSvc); err != nil {
		if errors.IsNotFound(err) {
			curSvc = nil
		} else {
			return reconcile.Result{}, err
		}
	}

	if curSvc != nil && curSvc.Spec.Type != "LoadBalancer" {
		klog.V(1).Infof("ignoring service %s of type %s", reqName, curSvc.Spec.Type)
		return reconcile.Result{}, nil
	}

	innerSvc := &v1alpha1.InnerService{}
	if err := r.client.Get(context.TODO(), req.NamespacedName, innerSvc); err != nil {
		if errors.IsNotFound(err) {
			klog.V(1).Infof("no inner svc for %v; deleting service, if it exists", reqName)
			// ...TODO: multicluster garbage collector
			// Until then...
			err := r.deleteService(r.targetNamespacedName(req.NamespacedName))
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, err
	}

	svc := r.makeService(innerSvc)
	reference.SetMulticlusterControllerReference(svc, reference.NewMulticlusterOwnerReference(innerSvc, innerSvc.GroupVersionKind(), req.Context))

	if curSvc == nil {
		klog.V(1).Infof("not found: %v, creating new service for %v", r.targetNamespacedName(req.NamespacedName), reqName)
		klog.V(1).Infof("content:\n%s\n", ToJSON(svc))
		err := r.client.Create(context.TODO(), svc)
		return reconcile.Result{}, err
	}

	// Some parts of the spec are updated by kube-controller-manager,
	// let's skip updating the service if other parts didn't change
	// FIXME: do it in a more sane and futureproof way
	svc.Spec.ClusterIP = curSvc.Spec.ClusterIP
	svc.Spec.SessionAffinity = curSvc.Spec.SessionAffinity
	svc.Spec.ExternalTrafficPolicy = curSvc.Spec.ExternalTrafficPolicy
	if len(svc.Spec.Ports) == len(curSvc.Spec.Ports) {
		for n, p := range curSvc.Spec.Ports {
			svc.Spec.Ports[n].NodePort = p.NodePort
		}
	}

	shouldUpdate := !reflect.DeepEqual(curSvc.Spec, svc.Spec)
	klog.V(1).Infof("shouldUpdate: %v", shouldUpdate)
	if shouldUpdate {
		klog.V(1).Infof("spec mismatch! WAS:\n%s\n\nNOW:\n%s\n", ToJSON(curSvc.Spec), ToJSON(svc.Spec))
	}

	lbIP := ""
	if len(curSvc.Status.LoadBalancer.Ingress) > 0 {
		lbIP = curSvc.Status.LoadBalancer.Ingress[0].IP
	}
	klog.V(1).Infof("current service lbIP: %q", lbIP)

	var err error
	if innerSvc.Status.LoadBalancerIP != lbIP {
		if !reflect.DeepEqual(curSvc.Status, svc.Status) {
			klog.V(1).Infof("outer: setting inner service's LbIP to %s", lbIP)
			innerSvc.Status = v1alpha1.InnerServiceStatus{
				LoadBalancerIP: lbIP,
			}
			// FIXME: perhaps should be doable via r.client.Status().Update(...)
			// but it isn't, need to check
			err = r.client.Update(context.TODO(), innerSvc)
		}
	}

	if err == nil && shouldUpdate {
		klog.V(1).Infof("updating the outer service")
		curSvc.Spec = svc.Spec
		err = r.client.Update(context.TODO(), curSvc)
	}

	return reconcile.Result{}, err
}

func (r *reconciler) targetNamespacedName(pod types.NamespacedName) types.NamespacedName {
	return types.NamespacedName{
		Namespace: r.targetNamespace,
		Name:      pod.Name,
	}
}

func (r *reconciler) deleteService(nsn types.NamespacedName) error {
	svc := &v1.Service{}
	if err := r.client.Get(context.TODO(), nsn, svc); err != nil {
		if errors.IsNotFound(err) {
			// all good
			return nil
		}
		return err
	}
	if err := r.client.Delete(context.TODO(), svc); err != nil {
		return err
	}
	return nil
}

func (r *reconciler) makeService(isvc *v1alpha1.InnerService) *v1.Service {
	var selector map[string]string
	if len(isvc.Spec.NodeNames) > 0 {
		selector = map[string]string{
			"statefulset.kubernetes.io/pod-name": isvc.Spec.NodeNames[0],
		}
	}
	var ports []v1.ServicePort
	for _, p := range isvc.Spec.Ports {
		ports = append(ports, v1.ServicePort{
			Name:       p.Name,
			Protocol:   p.Protocol,
			Port:       p.Port,
			TargetPort: intstr.FromInt(int(p.NodePort)),
		})
	}

	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.targetNamespace,
			Name:      isvc.Name,
		},
		Spec: v1.ServiceSpec{
			Type:     "LoadBalancer",
			Ports:    ports,
			Selector: selector,
		},
	}
}
