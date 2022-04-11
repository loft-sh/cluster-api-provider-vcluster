package vclustervalues

import (
	"github.com/ghodss/yaml"
	vclusterhelm "github.com/loft-sh/vcluster/pkg/helm"
	vclustervalues "github.com/loft-sh/vcluster/pkg/helm/values"
	"github.com/loft-sh/vcluster/pkg/util/loghelper"
	"k8s.io/apimachinery/pkg/version"

	v1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/v1alpha1"
)

type Values interface {
	Merge(release *v1alpha1.VirtualClusterHelmRelease, logger loghelper.Logger) (string, error)
}

func NewValuesMerger(kubernetesVersion *version.Info, serviceCIDR string) Values {
	return &values{
		kubernetesVersion: kubernetesVersion,
		serviceCIDR:       serviceCIDR,
	}
}

type values struct {
	kubernetesVersion *version.Info
	serviceCIDR       string
}

func (v *values) Merge(release *v1alpha1.VirtualClusterHelmRelease, logger loghelper.Logger) (string, error) {
	valuesObj := map[string]interface{}{}
	values := release.Values
	if values != "" {
		err := yaml.Unmarshal([]byte(values), &valuesObj)
		if err != nil {
			return "", err
		}
	}

	defaultValues, err := v.getVClusterDefaultValues(release, logger)
	if err != nil {
		return "", err
	}

	finalValues := mergeMaps(defaultValues, valuesObj)

	out, err := yaml.Marshal(finalValues)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

func (v *values) getVClusterDefaultValues(release *v1alpha1.VirtualClusterHelmRelease, logger loghelper.Logger) (map[string]interface{}, error) {
	valuesStr, err := vclustervalues.GetDefaultReleaseValues(
		&vclusterhelm.ChartOptions{
			ChartName:         release.Chart.Name,
			ChartRepo:         release.Chart.Repo,
			ChartVersion:      release.Chart.Version,
			CIDR:              v.serviceCIDR,
			KubernetesVersion: v.kubernetesVersion,
		}, logger,
	)
	if err != nil {
		return nil, err
	}

	valuesObj := map[string]interface{}{}
	err = yaml.Unmarshal([]byte(valuesStr), &valuesObj)
	if err != nil {
		return nil, err
	}

	return valuesObj, nil
}

func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}
