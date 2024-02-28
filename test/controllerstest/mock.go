package controllerstest

import (
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/helm"
	"github.com/stretchr/testify/mock"
)

type MockHelmClient struct {
	mock.Mock
}

func (m *MockHelmClient) Install(_, _ string, _ helm.UpgradeOptions) error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockHelmClient) Upgrade(_, _ string, _ helm.UpgradeOptions) error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockHelmClient) Rollback(_, _ string, _ string) error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockHelmClient) Delete(_, _ string) error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockHelmClient) Exists(_, _ string) (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}
