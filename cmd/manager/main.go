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
	"fmt"
	"io/ioutil"
	"k8s.io/klog"
	"net"
	"os"
	"path/filepath"

	"admiralty.io/multicluster-controller/pkg/cluster"
	"admiralty.io/multicluster-controller/pkg/manager"
	"admiralty.io/multicluster-service-account/pkg/config"
	// extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	// "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/api/core/v1"
	kscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/sample-controller/pkg/signals"

	"github.com/ivan4th/virtletlb/pkg/apis/virtletlb/v1alpha1"
	inner "github.com/ivan4th/virtletlb/pkg/controller/inner"
	outer "github.com/ivan4th/virtletlb/pkg/controller/outer"
	pubconfig "github.com/ivan4th/virtletlb/pkg/pubconfig"
)

const (
	incluster               = "INCLUSTER"
	outcluster              = "OUTCLUSTER"
	outerServiceAccountPath = "/outer-serviceaccount"
	configSecretName        = "config"
)

// OuterClusterConfigInsideVM returns rest.Config and the namespace
// for the outer cluster that's based upon the service account info
// available inside Virtlet VMs. Based on OuterClusterConfig from
// client-go.
func OuterClusterConfigInsideVM() (*rest.Config, string, error) {
	// TODO: extract these from /etc/cloud/environment
	host, port := os.Getenv("OUTER_KUBERNETES_SERVICE_HOST"), os.Getenv("OUTER_KUBERNETES_SERVICE_PORT")
	if len(host) == 0 || len(port) == 0 {
		return nil, "", fmt.Errorf("unable to load the configuration of outer cluster, OUTER_KUBERNETES_SERVICE_HOST and OUTER_KUBERNETES_SERVICE_PORT must be defined")
	}

	token, err := ioutil.ReadFile(filepath.Join(outerServiceAccountPath, v1.ServiceAccountTokenKey))
	if err != nil {
		return nil, "", err
	}
	tlsClientConfig := rest.TLSClientConfig{}
	rootCAFile := filepath.Join(outerServiceAccountPath, v1.ServiceAccountRootCAKey)
	if _, err := certutil.NewPool(rootCAFile); err != nil {
		klog.Errorf("Expected to load root CA config from %s, but got err: %v", rootCAFile, err)
	} else {
		tlsClientConfig.CAFile = rootCAFile
	}

	ns, err := ioutil.ReadFile(filepath.Join(outerServiceAccountPath, v1.ServiceAccountNamespaceKey))
	if err != nil {
		return nil, "", err
	}

	return &rest.Config{
		// TODO: switch to using cluster DNS.
		Host:            "https://" + net.JoinHostPort(host, port),
		BearerToken:     string(token),
		TLSClientConfig: tlsClientConfig,
	}, string(ns), nil
}

// var (
// 	scheme = runtime.NewScheme()
// )

// func init() {
// 	// https://github.com/kubernetes-sigs/kubebuilder/issues/491#issuecomment-459474907
// 	kscheme.AddToScheme(scheme)
// 	extapi.AddToScheme(scheme)
// 	v1alpha1.AddToScheme(scheme)
// }

func getInClusterConfigOrContext(spec string) (*rest.Config, string, error) {
	var err error
	var innerCfg *rest.Config
	namespace := "default"
	if spec == incluster {
		innerCfg, err = rest.InClusterConfig()
	} else {
		innerCfg, namespace, err = config.NamedConfigAndNamespace(spec)
	}
	return innerCfg, namespace, err
}

func getOuterConfig(spec string) (*rest.Config, string, error) {
	if spec == outcluster {
		return OuterClusterConfigInsideVM()
	} else {
		return config.NamedConfigAndNamespace(spec)
	}
}

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
		klog.Fatalf("Usage: manager command args...")
	}

	// TODO: use cobra
	m := manager.New()
	command := flag.Arg(0)
	switch command {
	case "inner":
		if flag.NArg() != 3 {
			klog.Fatalf("Usage: manager inner inner-ctx|INCLUSTER outer-ctx|OUTCLUSTER")
		}

		srcCtx, dstCtx := flag.Arg(1), flag.Arg(2)

		innerCfg, _, err := getInClusterConfigOrContext(srcCtx)
		if err != nil {
			klog.Fatal(err)
		}
		innerCluster := cluster.New(srcCtx, innerCfg, cluster.Options{})

		outerCfg, outerNs, err := getOuterConfig(dstCtx)
		if err != nil {
			klog.Fatal(err)
		}
		outerCluster := cluster.New(dstCtx, outerCfg, cluster.Options{})

		co, err := inner.NewController(innerCluster, outerCluster, outerNs)
		if err != nil {
			klog.Fatalf("creating dest controller: %v", err)
		}

		m.AddController(co)
	case "outer":
		if flag.NArg() != 2 {
			klog.Fatalf("Usage: manager outer outer-ctx|INCLUSTER")
		}

		srcCtx := flag.Arg(1)
		cfg, outerNs, err := getInClusterConfigOrContext(srcCtx)
		if err != nil {
			klog.Fatal(err)
		}
		outerCluster := cluster.New(srcCtx, cfg, cluster.Options{})

		co, err := outer.NewController(outerCluster, outerNs)
		if err != nil {
			klog.Fatalf("creating dest controller: %v", err)
		}

		m.AddController(co)
	case "publish-config":
		if flag.NArg() != 3 {
			klog.Fatalf("Usage: publish-config outer-ctx|OUTCLUSTER config-path")
		}

		outerCtx := flag.Arg(1)
		configPath := flag.Arg(2)
		outerCfg, ns, err := getOuterConfig(outerCtx)
		if err == nil {
			err = pubconfig.PublishConfig(configPath, configSecretName, ns, outerCfg)
		}
		if err != nil {
			klog.Fatal(err)
		}
		os.Exit(0)
	}

	if err := m.Start(signals.SetupSignalHandler()); err != nil {
		klog.Fatalf("while or after starting manager: %v", err)
	}
}
