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

package main

import (
	"flag"
	"k8s.io/klog"
	"log"

	"admiralty.io/multicluster-controller/pkg/cluster"
	"admiralty.io/multicluster-controller/pkg/manager"
	"admiralty.io/multicluster-service-account/pkg/config"
	// extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	// "k8s.io/apimachinery/pkg/runtime"
	kscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/sample-controller/pkg/signals"

	"github.com/ivan4th/virtletlb/pkg/apis/virtletlb/v1alpha1"
	inner "github.com/ivan4th/virtletlb/pkg/controller/inner"
	outer "github.com/ivan4th/virtletlb/pkg/controller/outer"
)

// var (
// 	scheme = runtime.NewScheme()
// )

// func init() {
// 	// https://github.com/kubernetes-sigs/kubebuilder/issues/491#issuecomment-459474907
// 	kscheme.AddToScheme(scheme)
// 	extapi.AddToScheme(scheme)
// 	v1alpha1.AddToScheme(scheme)
// }

func main() {
	// https://github.com/kubernetes-sigs/kubebuilder/issues/491#issuecomment-459474907
	// FIXME: should be able to specify the scheme for Cluster (?)
	v1alpha1.AddToScheme(kscheme.Scheme)

	// https://github.com/kubernetes/klog/blob/master/examples/coexist_glog/coexist_glog.go
	flag.Set("alsologtostderr", "true")
	flag.Parse()

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	flag.CommandLine.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			f2.Value.Set(value)
		}
	})

	if flag.NArg() < 1 {
		log.Fatalf("Usage: manager command args...")
	}

	// TODO: use cobra
	m := manager.New()
	command := flag.Arg(0)
	switch command {
	case "inner":
		if flag.NArg() != 3 {
			log.Fatalf("Usage: manager inner inner-ctx outer-ctx")
		}

		srcCtx, dstCtx := flag.Arg(1), flag.Arg(2)

		innerCfg, _, err := config.NamedConfigAndNamespace(srcCtx)
		if err != nil {
			log.Fatal(err)
		}
		innerCluster := cluster.New(srcCtx, innerCfg, cluster.Options{})

		outerCfg, outerNs, err := config.NamedConfigAndNamespace(dstCtx)
		if err != nil {
			log.Fatal(err)
		}
		outerCluster := cluster.New(dstCtx, outerCfg, cluster.Options{})

		co, err := inner.NewController(innerCluster, outerCluster, outerNs)
		if err != nil {
			log.Fatalf("creating dest controller: %v", err)
		}

		m.AddController(co)
	case "outer":
		if flag.NArg() != 2 {
			log.Fatalf("Usage: manager outer outer-ctx")
		}

		srcCtx := flag.Arg(1)

		cfg, outerNs, err := config.NamedConfigAndNamespace(srcCtx)
		if err != nil {
			log.Fatal(err)
		}
		outerCluster := cluster.New(srcCtx, cfg, cluster.Options{})

		co, err := outer.NewController(outerCluster, outerNs)
		if err != nil {
			log.Fatalf("creating dest controller: %v", err)
		}

		m.AddController(co)
	case "outer-daemon":
		if flag.NArg() != 2 {
			log.Fatalf("Usage: manager outer outer-ctx")
		}
		log.Fatalf("TODO")
	}

	if err := m.Start(signals.SetupSignalHandler()); err != nil {
		log.Fatalf("while or after starting manager: %v", err)
	}
}
