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

package pubconfig

import (
	"fmt"
	"io/ioutil"

	"k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	configSecretKey = "admin.conf"
)

// PublishConfig publishes the specified Kubernetes config file as a
// secret in the target namespace.
func PublishConfig(configPath, secretName, namespace string, outerCfg *rest.Config) error {
	clientSet, err := kubernetes.NewForConfig(outerCfg)
	if err != nil {
		return fmt.Errorf("couldn't create clientset: %v", err)
	}

	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("couldn't read file %q: %v", configPath, err)
	}

	if err = clientSet.CoreV1().Secrets(namespace).Delete(secretName, nil); err != nil && !apierrs.IsNotFound(err) {
		return fmt.Errorf("error deleting secret %q in ns %q: %v", secretName, namespace)
	}

	_, err = clientSet.CoreV1().Secrets(namespace).Create(&v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      secretName,
		},
		Data: map[string][]byte{
			configSecretKey: data,
		},
	})

	return err
}
