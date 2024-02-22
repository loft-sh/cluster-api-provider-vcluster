package repository

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/helm"
	"github.com/pkg/errors"
)

// Entries describes the entries of an helm chart repository
type Entries struct {
	// The API Version of this repository.
	APIVersion string `json:"apiVersion,omitempty"`
	// The entries of this repository
	Entries map[string][]*helm.Metadata `json:"entries,omitempty"`
}

// Definition defines a named repository
type Definition struct {
	Name     string `json:"name,omitempty"`
	URL      string `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
}

func ParseReadmeValues(ctx context.Context, helmChart *helm.Chart) (string, string, error) {
	if len(helmChart.Metadata.Urls) == 0 {
		return "", "", nil
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	url := helmChart.Metadata.Urls[0]
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = strings.TrimSuffix(helmChart.Repository.URL, "/") + "/" + strings.TrimPrefix(url, "/")
	}

	resp, err := newRequest(ctx, client, url, helmChart.Repository.Username, helmChart.Repository.Password)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	uncompressedStream, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", "", errors.Wrap(err, "read gzip")
	}

	var (
		readme = ""
		values = ""
	)

	tarReader := tar.NewReader(uncompressedStream)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return "", "", fmt.Errorf("extract: Next() failed: %s", err.Error())
		}

		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
			splitted := strings.Split(header.Name, "/")
			if splitted[0] == "README.md" || (len(splitted) > 1 && splitted[1] == "README.md") {
				buffer := &bytes.Buffer{}
				_, err := io.Copy(buffer, tarReader)
				if err != nil {
					return "", "", fmt.Errorf("extract: error reading README.md: %v", err.Error())
				}

				readme = buffer.String()
				if values != "" {
					return readme, values, nil
				}
				continue
			}
			if splitted[0] == "values.yaml" || (len(splitted) > 1 && splitted[1] == "values.yaml") {
				buffer := &bytes.Buffer{}
				_, err := io.Copy(buffer, tarReader)
				if err != nil {
					return "", "", fmt.Errorf("extract: error reading values.yaml: %v", err.Error())
				}

				values = buffer.String()
				if readme != "" {
					return readme, values, nil
				}
				continue
			}
			if _, err := io.Copy(io.Discard, tarReader); err != nil {
				return "", "", fmt.Errorf("extract: Copy() failed: %s", err.Error())
			}
		default:
			return "", "", fmt.Errorf("extract: uknown type: %v in %s", header.Typeflag, header.Name)
		}
	}

	return readme, values, nil
}

func ParseRepository(ctx context.Context, repository *Definition) ([]helm.Chart, error) {
	indexURL := strings.Join([]string{strings.TrimRight(repository.URL, "/"), "index.yaml"}, "/")
	body, err := Get(ctx, &http.Client{
		Timeout: time.Second * 20,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}, indexURL, repository.Username, repository.Password)
	if err != nil {
		return nil, fmt.Errorf("skipping repo %s, because of error retrieving app store repository index %s: %w", repository.Name, indexURL, err)
	}

	entries := &Entries{}
	err = yaml.Unmarshal(body, entries)
	if err != nil {
		return nil, fmt.Errorf("skipping repo %s, because of error parsing app store repository index %s: %w", repository.Name, indexURL, err)
	}

	// we only add the latest version to avoid huge files
	charts := []helm.Chart{}
	for _, metadatas := range entries.Entries {
		if len(metadatas) == 0 {
			continue
		}

		chart := helm.Chart{
			Metadata: *metadatas[0],
			Repository: helm.ChartRepository{
				Name:     repository.Name,
				URL:      repository.URL,
				Username: repository.Username,
				Password: repository.Password,
				Insecure: repository.Insecure,
			},
			Versions: []string{},
		}

		// add versions
		for _, meta := range metadatas {
			chart.Versions = append(chart.Versions, meta.Version)
		}

		charts = append(charts, chart)
	}

	return charts, nil
}

func newRequest(ctx context.Context, client *http.Client, url, username, password string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if username != "" && password != "" {
		if strings.HasPrefix(username, "$") {
			username = os.Getenv(username[1:])
		}
		if strings.HasPrefix(password, "$") {
			password = os.Getenv(password[1:])
		}

		req.SetBasicAuth(username, password)
	}

	return client.Do(req)
}

func Get(ctx context.Context, client *http.Client, url, username, password string) ([]byte, error) {
	resp, err := newRequest(ctx, client, url, username, password)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}
