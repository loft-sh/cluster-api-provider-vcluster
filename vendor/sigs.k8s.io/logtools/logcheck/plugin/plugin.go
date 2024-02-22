/*
Copyright 2022 The Kubernetes Authors.

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

// Package main is meant to be compiled as a plugin for golangci-lint, see
// https://golangci-lint.run/contributing/new-linters/#create-a-plugin.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"golang.org/x/tools/go/analysis"
	"sigs.k8s.io/logtools/logcheck/pkg"
)

type analyzerPlugin struct{}

func (*analyzerPlugin) GetAnalyzers() []*analysis.Analyzer {
	analyzer, _ := pkg.Analyser()
	return []*analysis.Analyzer{analyzer}
}

// AnalyzerPlugin is the entry point for golangci-lint.
var AnalyzerPlugin analyzerPlugin

type settings struct {
	Check  map[string]bool `json:"check"`
	Config string          `json:"config"`
}

// New API, see https://github.com/golangci/golangci-lint/pull/3887.
func New(pluginSettings interface{}) ([]*analysis.Analyzer, error) {
	// We could manually parse the settings. This would involve several
	// type assertions. Encoding as JSON and then decoding into our
	// settings struct is easier.
	//
	// The downside is that format errors are less user-friendly.
	var buffer bytes.Buffer
	if err := json.NewEncoder(&buffer).Encode(pluginSettings); err != nil {
		return nil, fmt.Errorf("encoding settings as internal JSON buffer: %v", err)
	}
	var s settings
	decoder := json.NewDecoder(&buffer)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&s); err != nil {
		return nil, fmt.Errorf("decoding settings from internal JSON buffer: %v", err)
	}

	// Now create an analyzer and configure it.
	analyzer, config := pkg.Analyser()
	for check, enabled := range s.Check {
		if err := config.SetEnabled(check, enabled); err != nil {
			// No need to wrap, the error is informative.
			return nil, err
		}
	}
	if err := config.ParseConfig(s.Config); err != nil {
		return nil, fmt.Errorf("parsing config: %v", err)
	}

	return []*analysis.Analyzer{analyzer}, nil
}
