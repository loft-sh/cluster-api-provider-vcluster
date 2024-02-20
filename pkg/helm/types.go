package helm

type Chart struct {
	// Metadata provides information about a chart
	// +optional
	Metadata Metadata `json:"metadata,omitempty"`

	// Versions holds all chart versions
	// +optional
	Versions []string `json:"versions,omitempty"`

	// Repository is the repository name of this chart
	// +optional
	Repository ChartRepository `json:"repository,omitempty"`
}

type ChartRepository struct {
	// Name is the name of the repository
	// +optional
	Name string `json:"name,omitempty"`

	// URL is the repository url
	// +optional
	URL string `json:"url,omitempty"`

	// Username of the repository
	// +optional
	Username string `json:"username,omitempty"`

	// Password of the repository
	// +optional
	Password string `json:"password,omitempty"`

	// Insecure specifies if the chart should be retrieved without TLS
	// verification
	// +optional
	Insecure bool `json:"insecure,omitempty"`
}
type Maintainer struct {
	// Name is a user name or organization name
	// +optional
	Name string `json:"name,omitempty"`
	// Email is an optional email address to contact the named maintainer
	// +optional
	Email string `json:"email,omitempty"`
	// URL is an optional URL to an address for the named maintainer
	// +optional
	URL string `json:"url,omitempty"`
}

type Metadata struct {
	// The name of the chart
	// +optional
	Name string `json:"name,omitempty"`
	// The URL to a relevant project page, git repo, or contact person
	// +optional
	Home string `json:"home,omitempty"`
	// Source is the URL to the source code of this chart
	// +optional
	Sources []string `json:"sources,omitempty"`
	// A SemVer 2 conformant version string of the chart
	// +optional
	Version string `json:"version,omitempty"`
	// A one-sentence description of the chart
	// +optional
	Description string `json:"description,omitempty"`
	// A list of string keywords
	// +optional
	Keywords []string `json:"keywords,omitempty"`
	// A list of name and URL/email address combinations for the maintainer(s)
	// +optional
	Maintainers []*Maintainer `json:"maintainers,omitempty"`
	// The URL to an icon file.
	// +optional
	Icon string `json:"icon,omitempty"`
	// The API Version of this chart.
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
	// The condition to check to enable chart
	// +optional
	Condition string `json:"condition,omitempty"`
	// The tags to check to enable chart
	// +optional
	Tags string `json:"tags,omitempty"`
	// The version of the application enclosed inside of this chart.
	// +optional
	AppVersion string `json:"appVersion,omitempty"`
	// Whether or not this chart is deprecated
	// +optional
	Deprecated bool `json:"deprecated,omitempty"`
	// Annotations are additional mappings uninterpreted by Helm,
	// made available for inspection by other applications.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// KubeVersion is a SemVer constraint specifying the version of Kubernetes required.
	// +optional
	KubeVersion string `json:"kubeVersion,omitempty"`
	// Specifies the chart type: application or library
	// +optional
	Type string `json:"type,omitempty"`
	// Urls where to find the chart contents
	// +optional
	Urls []string `json:"urls,omitempty"`
}
