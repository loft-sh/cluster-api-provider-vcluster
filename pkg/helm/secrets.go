/*
Copyright The Helm Authors.
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
package helm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kblabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	klog "k8s.io/klog/v2"
	client2 "sigs.k8s.io/controller-runtime/pkg/client"
)

var b64 = base64.StdEncoding

var magicGzip = []byte{0x1f, 0x8b, 0x08}

// Release describes a deployment of a chart, together with the chart
// and the variables used to deploy that chart.
type Release struct {
	// Name is the name of the release
	Name string `json:"name,omitempty"`
	// Info provides information about a release
	Info *Info `json:"info,omitempty"`
	// Chart is the chart that was released.
	Chart *Chart `json:"chart,omitempty"`
	// Config is the set of extra Values added to the chart.
	// These values override the default values inside of the chart.
	Config map[string]interface{} `json:"config,omitempty"`
	// Version is an int which represents the version of the release.
	Version int `json:"version,omitempty"`
	// Namespace is the kubernetes namespace of the release.
	Namespace string `json:"namespace,omitempty"`

	Secret *corev1.Secret `json:"-"`
}

// Info describes release information.
type Info struct {
	// FirstDeployed is when the release was first deployed.
	// +optional
	FirstDeployed Time `json:"first_deployed,omitempty"`
	// LastDeployed is when the release was last deployed.
	// +optional
	LastDeployed Time `json:"last_deployed,omitempty"`
	// Deleted tracks when this object was deleted.
	// +optional
	Deleted Time `json:"deleted,omitempty"`
	// Description is human-friendly "log entry" about this release.
	// +optional
	Description string `json:"description,omitempty"`
	// Status is the current state of the release
	// +optional
	Status string `json:"status,omitempty"`
	// Contains the rendered templates/NOTES.txt if available
	// +optional
	Notes string `json:"notes,omitempty"`
}

// Chart holds the chart metadata
type Chart struct {
	Metadata *Metadata `json:"metadata,omitempty"`
}

// Secrets is a wrapper around an implementation of a kubernetes
// SecretsInterface.
type Secrets struct {
	kubeClient    client2.Client
	kubeClientset kubernetes.Interface
}

// NewSecrets initializes a new Secrets wrapping an implementation of
// the kubernetes SecretsInterface.
func NewSecrets(client client2.Client) *Secrets {
	return &Secrets{
		kubeClient: client,
	}
}

// NewSecretsClientSet initializes a new Secrets wrapping an implementation of
// the kubernetes SecretsInterface.
func NewSecretsClientSet(clientSet kubernetes.Interface) *Secrets {
	return &Secrets{
		kubeClientset: clientSet,
	}
}

func (secrets *Secrets) Update(ctx context.Context, secret *corev1.Secret) (*corev1.Secret, error) {
	if secrets.kubeClientset != nil {
		return secrets.kubeClientset.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
	}

	err := secrets.kubeClient.Update(ctx, secret)
	return secret, err
}

// List fetches all releases and returns the list releases such
// that filter(release) == true. An error is returned if the
// secret fails to retrieve the releases.
func (secrets *Secrets) List(ctx context.Context, labels kblabels.Selector, namespace string) ([]*Release, error) {
	req, err := kblabels.NewRequirement("owner", selection.Equals, []string{"helm"})
	if err != nil {
		return nil, err
	}
	if labels == nil {
		labels = kblabels.Everything()
	}
	labels = labels.Add(*req)

	list := &corev1.SecretList{}
	if secrets.kubeClientset != nil {
		list, err = secrets.kubeClientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labels.String(),
		})
	} else {
		err = secrets.kubeClient.List(ctx, list, &client2.ListOptions{
			LabelSelector: labels,
			Namespace:     namespace,
		})
	}
	if err != nil {
		return nil, err
	}

	var results []*Release

	// iterate over the secrets object list
	// and decode each release
	for _, item := range list.Items {
		cpy := item
		rls, err := decodeRelease(&cpy, string(item.Data["release"]))
		if err != nil {
			klog.Infof("list: failed to decode release: %v", err)
			continue
		} else if rls.Chart == nil || rls.Chart.Metadata == nil || rls.Info == nil {
			klog.Infof("list: metadata info is empty of release: %s", rls.Name)
			continue
		}

		results = append(results, rls)
	}
	return results, nil
}

// Query fetches all releases that match the provided map of labels.
// An error is returned if the secret fails to retrieve the releases.
func (secrets *Secrets) Get(ctx context.Context, name string, namespace string) (*Release, error) {
	ls := kblabels.Set{}
	ls["name"] = name
	list, err := secrets.List(ctx, ls.AsSelector(), namespace)
	if err != nil {
		return nil, err
	} else if len(list) == 0 {
		return nil, kerrors.NewNotFound(corev1.Resource("Secret"), name)
	}

	var latest *Release
	for _, rls := range list {
		if latest == nil || latest.Version < rls.Version {
			latest = rls
		}
	}

	return latest, nil
}

// decodeRelease decodes the bytes of data into a release
// type. Data must contain a base64 encoded gzipped string of a
// valid release, otherwise an error is returned.
func decodeRelease(secret *corev1.Secret, data string) (*Release, error) {
	// base64 decode string
	b, err := b64.DecodeString(data)
	if err != nil {
		return nil, err
	} else if len(b) < 3 {
		return nil, fmt.Errorf("unexpected secret content: %s", data)
	}

	// For backwards compatibility with releases that were stored before
	// compression was introduced we skip decompression if the
	// gzip magic header is not found
	if bytes.Equal(b[0:3], magicGzip) {
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		b2, err := ioutil.ReadAll(r)
		if err != nil {
			return nil, err
		}
		b = b2
	}

	var rls Release
	// unmarshal release object bytes
	if err := json.Unmarshal(b, &rls); err != nil {
		return nil, fmt.Errorf("error decoding %s: %v", string(b), err)
	}

	rls.Secret = secret
	return &rls, nil
}
