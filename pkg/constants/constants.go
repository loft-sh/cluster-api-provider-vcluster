package constants

import "os"

var (
	// DefaultVClusterVersion is the default version of the virtual cluster to use
	DefaultVClusterVersion = "0.15.0"

	// DefaultVClusterChartName is the default chart name of the virtual cluster to use
	DefaultVClusterChartName = "vcluster"

	// DefaultVClusterRepo is the default repo url of the virtual cluster to use
	DefaultVClusterRepo = "https://charts.loft.sh"
)

func init() {
	if os.Getenv("DEFAULT_VCLUSTER_CHART_VERSION") != "" {
		DefaultVClusterVersion = os.Getenv("DEFAULT_VCLUSTER_CHART_VERSION")
	}
	if os.Getenv("DEFAULT_VCLUSTER_CHART_NAME") != "" {
		DefaultVClusterChartName = os.Getenv("DEFAULT_VCLUSTER_CHART_NAME")
	}
	if os.Getenv("DEFAULT_VCLUSTER_CHART_REPO") != "" {
		DefaultVClusterRepo = os.Getenv("DEFAULT_VCLUSTER_CHART_REPO")
	}
}
